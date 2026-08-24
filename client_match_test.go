// Copyright (c) 2024-2026 Andres Mena Godino
// MIT Licence applies http://opensource.org/licenses/MIT

package main

import (
	"testing"
	"time"
)

// tabsFixture is a representative set of open tabs used by the filter tests.
func tabsFixture() []Tab {
	return []Tab{
		{ID: 1, Title: "Dashboard", URL: "https://wiki.example.com/docs/SettingsPageUI/AccountSettings/Report/Dashboard"},
		{ID: 2, Title: "Acme Account Home", URL: "https://acme.example.com/account"},
		{ID: 3, Title: "Report Overview", URL: "https://wiki.example.com/docs/SettingsPageUI/Report"},
		{ID: 4, Title: "Unrelated Wiki", URL: "https://wiki.example.com/docs/SomethingElse/Notes"},
		{ID: 5, Title: "Execute TASK-151538290 Step", URL: "https://tracker.example.com/tasks/TASK-151538290/run/80354499-e3f2-4636-b6ee-83b79a9da019"},
	}
}

func idsOf(tabs []Tab) []int {
	ids := make([]int, len(tabs))
	for i, t := range tabs {
		ids[i] = t.ID
	}
	return ids
}

func contains(ids []int, want int) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// TestFilterTabsMatching covers order-independence (logical AND in any order),
// URL searchability, CamelCase/acronym matching, and precise exclusion of
// tabs that don't contain every term.
func TestFilterTabsMatching(t *testing.T) {
	tabs := tabsFixture()

	cases := []struct {
		query   string
		wantIDs []int // expected matching tab IDs (order-insensitive here)
	}{
		{"AccountSettings Dashboard", []int{1}},  // originally-reported failing query
		{"Dashboard AccountSettings", []int{1}},  // reversed order must also work
		{"Account Settings Dashboard", []int{1}}, // CamelCase split form
		{"dash account", []int{1}},               // partial words, reversed
		{"spui", []int{1, 3}},                    // acronym -> SettingsPageUI (tabs 1 & 3)
		{"report", []int{1, 3}},                  // "Report" in both
		{"report dashboard", []int{1}},           // only tab 1 has a "dashboard" token
		{"account settings", []int{1}},           // tab 2 has no "settings" token
		{"TASK-151538290", []int{5}},             // hyphenated identifier (the reported bug)
		{"task 151538290", []int{5}},             // same, space-separated
		{"151538290", []int{5}},                  // bare number
		{"qzx nonexistent", []int{}},             // matches nothing
	}

	for _, c := range cases {
		got := idsOf(filterTabs(tabs, c.query))
		if len(got) != len(c.wantIDs) {
			t.Errorf("query %q: got %v, want %v", c.query, got, c.wantIDs)
			continue
		}
		for _, id := range c.wantIDs {
			if !contains(got, id) {
				t.Errorf("query %q: expected tab %d in %v", c.query, id, got)
			}
		}
	}
}

// TestFilterTabsTitleBonus verifies that a tab matching a term in its TITLE
// ranks above a tab matching the same term only in its URL — even when the
// title-matching tab appears later in the input.
func TestFilterTabsTitleBonus(t *testing.T) {
	tabs := tabsFixture()
	// "report" is tab 3's title ("Report Overview") but only a URL path segment
	// in tab 1. Tab 3 should rank first despite appearing later in the slice.
	got := idsOf(filterTabs(tabs, "report"))
	if len(got) != 2 {
		t.Fatalf("query %q: got %v, want 2 results", "report", got)
	}
	if got[0] != 3 {
		t.Errorf("query %q: expected tab 3 (title match) first, got order %v", "report", got)
	}
}

// TestFilterTabsEmptyQuery verifies an empty/whitespace query returns all tabs.
func TestFilterTabsEmptyQuery(t *testing.T) {
	tabs := tabsFixture()
	if got := filterTabs(tabs, ""); len(got) != len(tabs) {
		t.Errorf("empty query: expected all %d tabs, got %d", len(tabs), len(got))
	}
	if got := filterTabs(tabs, "   "); len(got) != len(tabs) {
		t.Errorf("whitespace query: expected all %d tabs, got %d", len(tabs), len(got))
	}
}

// TestRankBookmarks verifies relevance reordering without dropping any result
// (recall is preserved because Firefox may have matched on hidden metadata).
func TestRankBookmarks(t *testing.T) {
	in := []Bookmark{
		{ID: "a", Title: "Random Notes", URL: "https://example.com/notes"},
		{ID: "b", Title: "GitHub Actions Docs", URL: "https://docs.github.com/actions"},
		{ID: "c", Title: "Other Page", URL: "https://other.com"},
	}
	out := rankBookmarks(in, "github actions")
	if len(out) != len(in) {
		t.Fatalf("rankBookmarks dropped results: got %d, want %d", len(out), len(in))
	}
	if out[0].ID != "b" {
		t.Errorf("expected best match (b) first, got %q", out[0].ID)
	}
}

// TestFilterBookmarklets verifies only bookmarklets are kept and that the query
// filters/ranks them (while an empty query lists them all).
func TestFilterBookmarklets(t *testing.T) {
	in := []Bookmark{
		{ID: "1", Title: "Copy Title", URL: "javascript:void(document.title)"},
		{ID: "2", Title: "Normal Bookmark", URL: "https://example.com"},
		{ID: "3", Title: "Reader Mode", URL: "javascript:void(0)"},
	}
	if all := filterBookmarklets(in, ""); len(all) != 2 {
		t.Errorf("empty query: expected 2 bookmarklets, got %d", len(all))
	}
	got := filterBookmarklets(in, "reader")
	if len(got) != 1 || got[0].ID != "3" {
		t.Errorf("query %q: expected only bookmarklet 3, got %+v", "reader", got)
	}
	if none := filterBookmarklets(in, "normal"); len(none) != 0 {
		t.Errorf("query %q: a non-bookmarklet must not match, got %d", "normal", len(none))
	}
}

// TestNormalizeURL checks de-duplication canonicalisation.
func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"https://Example.com/Path/":  "https://example.com/path",
		"https://example.com/path":   "https://example.com/path",
		"https://example.com/p#frag": "https://example.com/p",
		"  https://example.com/  ":   "https://example.com",
		"https://example.com":        "https://example.com",
	}
	for in, want := range cases {
		if got := normalizeURL(in); got != want {
			t.Errorf("normalizeURL(%q) = %q, want %q", in, got, want)
		}
	}
	// Two URLs differing only by trailing slash / case / fragment collapse.
	if normalizeURL("https://A.com/x/") != normalizeURL("https://a.com/x#y") {
		t.Errorf("expected case/slash/fragment variants to normalise equal")
	}
}

// TestTabSubtitle checks indicator prefixes and group-name bracket.
func TestTabSubtitle(t *testing.T) {
	cases := []struct {
		tab  Tab
		want string
	}{
		{Tab{URL: "https://x.com"}, "https://x.com"},
		{Tab{URL: "https://x.com", Pinned: true}, "📌 https://x.com"},
		{Tab{URL: "https://x.com", Audible: true}, "🔊 https://x.com"},
		{Tab{URL: "https://x.com", Muted: true, Audible: true}, "🔇 https://x.com"},
		{Tab{URL: "https://x.com", GroupTitle: "Work"}, "[Work] https://x.com"},
		{Tab{URL: "https://x.com", Pinned: true, GroupTitle: "Work"}, "📌 [Work] https://x.com"},
	}
	for _, c := range cases {
		if got := tabSubtitle(c.tab); got != c.want {
			t.Errorf("tabSubtitle(%+v) = %q, want %q", c.tab, got, c.want)
		}
	}
}

// TestTabIcon checks group-colour icon selection with fallback.
func TestTabIcon(t *testing.T) {
	if got := tabIcon(Tab{}); got != iconTabOpen {
		t.Errorf("ungrouped tab should use iconTabOpen")
	}
	if got := tabIcon(Tab{GroupColor: "purple"}); got != tabGroupIcons["purple"] {
		t.Errorf("purple group tab should use purple group icon")
	}
	if got := tabIcon(Tab{GroupColor: "Purple"}); got != tabGroupIcons["purple"] {
		t.Errorf("group colour match should be case-insensitive")
	}
	if got := tabIcon(Tab{GroupColor: "chartreuse"}); got != iconTabOpen {
		t.Errorf("unknown group colour should fall back to iconTabOpen")
	}
}

// TestRelativeTime checks the "… ago" watermark formatting.
func TestRelativeTime(t *testing.T) {
	now := time.Now().UnixMilli()
	cases := []struct {
		ms   int64
		want string
	}{
		{0, ""},
		{now, "just now"},
		{now - 5*60*1000, "5m ago"},
		{now - 3*60*60*1000, "3h ago"},
		{now - 2*24*60*60*1000, "2d ago"},
		{now - 3*7*24*60*60*1000, "3w ago"},
	}
	for _, c := range cases {
		if got := relativeTime(c.ms); got != c.want {
			t.Errorf("relativeTime(%d) = %q, want %q", c.ms, got, c.want)
		}
	}
}
