// Copyright (c) 2020 Dean Jackson <deanishe@deanishe.net>
// Modifications Copyright (c) 2026 Andres Mena Godino
// MIT Licence applies http://opensource.org/licenses/MIT

package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	aw "github.com/deanishe/awgo"
	"github.com/deanishe/awgo/util"
	"github.com/peterbourgon/ff/ffcli"
	"go.deanishe.net/fuzzy"
)

// Regexes for tokenising tab titles and URLs.
//
// rxNonWord matches runs of non-letter/non-digit characters (URL separators
// such as "/", ":", ".", "-", "_", "?", "&", "=", "#"). They delimit tokens so
// each URL path/host/query segment becomes an independent word.
//
// rxCamel matches a lower-case letter or digit immediately followed by an
// upper-case letter, i.e. a CamelCase word boundary, so "AccountSettings"
// also yields the tokens "Business" and "Essentials".
var (
	rxNonWord = regexp.MustCompile(`[^\p{L}\p{N}]+`)
	rxCamel   = regexp.MustCompile(`([\p{Ll}\p{N}])(\p{Lu})`)
)

// titleMatchBonus is added to a term's score when it matches a token from the
// tab's title rather than its URL. The page title is more salient than a URL
// path segment, so title hits should rank higher.
const titleMatchBonus = 25.0

// tabTokens splits text into the individual word tokens used for matching:
// runs of letters/digits, plus CamelCase-split fragments. Tokens are
// de-duplicated case-insensitively. Matching happens per-token (see
// filterTabs), which keeps results precise: a query term must be a subsequence
// of a single token rather than scattering across the whole title+URL string.
//
// Example: "https://wiki.example.com/docs/.../AccountSettings/Report/Dashboard" yields
// tokens including "AccountSettings", "Business", "Essentials", "Report" and
// "Dashboard", so "accountsettings", "business" and "dashboard" all match.
func tabTokens(text string) []string {
	raw := strings.Fields(rxNonWord.ReplaceAllString(text, " "))

	seen := make(map[string]bool, len(raw)*2)
	var tokens []string
	add := func(w string) {
		if w == "" {
			return
		}
		k := strings.ToLower(w)
		if !seen[k] {
			seen[k] = true
			tokens = append(tokens, w)
		}
	}
	for _, w := range raw {
		add(w)
		if split := rxCamel.ReplaceAllString(w, "$1 $2"); split != w {
			for _, frag := range strings.Fields(split) {
				add(frag)
			}
		}
	}
	return tokens
}

// bestTermScore returns the highest fuzzy score for term across all tokens, and
// whether term matched any token. Title tokens receive titleMatchBonus so a
// title hit outranks an equivalent URL hit.
func bestTermScore(titleTokens, urlTokens []string, term string) (float64, bool) {
	var (
		best float64
		ok   bool
	)
	consider := func(tokens []string, bonus float64) {
		for _, tok := range tokens {
			if r := fuzzy.Match(tok, term); r.Match {
				s := r.Score + bonus
				if !ok || s > best {
					best, ok = s, true
				}
			}
		}
	}
	consider(titleTokens, titleMatchBonus)
	consider(urlTokens, 0)
	return best, ok
}

// filterTabs returns the tabs matching query, ranked best-first.
//
// Unlike AwGo's built-in Workflow.Filter (a single in-order subsequence match
// against one key, which only sees the tab title), this:
//   - searches the URL as well as the title;
//   - splits the query into whitespace-separated terms and requires *every*
//     term to match, in *any* order (logical AND) — so "Dashboard
//     AccountSettings" works as well as "AccountSettings Dashboard";
//   - matches each term against individual tokens, avoiding false positives
//     from characters scattered across unrelated path segments;
//   - ranks by summed per-term score, with a bonus for title matches.
//
// fuzzy.Match builds a Sorter per call, documented as unsuitable for large
// datasets. Open-tab counts are small (tens, rarely hundreds) and the work is
// O(tabs × terms × tokens), so this stays well within an interactive budget.
func filterTabs(tabs []Tab, query string) []Tab {
	terms := queryTerms(query)
	if len(terms) == 0 {
		// No query: show all tabs, most-recently-accessed first.
		sort.SliceStable(tabs, func(i, j int) bool {
			return tabs[i].LastAccessed > tabs[j].LastAccessed
		})
		return tabs
	}

	type scored struct {
		tab   Tab
		score float64
	}
	var matches []scored
	for _, t := range tabs {
		if s, ok := entryScore(t.Title, t.URL, terms); ok {
			matches = append(matches, scored{t, s})
		}
	}

	// Highest score first; ties broken by most-recently-accessed.
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].tab.LastAccessed > matches[j].tab.LastAccessed
	})

	out := make([]Tab, len(matches))
	for i, m := range matches {
		out[i] = m.tab
	}
	return out
}

// normalizeURL canonicalises a URL for de-duplication across open tabs,
// history and recently-closed tabs: lower-cased, fragment removed, and any
// trailing slash trimmed. It is intentionally lightweight, not a full parse.
func normalizeURL(u string) string {
	s := strings.ToLower(strings.TrimSpace(u))
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimRight(s, "/")
}

// queryTerms splits a user query into match terms on whitespace and the same
// non-word boundaries used to tokenise entries, so a term like "TASK-151538290"
// becomes two terms ("MCM", "151538290") that each match a token, rather than
// one term whose literal hyphen can never appear inside a (split) token.
func queryTerms(query string) []string {
	return strings.Fields(rxNonWord.ReplaceAllString(query, " "))
}

// entryScore scores an entry's title+URL against the given query terms using
// token-level fuzzy matching. It returns the summed score and whether *every*
// term matched at least one token (logical AND, order-independent). This is the
// shared relevance function used to filter tabs and to rank bookmarks, history
// and downloads, so matching behaves consistently across the whole workflow.
func entryScore(title, rawURL string, terms []string) (float64, bool) {
	decoded := rawURL
	if dec, err := url.QueryUnescape(rawURL); err == nil {
		decoded = dec
	}
	titleTokens := tabTokens(title)
	urlTokens := tabTokens(decoded)

	var total float64
	for _, term := range terms {
		s, ok := bestTermScore(titleTokens, urlTokens, term)
		if !ok {
			return 0, false
		}
		total += s
	}
	return total, true
}

// rankBookmarks reorders Firefox's bookmark search results best-match-first for
// query. It only reorders (never drops), so every result Firefox returned —
// including ones matched on metadata we can't see, such as tags — is preserved.
func rankBookmarks(items []Bookmark, query string) []Bookmark {
	terms := queryTerms(query)
	if len(terms) == 0 {
		return items
	}
	scores := make(map[int]float64, len(items))
	for i, b := range items {
		scores[i], _ = entryScore(b.Title, b.URL, terms)
	}
	idx := make([]int, len(items))
	for i := range items {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return scores[idx[a]] > scores[idx[b]] })
	out := make([]Bookmark, len(items))
	for i, j := range idx {
		out[i] = items[j]
	}
	return out
}

// rankHistory reorders history search results best-match-first for query.
func rankHistory(items []History, query string) []History {
	terms := queryTerms(query)
	if len(terms) == 0 {
		// No query yet: browse most-recently-visited first.
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].LastVisitTime > items[j].LastVisitTime
		})
		return items
	}
	scores := make(map[int]float64, len(items))
	for i, h := range items {
		scores[i], _ = entryScore(h.Title, h.URL, terms)
	}
	idx := make([]int, len(items))
	for i := range items {
		idx[i] = i
	}
	// Relevance first; ties broken by most-recently-visited.
	sort.SliceStable(idx, func(a, b int) bool {
		if scores[idx[a]] != scores[idx[b]] {
			return scores[idx[a]] > scores[idx[b]]
		}
		return items[idx[a]].LastVisitTime > items[idx[b]].LastVisitTime
	})
	out := make([]History, len(items))
	for i, j := range idx {
		out[i] = items[j]
	}
	return out
}

// rankDownloads reorders download search results best-match-first for query,
// scoring against the file name and the source URL.
func rankDownloads(items []Download, query string) []Download {
	terms := queryTerms(query)
	if len(terms) == 0 {
		return items
	}
	scores := make(map[int]float64, len(items))
	for i, d := range items {
		scores[i], _ = entryScore(filepath.Base(d.Path), d.Path+" "+d.URL, terms)
	}
	idx := make([]int, len(items))
	for i := range items {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return scores[idx[a]] > scores[idx[b]] })
	out := make([]Download, len(items))
	for i, j := range idx {
		out[i] = items[j]
	}
	return out
}

// filterBookmarklets keeps only bookmarklets, then (if query is non-empty)
// fuzzy-filters and ranks them locally — so the bml keyword can browse all
// bookmarklets with no query and still match by name/URL with one.
func filterBookmarklets(items []Bookmark, query string) []Bookmark {
	var blets []Bookmark
	for _, bm := range items {
		if bm.IsBookmarklet() {
			blets = append(blets, bm)
		}
	}
	terms := queryTerms(query)
	if len(terms) == 0 {
		return blets
	}
	type scored struct {
		bm    Bookmark
		score float64
	}
	var matches []scored
	for _, bm := range blets {
		// Bookmarklet URLs are javascript: code, so match on the name only.
		if s, ok := entryScore(bm.Title, "", terms); ok {
			matches = append(matches, scored{bm, s})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	out := make([]Bookmark, len(matches))
	for i, m := range matches {
		out[i] = m.bm
	}
	return out
}

// connectOrWarn returns a connected RPC client. If the browser/extension can't
// be reached, it shows an actionable Alfred item and returns ok=false instead
// of panicking with a raw error, so the user gets a clear next step.
func connectOrWarn() (*rpcClient, bool) {
	c, err := newClient()
	if err != nil {
		log.Printf("[ERROR] connect to extension: %v", err)
		wf.NewItem("Can’t connect to the browser extension").
			Subtitle("Is Firefox running? Reconnect via ffass ▸ “Reload Extension”, or reopen Firefox").
			Icon(iconError).
			Valid(false)
		wf.NewItem("Reload Extension").
			Subtitle("Re-register and reconnect the workflow with Firefox").
			Autocomplete("workflow:register").
			Icon(iconInstall).
			Valid(false)
		wf.SendFeedback()
		return nil, false
	}
	return c, true
}

var (
	// search history
	historyCmd = &ffcli.Command{
		Name:      "history",
		Usage:     "alfred-firefox -query <query> history",
		ShortHelp: "search browsing history",
		LongHelp:  wrap(`Search browser history.`),
		Exec:      runHistory,
	}

	// search downloads
	downloadsCmd = &ffcli.Command{
		Name:      "downloads",
		Usage:     "alfred-firefox -query <query> downloads",
		ShortHelp: "search downloads",
		LongHelp:  wrap(`Search browser downloads.`),
		Exec:      runDownloads,
	}

	// search bookmarks
	bookmarksCmd = &ffcli.Command{
		Name:      "bookmarks",
		Usage:     "alfred-firefox -query <query> bookmarks",
		ShortHelp: "search bookmarks",
		LongHelp:  wrap(`Search browser bookmarks.`),
		Exec:      runBookmarks,
	}

	// search bookmarklets
	bookmarkletsCmd = &ffcli.Command{
		Name:      "bookmarklets",
		Usage:     "alfred-firefox -query <query> bookmarklets",
		ShortHelp: "search bookmarklets",
		LongHelp:  wrap(`Search bookmarklets and execute in frontmost tab.`),
		Exec:      runBookmarklets,
	}

	/*
		// open URL
		// TODO: is this used? can it be removed?
		openURLCmd = &ffcli.Command{
			Name:      "open-url",
			Usage:     "alfred-firefox -url <url> open-url",
			ShortHelp: "open URL",
			LongHelp:  wrap(`Open specified URL`),
			Exec:      runOpenURL,
		}
	*/

	// execute a bookmarklet in the specified tab
	runBookmarkletCmd = &ffcli.Command{
		Name:      "run-bookmarklet",
		Usage:     "alfred-firefox [-tab <id>] -bookmark <id> run-bookmarklet",
		ShortHelp: "execute bookmarklet in the specified tab",
		LongHelp: wrap(`
			Execute a bookmarklet in a tab. Bookmark ID is required.
			If no tab ID is specified, bookmarklet is run in the active tab.
		`),
		Exec: runBookmarklet,
	}

	// filter open tabs
	tabsCmd = &ffcli.Command{
		Name:      "tabs",
		Usage:     "alfred-firefox [-query <query>] tabs",
		ShortHelp: "filter browser tabs",
		LongHelp:  wrap(`Filter browser tabs and perform actions on them.`),
		Exec:      runTabs,
	}

	// filter tab & URL actions for current tab
	currentTabCmd = &ffcli.Command{
		Name:      "current-tab",
		Usage:     "alfred-firefox [-query <query>] current-tab",
		ShortHelp: "actions for current tab",
		LongHelp:  wrap(`Filter and run actions for current tab`),
		Exec:      runCurrentTab,
	}

	infoFlags = flag.NewFlagSet("tab-info", flag.ExitOnError)
	shellVars bool // export tab info as shell variables
	// export info for current tab
	currentTabInfoCmd = &ffcli.Command{
		Name:      "tab-info",
		Usage:     "alfred-firefox tab-info [-shell]",
		ShortHelp: "export current tab info",
		LongHelp:  wrap(`Export current tab info as variables`),
		FlagSet:   infoFlags,
		Exec:      runCurrentTabInfo,
	}

	// run a tab/URL action for the specified tab
	tabCmd = &ffcli.Command{
		Name:      "tab",
		Usage:     "alfred-firefox [-tab <id>] -action <name> tab",
		ShortHelp: "execute tab action",
		LongHelp: wrap(`
			Execute specified action on tab. Both URL and tab actions
			are available on tabs.
			`),
		Exec: runTabAction,
	}

	// inject JS into the specified tab
	injectCmd = &ffcli.Command{
		Name:      "inject",
		Usage:     "alfred-firefox [-tab <id>] inject <script>",
		ShortHelp: "inject JavaScript into tab",
		LongHelp: wrap(`
			Execute JavaScript in specifed tab and return result as JSON.
			`),
		Exec: runInject,
	}

	// run action for URL
	urlCmd = &ffcli.Command{
		Name:      "url",
		Usage:     "alfred-firefox [-url <url>] -action <name> url",
		ShortHelp: "execute URL action",
		LongHelp:  wrap(`Execute specified action on URL`),
		Exec:      runURLAction,
	}

	// filter URL (and tab) actions
	actionsCmd = &ffcli.Command{
		Name:      "actions",
		Usage:     "alfred-firefox [-tab <id>] [-url <url>] [-query <query>] actions",
		ShortHelp: "filter tab/URL actions",
		LongHelp:  wrap(`View/filter and execute tab/URL actions.`),
		Exec:      runActions,
	}

	// check for update
	updateCmd = &ffcli.Command{
		Name:      "update",
		Usage:     "alfred-firefox update",
		ShortHelp: "check for workflow update",
		LongHelp:  wrap(`Check if newer version of workflow is available.`),
		Exec:      runUpdate,
	}

	// show workflow status
	statusCmd = &ffcli.Command{
		Name:      "options",
		Usage:     "alfred-firefox [-query <query>] options",
		ShortHelp: "show workflow status & options",
		LongHelp:  wrap(`Show workflow status, info and options.`),
		Exec:      runStatus,
	}

	// open file in default application
	openCmd = &ffcli.Command{
		Name:      "open",
		Usage:     "alfred-firefox open <path>",
		ShortHelp: "open file in default application",
		LongHelp:  wrap(`Open file in default application.`),
		Exec:      runOpen,
	}

	// reveal file in Finder
	revealCmd = &ffcli.Command{
		Name:      "reveal",
		Usage:     "alfred-firefox reveal <path>",
		ShortHelp: "reveal file in Finder",
		LongHelp:  wrap(`Reveal file in Finder.`),
		Exec:      runReveal,
	}
)

func init() {
	infoFlags.BoolVar(&shellVars, "shell", false, "export shell variables")
}

// func runOpenURL(_ []string) error {
// 	wf.Configure(aw.TextErrors(true))
// 	log.Printf("opening URL %q ...", URL)
// 	_, err := util.RunCmd(exec.Command("open", URL))
// 	return err
// }

// search Firefox history
func runHistory(_ []string) error {
	checkForUpdate()
	log.Printf("searching history for %q ...", query)
	c, ok := connectOrWarn()
	if !ok {
		return nil
	}
	history, err := c.History(query)
	if err != nil {
		return err
	}
	history = rankHistory(history, query)

	custom := loadCustomActions()
	for _, h := range history {
		sub := h.URL
		if rel := relativeTime(h.LastVisitTime); rel != "" {
			sub = "visited " + rel + "  ·  " + h.URL
		}
		it := wf.NewItem(h.Title).
			Subtitle(sub).
			Arg(h.URL).
			UID(h.ID).
			Valid(true).
			Icon(iconHistory).
			Var("CMD", "url").
			Var("ACTION", urlDefault).
			Var("URL", h.URL).
			Var("TITLE", h.Title)

		it.NewModifier(aw.ModCmd).
			Subtitle("Other Actions…").
			Arg("").
			Icon(iconMore).
			Var("CMD", "actions")

		custom.Add(it, false)
	}

	wf.WarnEmpty("No History", "Type to search your browsing history")
	wf.SendFeedback()
	return nil
}

// search Firefox bookmarks
func runBookmarks(_ []string) error {
	checkForUpdate()
	if len(query) < 3 {
		wf.Warn("Query Too Short", "Please enter at least 3 characters")
		return nil
	}

	log.Printf("searching bookmarks for %q ...", query)
	c, ok := connectOrWarn()
	if !ok {
		return nil
	}
	bookmarks, err := c.Bookmarks(query)
	if err != nil {
		return err
	}
	bookmarks = rankBookmarks(bookmarks, query)

	custom := loadCustomActions()
	for _, bm := range bookmarks {
		if bm.IsBookmarklet() {
			continue
		}
		it := wf.NewItem(bm.Title).
			Subtitle(bm.URL).
			Arg(bm.URL).
			UID(bm.ID).
			Valid(true).
			Icon(iconBookmark).
			Var("CMD", "url").
			Var("ACTION", urlDefault).
			Var("URL", bm.URL).
			Var("TITLE", bm.Title)

		it.NewModifier(aw.ModCmd).
			Subtitle("Other Actions…").
			Arg("").
			Icon(iconMore).
			Var("CMD", "actions")

		custom.Add(it, false)
	}

	wf.WarnEmpty("No Results", "Try a different query?")
	wf.SendFeedback()
	return nil
}

// search Firefox bookmarklets
func runBookmarklets(_ []string) error {
	checkForUpdate()
	log.Printf("searching bookmarklets for %q ...", query)
	c, ok := connectOrWarn()
	if !ok {
		return nil
	}
	// Fetch all bookmarks (Firefox can't search by the javascript: scheme), then
	// keep and rank bookmarklets locally. This lets `bml` browse every
	// bookmarklet with no query, instead of requiring 3+ characters.
	bookmarks, err := c.Bookmarks("")
	if err != nil {
		return err
	}
	bookmarks = filterBookmarklets(bookmarks, query)

	for _, bm := range bookmarks {
		wf.NewItem(bm.Title).
			Subtitle("↩ to execute in current tab").
			UID(bm.ID).
			Copytext("bml:"+bm.ID+","+bm.Title).
			Arg(bm.URL).
			Icon(iconBookmarklet).
			Valid(true).
			Var("CMD", "run-bookmarklet").
			Var("BOOKMARK", bm.ID)
	}

	wf.WarnEmpty("No Results", "Try a different query?")
	wf.SendFeedback()
	return nil
}

// execute a bookmarklet in a tab
func runBookmarklet(_ []string) error {
	wf.Configure(aw.TextErrors(true))
	log.Printf("running bookmarklet %q in tab #%d ...", bookmarkID, tabID)

	return mustClient().
		RunBookmarklet(RunBookmarkletArg{BookmarkID: bookmarkID, TabID: tabID})
}

// filter open Firefox tabs
func runTabs(_ []string) error {
	log.Printf("fetching tabs for query %q ...", query)
	checkForUpdate()

	// Suppress item UIDs so Alfred does NOT reorder results by how often each
	// is picked ("knowledge"). The tab list must reflect *our* ordering:
	// recency when there's no query, relevance (then recency) when searching.
	wf.Configure(aw.SuppressUIDs(true))

	c, ok := connectOrWarn()
	if !ok {
		return nil
	}
	tabs, err := c.Tabs()
	if err != nil {
		return err
	}

	custom := loadCustomActions()
	seen := map[string]bool{} // normalised URLs already shown, for de-duplication

	// 1) Open tabs matching the query (ranked, URL-aware, order-independent).
	//    With an empty query, all tabs are shown most-recently-used first.
	for _, t := range filterTabs(tabs, query) {
		seen[normalizeURL(t.URL)] = true
		id := fmt.Sprintf("%d", t.ID)
		it := wf.NewItem(t.Title).
			Subtitle(tabSubtitle(t)).
			Arg(t.URL).
			UID(t.Title).
			Valid(true).
			Icon(tabIcon(t)).
			Var("CMD", "tab").
			Var("ACTION", "Activate Tab").
			Var("TAB", id).
			Var("URL", t.URL).
			Var("TITLE", t.Title)

		it.NewModifier(aw.ModCmd).
			Subtitle("Other Actions…").
			Arg("").
			Icon(iconMore).
			Var("CMD", "actions")

		custom.Add(it, true)
	}

	// Once there's a usable query, fold in matching history and recently-closed
	// tabs so `tab` becomes a single switch-or-reopen launcher.
	if len([]rune(strings.TrimSpace(query))) >= augmentMinQueryLen {
		addHistoryResults(c, query, seen, custom)
		addRecentlyClosedResults(c, query, seen)
	}

	wf.WarnEmpty("No Matching Tabs", "Try a different query, or use hist/bm")
	wf.SendFeedback()
	return nil
}

// tabIcon returns the open-tab icon tinted with the tab's group colour (if the
// tab is in a group and the colour is known), else the default blue open icon.
func tabIcon(t Tab) *aw.Icon {
	if t.GroupColor != "" {
		if icon, ok := tabGroupIcons[strings.ToLower(t.GroupColor)]; ok {
			return icon
		}
	}
	return iconTabOpen
}

// tabSubtitle builds a tab's subtitle: pinned/audio indicators, then the group
// name in brackets (if any), then the URL, then a "last opened" watermark.
// Group/indicator/recency data is only present with the upgraded extension.
func tabSubtitle(t Tab) string {
	var b strings.Builder
	if rel := relativeTime(t.LastAccessed); rel != "" {
		b.WriteString(rel + "  ·  ")
	}
	if t.Pinned {
		b.WriteString("📌 ")
	}
	if t.Muted {
		b.WriteString("🔇 ")
	} else if t.Audible {
		b.WriteString("🔊 ")
	}
	if t.GroupTitle != "" {
		b.WriteString("[" + t.GroupTitle + "] ")
	}
	b.WriteString(t.URL)
	return b.String()
}

// relativeTime renders a millisecond epoch timestamp as a short "… ago"
// watermark, or "" if unknown (ts <= 0).
func relativeTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	d := time.Since(time.UnixMilli(ms))
	if d < 0 {
		d = 0
	}
	switch {
	case d < 45*time.Second:
		return "just now"
	case d < 60*time.Minute:
		m := int(d.Minutes())
		if m < 1 {
			m = 1
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 8*7*24*time.Hour:
		return fmt.Sprintf("%dw ago", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/(24*365)))
	}
}

const (
	augmentMinQueryLen = 3  // min query length before history is added
	maxHistoryResults  = 15 // cap on history items appended to tab search
	maxRecentlyClosed  = 8  // cap on recently-closed items appended
)

// addHistoryResults appends history entries matching query (ranked, capped,
// de-duplicated against already-shown URLs) as "reopen" items.
func addHistoryResults(c *rpcClient, query string, seen map[string]bool, custom customActions) {
	history, err := c.History(query)
	if err != nil {
		log.Printf("[WARN] history lookup failed: %v", err)
		return
	}
	terms := queryTerms(query)
	type scored struct {
		h     History
		score float64
	}
	var matches []scored
	for _, h := range history {
		if seen[normalizeURL(h.URL)] {
			continue
		}
		if s, ok := entryScore(h.Title, h.URL, terms); ok {
			matches = append(matches, scored{h, s})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].h.LastVisitTime > matches[j].h.LastVisitTime
	})

	n := 0
	for _, m := range matches {
		if n >= maxHistoryResults {
			break
		}
		nu := normalizeURL(m.h.URL)
		if seen[nu] {
			continue
		}
		seen[nu] = true
		n++
		sub := "↩ history · " + m.h.URL
		if rel := relativeTime(m.h.LastVisitTime); rel != "" {
			sub = "visited " + rel + "  ·  ↩ history · " + m.h.URL
		}
		it := wf.NewItem(m.h.Title).
			Subtitle(sub).
			Arg(m.h.URL).
			UID("hist:"+m.h.ID).
			Valid(true).
			Icon(iconHistoryReopen).
			Var("CMD", "url").
			Var("ACTION", urlDefault).
			Var("URL", m.h.URL).
			Var("TITLE", m.h.Title)
		it.NewModifier(aw.ModCmd).
			Subtitle("Other Actions…").
			Arg("").
			Icon(iconMore).
			Var("CMD", "actions")
		custom.Add(it, false)
	}
}

// addRecentlyClosedResults appends recently-closed tabs matching query as
// "reopen" items. Requires the upgraded extension; on older extensions the
// RPC errors and this is silently skipped.
func addRecentlyClosedResults(c *rpcClient, query string, seen map[string]bool) {
	closed, err := c.RecentlyClosed()
	if err != nil {
		log.Printf("[INFO] recently-closed unavailable (older extension?): %v", err)
		return
	}
	terms := queryTerms(query)
	n := 0
	for _, t := range closed { // already most-recently-closed first
		if n >= maxRecentlyClosed {
			break
		}
		if t.URL == "" {
			continue
		}
		nu := normalizeURL(t.URL)
		if seen[nu] {
			continue
		}
		if _, ok := entryScore(t.Title, t.URL, terms); !ok {
			continue
		}
		seen[nu] = true
		n++
		sub := "↩ closed tab · " + t.URL
		if rel := relativeTime(t.LastModified); rel != "" {
			sub = "closed " + rel + "  ·  ↩ closed tab · " + t.URL
		}
		wf.NewItem(t.Title).
			Subtitle(sub).
			Arg(t.URL).
			UID("closed:"+t.URL).
			Valid(true).
			Icon(iconHistoryReopen).
			Var("CMD", "url").
			Var("ACTION", urlDefault).
			Var("URL", t.URL).
			Var("TITLE", t.Title)
	}
}

// execute a tab or URL action on the given tab
func runTabAction(_ []string) error {
	wf.Configure(aw.TextErrors(true))
	// load tab info so we can also run URL actions
	tab, err := mustClient().Tab(tabID)
	if err != nil {
		return err
	}

	log.Printf("running action %q on tab #%d ...", action, tab.ID)
	if a, ok := tabActions[action]; ok {
		return a.Run(tabID)
	}
	if a, ok := urlActions[action]; ok {
		return a.Run(tab.URL)
	}
	return fmt.Errorf("unknown action %q", action)
}

// run an action on a URL
func runURLAction(_ []string) error {
	_ = wf.Configure(aw.TextErrors(true))
	if URL == "" {
		tab, err := mustClient().Tab(0)
		if err != nil {
			return err
		}
		URL = tab.URL
	}
	log.Printf("running action %q on URL %q ...", action, URL)
	a, ok := urlActions[action]
	if !ok {
		return fmt.Errorf("unknown action %q", action)
	}
	return a.Run(URL)
}

// export variables containing info for currently-active tab
func runCurrentTabInfo(_ []string) error {
	_ = wf.Configure(aw.TextErrors(true))
	tab, err := mustClient().Tab(0)
	if err != nil {
		return err
	}
	if shellVars {
		fmt.Printf("export FF_TAB=%d\n", tab.ID)
		fmt.Printf("export FF_WINDOW=%d\n", tab.WindowID)
		fmt.Printf("export FF_INDEX=%d\n", tab.Index)
		fmt.Printf("export FF_TITLE=\"%s\"\n", tab.Title)
		fmt.Printf("export FF_URL=\"%s\"\n", tab.URL)
		return nil
	}
	av := aw.NewArgVars().
		Var("FF_TAB", fmt.Sprintf("%d", tab.ID)).
		Var("FF_WINDOW", fmt.Sprintf("%d", tab.WindowID)).
		Var("FF_INDEX", fmt.Sprintf("%d", tab.Index)).
		Var("FF_TITLE", tab.Title).
		Var("FF_URL", tab.URL)
	return av.Send()
}

// show actions for currently-active tab
func runCurrentTab(_ []string) error {
	tab, err := mustClient().Tab(0)
	if err != nil {
		return err
	}
	tabID = tab.ID
	URL = tab.URL
	return runActions([]string{})
}

// inject JavaScript into specified tab. If tabID is 0, JS in injected
// into the active tab.
func runInject(args []string) error {
	_ = wf.Configure(aw.TextErrors(true))
	if len(args) != 1 {
		return fmt.Errorf("inject command takes 1 argument, not %d", len(args))
	}
	js, err := mustClient().RunJS(RunJSArg{TabID: tabID, JS: args[0]})
	if err != nil {
		return err
	}
	fmt.Print(js)
	return nil
}

// filter actions for tab or URL
func runActions(_ []string) error {
	// Carry the originating item's title through to action items so title-aware
	// URL actions (e.g. "Copy Link as Markdown") have it available.
	title := os.Getenv("TITLE")

	if tabID != 0 {
		for _, name := range sortedTabActionNames() {
			a := tabActions[name]
			wf.NewItem(a.Name()).
				UID(a.Name()).
				Copytext(a.Name()).
				Icon(a.Icon()).
				Valid(true).
				Var("CMD", "tab").
				Var("ACTION", a.Name()).
				Var("TAB", fmt.Sprintf("%d", tabID)).
				Var("URL", URL).
				Var("TITLE", title)
		}

		// add custom bookmarklet commands
		for _, a := range loadCustomActions() {
			if a.kind != "bookmarklet" {
				continue
			}
			wf.NewItem(a.name).
				UID(a.id).
				Copytext("bml:"+a.id+","+a.name).
				Icon(actionIcon(a.name, iconBookmarklet)).
				Valid(true).
				Var("CMD", "run-bookmarklet").
				Var("BOOKMARK", a.id).
				Var("TAB", fmt.Sprintf("%d", tabID))
		}
	}

	if URL != "" {
		for _, name := range sortedURLActionNames() {
			if name == urlDefault {
				continue
			}
			a := urlActions[name]
			wf.NewItem(a.Name()).
				UID(a.Name()).
				Copytext(a.Name()).
				Icon(a.Icon()).
				Valid(true).
				Var("CMD", "url").
				Var("ACTION", a.Name()).
				Var("URL", URL).
				Var("TITLE", title)
		}
	}

	if query != "" {
		_ = wf.Filter(query)
	}

	wf.WarnEmpty("No Matching Actions", "Try a different query?")
	wf.SendFeedback()
	return nil
}

// sortedTabActionNames returns tab action names in stable alphabetical order.
func sortedTabActionNames() []string {
	names := make([]string, 0, len(tabActions))
	for name := range tabActions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// sortedURLActionNames returns URL action names in stable alphabetical order.
func sortedURLActionNames() []string {
	names := make([]string, 0, len(urlActions))
	for name := range urlActions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// check if a newer version of workflow is available
func runUpdate(_ []string) error {
	wf.Configure(aw.TextErrors(true))
	// Auto-update is intentionally disabled: this is a manually maintained
	// fork, so there is no Updater configured. Nothing to check.
	if !wf.UpdateCheckDue() {
		log.Print("auto-update disabled (manually maintained fork)")
		return nil
	}
	if err := wf.CheckForUpdate(); err != nil {
		return err
	}
	if wf.UpdateAvailable() {
		log.Println("a newer version of the workflow is available")
	}
	return nil
}

// show workflow status and options
func runStatus(_ []string) error {
	if c, err := newClient(); err != nil {
		wf.NewItem("No Connection to Browser").
			Subtitle(err.Error()).
			Icon(iconError)
	} else {
		if err := c.Ping(); err != nil {
			wf.NewItem("No Connection to Browser").
				Subtitle(err.Error()).
				Icon(iconError)

		} else {
			wf.NewItem("Connected to Browser").
				Subtitle("Extension is installed and running")
		}
	}

	wf.NewItem("Register Workflow with Browser").
		Subtitle("Use if you've updated or moved the workflow and it isn't working").
		Autocomplete("workflow:register").
		Icon(iconInstall).
		Valid(false)

	wf.NewItem("Browser Extension").
		Subtitle("Install the signed .xpi provided by the maintainer (not the public add-on)").
		Icon(iconAddon).
		Valid(false)

	wf.NewItem("Updates Managed Manually").
		Subtitle("This is a maintained fork — get new versions from the maintainer").
		Icon(iconUpdateOK).
		Valid(false)

	dir := filepath.Join(wf.DataDir(), "scripts")
	wf.NewItem("Open Scripts Directory").
		Subtitle("Open custom scripts directory in Finder").
		Arg(dir).
		Valid(true).
		Icon(iconScript).
		Var("CMD", "url").
		Var("ACTION", "Open in Default Application").
		Var("URL", dir)

	wf.NewItem("Documentation (upstream base commands)").
		Subtitle("Opens the original project docs — fork features are documented separately").
		Arg(docsURL).
		Valid(true).
		Icon(iconDocs).
		Var("CMD", "url").
		Var("ACTION", urlDefault).
		Var("URL", docsURL)

	wf.NewItem("Report Issue").
		Subtitle("Contact the workflow maintainer (this is a maintained fork)").
		Icon(iconIssue).
		Valid(false)

	if query != "" {
		wf.Filter(query)
	}

	wf.WarnEmpty("No Matching Items", "Try a different query?")
	wf.SendFeedback()
	return nil
}

func runDownloads(_ []string) error {
	log.Printf("searching downloads for %q ...", query)
	c, ok := connectOrWarn()
	if !ok {
		return nil
	}
	downloads, err := c.Downloads(query)
	if err != nil {
		return err
	}
	downloads = rankDownloads(downloads, query)

	for _, dl := range downloads {
		wf.NewItem(filepath.Base(dl.Path)).
			Subtitle(util.PrettyPath(dl.Path)).
			Arg(dl.Path).
			UID(dl.Path).
			IsFile(true).
			Icon(&aw.Icon{Value: dl.Path, Type: aw.IconTypeFileIcon}).
			Valid(true).
			Var("CMD", "open").
			NewModifier(aw.ModCmd).
			Subtitle("Reveal in Finder").
			Var("CMD", "reveal")
	}

	wf.WarnEmpty("Nothing Found", "Try a different query?")
	wf.SendFeedback()
	return nil
}

// open file in default application
func runOpen(args []string) error {
	path := args[0]
	log.Printf("opening file %q ...", util.PrettyPath(path))
	return exec.Command("/usr/bin/open", path).Run()
}

// reveal file in Finder
func runReveal(args []string) error {
	path := args[0]
	log.Printf("revealing file %q in Finder ...", util.PrettyPath(path))
	return exec.Command("/usr/bin/open", "-R", path).Run()
}

// run update check in background
func checkForUpdate() {
	if wf.UpdateCheckDue() && !wf.IsRunning("update") {
		wf.RunInBackground("update", exec.Command(os.Args[0], "update"))
	}
	// TODO: show "update available" message
}
