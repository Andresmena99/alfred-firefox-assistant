// Copyright (c) 2026 Andres Mena Godino
// MIT Licence applies http://opensource.org/licenses/MIT
//
// Tab-group search & switching. Lets you type `tg [query]` in Alfred to browse
// Firefox tab groups (Firefox 139+), fuzzy-search them by name *or* by the
// content of their member tabs, and switch to one on ↩ — expanding it if it is
// collapsed and focusing its most-recently-used tab.
//
// Two data paths back this feature so it degrades gracefully:
//
//   - extension v1.4.0+ answers "all-tab-groups" with the authoritative group
//     list, including each group's collapsed state and pre-computed aggregates;
//   - against an older extension the RPC errors and we reconstruct the group
//     list from the per-tab group fields already present on Tabs() (v1.3.0+),
//     losing only the collapsed indicator.
//
// Either way we always fetch Tabs() as well, because the member tabs drive
// content-search and are the fallback source for the group list.

package main

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	aw "github.com/deanishe/awgo"
	"github.com/deanishe/awgo/util"
	"github.com/peterbourgon/ff/ffcli"
)

var (
	// browse/search tab groups
	tabGroupsCmd = &ffcli.Command{
		Name:      "tab-groups",
		Usage:     "alfred-firefox [-query <query>] tab-groups",
		ShortHelp: "search Firefox tab groups",
		LongHelp: wrap(`
			Browse and search Firefox tab groups. With no query, groups are listed
			most-recently-used first. Type to fuzzy-match a group by its name or by
			the contents of its tabs. ↩ switches to the group, expanding it if it is
			collapsed.
			`),
		Exec: runTabGroups,
	}

	// switch to a tab group by ID
	tabGroupCmd = &ffcli.Command{
		Name:      "tabgroup",
		Usage:     "alfred-firefox tabgroup <group-id>",
		ShortHelp: "switch to a tab group",
		LongHelp: wrap(`
			Switch to the tab group with the given ID: focus its window, expand it if
			collapsed, and activate its most-recently-used tab.
			`),
		Exec: runTabGroupAction,
	}
)

// deriveTabGroups reconstructs the tab-group list from the per-tab group fields
// returned by "all-tabs", and returns a map of group ID to its member tabs.
//
// It is both the fallback for older extensions (which can't answer
// "all-tab-groups") and the source of member tabs used for content-search and
// for choosing which tab to activate. Groups are aggregated in a single pass:
//   - TabCount   — number of member tabs;
//   - LastAccessed / ActiveTabID — from the group's most-recently-used tab;
//   - MinIndex   — the group's leftmost position in the tab strip.
//
// Collapsed is left unknown here (CollapsedKnown=false); only the extension can
// report it.
func deriveTabGroups(tabs []Tab) ([]TabGroup, map[int][]Tab) {
	members := map[int][]Tab{}
	byID := map[int]*TabGroup{}
	var order []int // preserve first-seen order for stable output

	for _, t := range tabs {
		if t.GroupID == ungroupedID {
			continue
		}
		members[t.GroupID] = append(members[t.GroupID], t)

		g, ok := byID[t.GroupID]
		if !ok {
			g = &TabGroup{ID: t.GroupID, WindowID: t.WindowID, MinIndex: t.Index}
			byID[t.GroupID] = g
			order = append(order, t.GroupID)
		}
		g.TabCount++
		// The group's name/colour is carried on every member tab; take the first
		// non-empty value we see so an odd tab with blank group metadata can't
		// wipe it out.
		if g.Title == "" && t.GroupTitle != "" {
			g.Title = t.GroupTitle
		}
		if g.Color == "" && t.GroupColor != "" {
			g.Color = t.GroupColor
		}
		if t.LastAccessed >= g.LastAccessed {
			g.LastAccessed = t.LastAccessed
			g.ActiveTabID = t.ID
		}
		if t.Index < g.MinIndex {
			g.MinIndex = t.Index
		}
	}

	groups := make([]TabGroup, 0, len(order))
	for _, id := range order {
		groups = append(groups, *byID[id])
	}
	return groups, members
}

// loadTabGroups returns the tab groups and a map of group ID to member tabs.
//
// It prefers the extension's authoritative "all-tab-groups" (which knows the
// collapsed state), but always uses Tabs() for the member map, and falls back
// to deriving the whole list from tabs when the extension is too old. When the
// extension list is used, member-derived aggregates (tab count, recency, index)
// are merged in so ranking and subtitles are populated even if the extension
// ever reports zeroes.
func loadTabGroups(c *rpcClient) ([]TabGroup, map[int][]Tab, error) {
	tabs, err := c.Tabs()
	if err != nil {
		return nil, nil, err
	}
	derived, members := deriveTabGroups(tabs)

	groups, err := c.TabGroups()
	if err != nil {
		// Older extension (pre-1.4.0): derive everything from tabs. Collapsed
		// state is unavailable, which the UI handles gracefully.
		log.Printf("[INFO] all-tab-groups unavailable (older extension?): %v", err)
		return derived, members, nil
	}

	// Extension list is authoritative for identity + collapsed state. Backfill
	// aggregates from the member map where the extension left them at zero.
	byID := make(map[int]TabGroup, len(derived))
	for _, g := range derived {
		byID[g.ID] = g
	}
	for i := range groups {
		g := &groups[i]
		d, ok := byID[g.ID]
		if !ok {
			continue
		}
		if g.TabCount == 0 {
			g.TabCount = d.TabCount
		}
		if g.LastAccessed == 0 {
			g.LastAccessed = d.LastAccessed
		}
		if g.ActiveTabID == 0 {
			g.ActiveTabID = d.ActiveTabID
		}
		if g.MinIndex == 0 {
			g.MinIndex = d.MinIndex
		}
	}
	return groups, members, nil
}

// groupContentTokens returns the de-duplicated set of match tokens drawn from a
// group's member tabs (each tab's title and URL). These feed content-search so
// a group can be found by what is inside it, not just its name.
func groupContentTokens(tabs []Tab) []string {
	seen := map[string]bool{}
	var toks []string
	for _, t := range tabs {
		for _, tok := range tabTokens(t.Title + " " + t.URL) {
			k := strings.ToLower(tok)
			if !seen[k] {
				seen[k] = true
				toks = append(toks, tok)
			}
		}
	}
	return toks
}

// groupScore scores a group against the query terms. A term may match the
// group's name (which carries titleMatchBonus, so a name hit outranks a content
// hit) or any token from its member tabs. Every term must match at least one of
// the two (logical AND, order-independent), mirroring filterTabs. Returns the
// summed score and whether the group matched.
func groupScore(name string, contentTokens []string, terms []string) (float64, bool) {
	nameTokens := tabTokens(name)
	var total float64
	for _, term := range terms {
		s, ok := bestTermScore(nameTokens, contentTokens, term)
		if !ok {
			return 0, false
		}
		total += s
	}
	return total, true
}

// filterTabGroups returns the groups matching query, ranked best-first. With no
// query, groups are ordered most-recently-used first (tie-break: tab-strip
// position). With a query, relevance wins and recency breaks ties.
func filterTabGroups(groups []TabGroup, members map[int][]Tab, query string) []TabGroup {
	terms := queryTerms(query)
	if len(terms) == 0 {
		out := append([]TabGroup(nil), groups...)
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].LastAccessed != out[j].LastAccessed {
				return out[i].LastAccessed > out[j].LastAccessed
			}
			return out[i].MinIndex < out[j].MinIndex
		})
		return out
	}

	type scored struct {
		group TabGroup
		score float64
	}
	var matches []scored
	for _, g := range groups {
		content := groupContentTokens(members[g.ID])
		if s, ok := groupScore(g.Name(), content, terms); ok {
			matches = append(matches, scored{g, s})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].group.LastAccessed > matches[j].group.LastAccessed
	})

	out := make([]TabGroup, len(matches))
	for i, m := range matches {
		out[i] = m.group
	}
	return out
}

// groupIcon returns the tab-group icon tinted with the group's colour, or a
// neutral fallback when the colour is unknown/unmapped.
func groupIcon(g TabGroup) *aw.Icon {
	if g.Color != "" {
		if icon, ok := tabGroupIcons[strings.ToLower(g.Color)]; ok {
			return icon
		}
	}
	return iconTab
}

// groupSubtitle builds a group's subtitle: a "last used" watermark, the tab
// count, and — when known — a collapsed indicator.
func groupSubtitle(g TabGroup) string {
	var parts []string
	if rel := relativeTime(g.LastAccessed); rel != "" {
		parts = append(parts, rel)
	}
	n := "tabs"
	if g.TabCount == 1 {
		n = "tab"
	}
	parts = append(parts, fmt.Sprintf("%d %s", g.TabCount, n))
	if g.CollapsedKnown && g.Collapsed {
		parts = append(parts, "⊟ collapsed")
	}
	parts = append(parts, "↩ switch to group")
	return strings.Join(parts, "  ·  ")
}

// runTabGroups lists/searches Firefox tab groups.
func runTabGroups(_ []string) error {
	log.Printf("fetching tab groups for query %q ...", query)
	checkForUpdate()

	// Suppress UIDs so Alfred keeps *our* ordering (recency / relevance) instead
	// of re-sorting by how often each item is picked — same rationale as tabs.
	wf.Configure(aw.SuppressUIDs(true))

	c, ok := connectOrWarn()
	if !ok {
		return nil
	}

	groups, members, err := loadTabGroups(c)
	if err != nil {
		return err
	}

	for _, g := range filterTabGroups(groups, members, query) {
		it := wf.NewItem(g.Name()).
			Subtitle(groupSubtitle(g)).
			Arg(strconv.Itoa(g.ID)).
			UID(fmt.Sprintf("group:%d", g.ID)).
			Valid(true).
			Icon(groupIcon(g)).
			Var("CMD", "tabgroup").
			Var("GROUP", strconv.Itoa(g.ID)).
			Var("GROUP_NAME", g.Name())

		// ⌘C copies the group name for convenience.
		it.Copytext(g.Name())
	}

	if len(groups) == 0 {
		wf.WarnEmpty("No Tab Groups", "Create a tab group in Firefox, or use tab/hist/bm")
	} else {
		wf.WarnEmpty("No Matching Tab Groups", "Try a different query")
	}
	wf.SendFeedback()
	return nil
}

// mruTabInGroup returns the most-recently-used tab in the given group. It is
// the fallback target for switching to a group when the extension/native host
// is too old to handle activate-tab-group directly.
func mruTabInGroup(c *rpcClient, groupID int) (Tab, bool) {
	tabs, err := c.Tabs()
	if err != nil {
		log.Printf("[WARN] could not list tabs for group fallback: %v", err)
		return Tab{}, false
	}
	var best Tab
	found := false
	for _, t := range tabs {
		if t.GroupID != groupID {
			continue
		}
		if !found || t.LastAccessed > best.LastAccessed {
			best, found = t, true
		}
	}
	return best, found
}

// runTabGroupAction switches to the tab group whose ID is given as the sole
// argument. It focuses Firefox and asks the extension to expand + activate the
// group.
//
// If the extension (or the running native host) predates the group commands,
// ActivateTabGroup fails; we then fall back to activating the group's
// most-recently-used tab via the long-supported ActivateTab RPC. That still
// switches to the group — it just can't expand a collapsed group — so switching
// works immediately, before the upgraded extension is installed.
func runTabGroupAction(args []string) error {
	wf.Configure(aw.TextErrors(true))
	if len(args) != 1 {
		return fmt.Errorf("tabgroup command takes 1 argument (group id), not %d", len(args))
	}
	gid, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil {
		return fmt.Errorf("invalid group id %q: %w", args[0], err)
	}

	c := mustClient()
	// Bring Firefox to the front, mirroring the "Activate Tab" action, so the
	// group is visible immediately after switching.
	if _, err := util.RunAS(fmt.Sprintf(`tell application "%s" to activate`, c.appName)); err != nil {
		log.Printf("[WARN] could not activate %s: %v", c.appName, err)
	}

	log.Printf("switching to tab group #%d ...", gid)
	if err := c.ActivateTabGroup(gid); err != nil {
		log.Printf("[INFO] activate-tab-group unavailable (older extension?): %v; falling back to MRU tab", err)
		if t, ok := mruTabInGroup(c, gid); ok {
			return c.ActivateTab(t.ID)
		}
		return err
	}
	return nil
}
