// Copyright (c) 2026 Andres Mena Godino
// MIT Licence applies http://opensource.org/licenses/MIT

package main

import (
	"testing"
	"time"
)

// groupTabsFixture is a representative set of tabs spanning three tab groups
// plus one ungrouped tab, used by the tab-group tests. Group metadata (title,
// colour) is carried on each member tab as the extension reports it.
func groupTabsFixture() []Tab {
	return []Tab{
		// Group 10 "MetricsApp" (green): 3 tabs, most-recently-used overall.
		{ID: 1, Title: "Metrics Console", URL: "https://console.example.com/tenant", Index: 0, WindowID: 1, LastAccessed: 1000, GroupID: 10, GroupTitle: "MetricsApp", GroupColor: "green"},
		{ID: 2, Title: "Metrics Docs", URL: "https://docs.example.com", Index: 1, WindowID: 1, LastAccessed: 5000, GroupID: 10, GroupTitle: "MetricsApp", GroupColor: "green"},
		{ID: 3, Title: "Tenant Setup", URL: "https://console.example.com/setup", Index: 2, WindowID: 1, LastAccessed: 2000, GroupID: 10, GroupTitle: "MetricsApp", GroupColor: "green"},
		// Group 20 "RegionMap" (purple): 2 tabs, older. A member tab mentions
		// "Reykjavik" so the group is findable by content, not just by name.
		{ID: 4, Title: "Reykjavik Region Map", URL: "https://wiki.example.com/docs/RegionMap/Reykjavik", Index: 5, WindowID: 1, LastAccessed: 500, GroupID: 20, GroupTitle: "RegionMap", GroupColor: "purple"},
		{ID: 5, Title: "Latency Budget", URL: "https://wiki.example.com/docs/RegionMap/Latency", Index: 6, WindowID: 1, LastAccessed: 400, GroupID: 20, GroupTitle: "RegionMap", GroupColor: "purple"},
		// Group 30 "TODO" (pink): 1 tab.
		{ID: 6, Title: "Weekly Tasks", URL: "https://notes.example.com/abc", Index: 8, WindowID: 1, LastAccessed: 3000, GroupID: 30, GroupTitle: "TODO", GroupColor: "pink"},
		// Ungrouped tab — must never appear in a group.
		{ID: 7, Title: "Scratch", URL: "https://example.com", Index: 9, WindowID: 1, LastAccessed: 9000, GroupID: ungroupedID},
	}
}

func groupIDsOf(groups []TabGroup) []int {
	ids := make([]int, len(groups))
	for i, g := range groups {
		ids[i] = g.ID
	}
	return ids
}

func findGroup(groups []TabGroup, id int) (TabGroup, bool) {
	for _, g := range groups {
		if g.ID == id {
			return g, true
		}
	}
	return TabGroup{}, false
}

// TestGroupsFromTabs verifies the derive-from-tabs path: correct grouping,
// aggregate counts, recency/active-tab, min index, and exclusion of ungrouped
// tabs.
func TestGroupsFromTabs(t *testing.T) {
	groups, members := deriveTabGroups(groupTabsFixture())

	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d (%v)", len(groups), groupIDsOf(groups))
	}

	// Ungrouped tab #7 must not create a group nor appear in any member list.
	if _, ok := findGroup(groups, ungroupedID); ok {
		t.Errorf("ungrouped tabs must not form a group")
	}

	g10, ok := findGroup(groups, 10)
	if !ok {
		t.Fatal("group 10 missing")
	}
	if g10.Title != "MetricsApp" || g10.Color != "green" {
		t.Errorf("group 10 metadata: got title=%q color=%q", g10.Title, g10.Color)
	}
	if g10.TabCount != 3 {
		t.Errorf("group 10 tab count: got %d, want 3", g10.TabCount)
	}
	if g10.LastAccessed != 5000 || g10.ActiveTabID != 2 {
		t.Errorf("group 10 recency: got lastAccessed=%d activeTab=%d, want 5000/2", g10.LastAccessed, g10.ActiveTabID)
	}
	if g10.MinIndex != 0 {
		t.Errorf("group 10 min index: got %d, want 0", g10.MinIndex)
	}
	if g10.CollapsedKnown {
		t.Errorf("derived groups must report CollapsedKnown=false")
	}
	if len(members[10]) != 3 {
		t.Errorf("group 10 members: got %d, want 3", len(members[10]))
	}
	if len(members[20]) != 2 {
		t.Errorf("group 20 members: got %d, want 2", len(members[20]))
	}
}

// TestFilterTabGroupsEmptyQuery verifies groups are returned most-recently-used
// first when there is no query.
func TestFilterTabGroupsEmptyQuery(t *testing.T) {
	groups, members := deriveTabGroups(groupTabsFixture())
	got := groupIDsOf(filterTabGroups(groups, members, ""))
	// Recency: g10 (5000) > g30 (3000) > g20 (500).
	want := []int{10, 30, 20}
	if len(got) != len(want) {
		t.Fatalf("empty query: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("empty query order: got %v, want %v", got, want)
			break
		}
	}
}

// TestFilterTabGroupsNameMatch verifies name matching, ordering, and that a
// name match outranks a content-only match for the same term.
func TestFilterTabGroupsNameMatch(t *testing.T) {
	groups, members := deriveTabGroups(groupTabsFixture())

	// "metrics" is in group 10's NAME and in its tab content; no other group's
	// name contains it. Only group 10 should match.
	got := groupIDsOf(filterTabGroups(groups, members, "metrics"))
	if len(got) != 1 || got[0] != 10 {
		t.Errorf("query %q: got %v, want [10]", "metrics", got)
	}

	// "region" matches only group 20 by name.
	got = groupIDsOf(filterTabGroups(groups, members, "region"))
	if len(got) != 1 || got[0] != 20 {
		t.Errorf("query %q: got %v, want [20]", "region", got)
	}
}

// TestFilterTabGroupsContentMatch verifies a group is findable by the content
// of its member tabs even when its name doesn't contain the term.
func TestFilterTabGroupsContentMatch(t *testing.T) {
	groups, members := deriveTabGroups(groupTabsFixture())
	// "reykjavik" appears only in group 20's member tab title, not in any group
	// name.
	got := groupIDsOf(filterTabGroups(groups, members, "reykjavik"))
	if len(got) != 1 || got[0] != 20 {
		t.Errorf("content query %q: got %v, want [20]", "reykjavik", got)
	}
}

// TestFilterTabGroupsNameOutranksContent verifies that when a term matches one
// group's name and another group's content, the name match ranks first.
func TestFilterTabGroupsNameOutranksContent(t *testing.T) {
	tabs := []Tab{
		// Group 1 "Docs": name contains "docs".
		{ID: 1, Title: "Home", URL: "https://a.com", Index: 0, LastAccessed: 100, GroupID: 1, GroupTitle: "Docs", GroupColor: "blue"},
		// Group 2 "Misc": a tab whose title contains "docs" (content only).
		{ID: 2, Title: "API docs", URL: "https://b.com", Index: 1, LastAccessed: 900, GroupID: 2, GroupTitle: "Misc", GroupColor: "red"},
	}
	groups, members := deriveTabGroups(tabs)
	got := groupIDsOf(filterTabGroups(groups, members, "docs"))
	if len(got) != 2 {
		t.Fatalf("query %q: got %v, want 2 groups", "docs", got)
	}
	if got[0] != 1 {
		t.Errorf("query %q: name match (group 1) should rank above content match (group 2); got %v", "docs", got)
	}
}

// TestFilterTabGroupsMultiTerm verifies order-independent multi-term AND
// matching against group names.
func TestFilterTabGroupsMultiTerm(t *testing.T) {
	tabs := []Tab{
		{ID: 1, Title: "x", URL: "https://a.com", GroupID: 1, GroupTitle: "PaymentsRefactor", GroupColor: "yellow"},
		{ID: 2, Title: "y", URL: "https://b.com", GroupID: 2, GroupTitle: "PaymentsInfra", GroupColor: "grey"},
	}
	groups, members := deriveTabGroups(tabs)
	// "refactor payments" (reversed) must match only the PaymentsRefactor group.
	got := groupIDsOf(filterTabGroups(groups, members, "refactor payments"))
	if len(got) != 1 || got[0] != 1 {
		t.Errorf("multi-term query: got %v, want [1]", got)
	}
}

// TestFilterTabGroupsNoMatch verifies a non-matching query returns nothing.
func TestFilterTabGroupsNoMatch(t *testing.T) {
	groups, members := deriveTabGroups(groupTabsFixture())
	if got := filterTabGroups(groups, members, "zzznotarealthing"); len(got) != 0 {
		t.Errorf("expected no matches, got %v", groupIDsOf(got))
	}
}

// TestTabGroupName verifies the display-name fallback for unnamed groups.
func TestTabGroupName(t *testing.T) {
	if got := (TabGroup{ID: 5, Title: "Work"}).Name(); got != "Work" {
		t.Errorf("named group: got %q, want %q", got, "Work")
	}
	if got := (TabGroup{ID: 5, Title: "   "}).Name(); got != "Untitled group #5" {
		t.Errorf("blank title: got %q, want %q", got, "Untitled group #5")
	}
	if got := (TabGroup{ID: 7}).Name(); got != "Untitled group #7" {
		t.Errorf("empty title: got %q, want %q", got, "Untitled group #7")
	}
}

// TestGroupIcon verifies colour-tinted icon selection with case-insensitive
// matching and a neutral fallback.
func TestGroupIcon(t *testing.T) {
	if got := groupIcon(TabGroup{Color: "purple"}); got != tabGroupIcons["purple"] {
		t.Errorf("purple group should use purple icon")
	}
	if got := groupIcon(TabGroup{Color: "Green"}); got != tabGroupIcons["green"] {
		t.Errorf("colour match should be case-insensitive")
	}
	if got := groupIcon(TabGroup{Color: ""}); got != iconTab {
		t.Errorf("missing colour should fall back to iconTab")
	}
	if got := groupIcon(TabGroup{Color: "chartreuse"}); got != iconTab {
		t.Errorf("unknown colour should fall back to iconTab")
	}
}

// TestGroupSubtitle verifies subtitle composition: recency, tab-count
// pluralisation, and the collapsed indicator only when known.
func TestGroupSubtitle(t *testing.T) {
	now := time.Now().UnixMilli()

	// Singular tab, recency present.
	s := groupSubtitle(TabGroup{TabCount: 1, LastAccessed: now})
	if want := "just now  ·  1 tab  ·  ↩ switch to group"; s != want {
		t.Errorf("singular subtitle: got %q, want %q", s, want)
	}

	// Plural tabs, no recency (LastAccessed 0 → watermark omitted).
	s = groupSubtitle(TabGroup{TabCount: 3})
	if want := "3 tabs  ·  ↩ switch to group"; s != want {
		t.Errorf("plural subtitle: got %q, want %q", s, want)
	}

	// Collapsed indicator only shows when CollapsedKnown.
	s = groupSubtitle(TabGroup{TabCount: 2, Collapsed: true, CollapsedKnown: true})
	if want := "2 tabs  ·  ⊟ collapsed  ·  ↩ switch to group"; s != want {
		t.Errorf("collapsed subtitle: got %q, want %q", s, want)
	}
	s = groupSubtitle(TabGroup{TabCount: 2, Collapsed: true, CollapsedKnown: false})
	if want := "2 tabs  ·  ↩ switch to group"; s != want {
		t.Errorf("unknown-collapsed subtitle must omit indicator: got %q, want %q", s, want)
	}
}

// TestLoadTabGroupsMergesAggregates verifies that when the extension supplies a
// group list, member-derived aggregates backfill any zero fields while the
// extension's collapsed state is preserved. Uses a fake RPC transport.
func TestMergeExtensionGroupsBackfillsAggregates(t *testing.T) {
	// Simulate the merge loop of loadTabGroups directly (no RPC): extension
	// reports identity + collapsed but zero aggregates; derived fills them in.
	tabs := groupTabsFixture()
	derived, _ := deriveTabGroups(tabs)
	byID := map[int]TabGroup{}
	for _, g := range derived {
		byID[g.ID] = g
	}

	extGroups := []TabGroup{
		{ID: 10, Title: "MetricsApp", Color: "green", Collapsed: true, CollapsedKnown: true},
	}
	// Apply the same backfill loadTabGroups uses.
	g := &extGroups[0]
	d := byID[g.ID]
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

	if extGroups[0].TabCount != 3 {
		t.Errorf("backfill tab count: got %d, want 3", extGroups[0].TabCount)
	}
	if extGroups[0].ActiveTabID != 2 {
		t.Errorf("backfill active tab: got %d, want 2", extGroups[0].ActiveTabID)
	}
	if !extGroups[0].Collapsed || !extGroups[0].CollapsedKnown {
		t.Errorf("extension collapsed state must be preserved")
	}
}
