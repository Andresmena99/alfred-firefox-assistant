// Copyright (c) 2020 Dean Jackson <deanishe@deanishe.net>
// Modifications Copyright (c) 2026 Andres Mena Godino
// MIT Licence applies http://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"net/url"
	"strings"
)

/*
type Window struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
	Tabs   []Tab  `json:"tabs"`
}

func (w Window) String() string {
	return fmt.Sprintf("Window(id=%d, title=%q, active=%v)", w.ID, w.Title, w.Active)
}
*/

// Tab represents a Firefox tab. It contains a subset of the properties
// of the tab.Tab object from Firefox's extensions API.
// https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/API/tabs/Tab
type Tab struct {
	ID       int    `json:"id"`       // unique ID of tab
	WindowID int    `json:"windowId"` // unique ID of window tab belongs to
	Index    int    `json:"index"`    // position of tab in window
	Title    string `json:"title"`    // tab's title
	URL      string `json:"url"`      // tab's URL
	Active   bool   `json:"active"`   // whether tab is the active tab in its window

	// Populated by the upgraded extension (v1.3.0+). Default to zero values
	// when talking to an older extension.
	Pinned       bool   `json:"pinned"`       // whether tab is pinned
	Audible      bool   `json:"audible"`      // whether tab is currently playing audio
	Muted        bool   `json:"muted"`        // whether tab is muted
	LastAccessed int64  `json:"lastAccessed"` // ms since epoch the tab was last active
	GroupID      int    `json:"groupId"`      // tab group ID, or -1 if ungrouped
	GroupTitle   string `json:"groupTitle"`   // tab group name (if any)
	GroupColor   string `json:"groupColor"`   // tab group colour name (Firefox palette)
}

func (t Tab) String() string {
	return fmt.Sprintf("Tab(id=%d, title=%q, url=%q, active=%v)", t.ID, t.Title, t.URL, t.Active)
}

// TabGroup represents a Firefox tab group (Firefox 139+). It contains a subset
// of the properties of the tabGroups.TabGroup object from Firefox's extensions
// API, plus aggregates the workflow computes over the group's member tabs.
// https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/API/tabGroups/TabGroup
//
// Two sources can populate a TabGroup:
//
//   - the extension's "all-tab-groups" command (v1.4.0+), which reports the
//     authoritative Collapsed state and the group's position in the tab strip;
//   - deriveTabGroups, which reconstructs the list from the per-tab GroupID /
//     GroupTitle / GroupColor already returned by "all-tabs" (v1.3.0+).
//
// The derived path exists so tab-group search keeps working against an older
// extension; only Collapsed is unavailable there (see deriveTabGroups).
type TabGroup struct {
	ID       int    `json:"id"`       // unique ID of group
	Title    string `json:"title"`    // group name (may be empty — Firefox allows unnamed groups)
	Color    string `json:"color"`    // group colour name from the Firefox palette
	WindowID int    `json:"windowId"` // unique ID of the window the group lives in

	// Collapsed reports whether the group is collapsed in the tab strip. Only
	// meaningful when Known is true.
	Collapsed bool `json:"collapsed"`
	// CollapsedKnown is true when Collapsed came from the extension rather than
	// being defaulted, i.e. the group list was not derived from tabs alone.
	CollapsedKnown bool `json:"collapsedKnown"`

	// Aggregates over the group's tabs. Computed by the workflow, not Firefox.
	TabCount     int   `json:"tabCount"`     // number of tabs in the group
	LastAccessed int64 `json:"lastAccessed"` // ms since epoch the group's most-recently-used tab was active
	MinIndex     int   `json:"minIndex"`     // lowest tab index in the group (its position in the tab strip)
	ActiveTabID  int   `json:"activeTabId"`  // ID of the group's most-recently-used tab, or 0 if unknown
}

func (g TabGroup) String() string {
	return fmt.Sprintf("TabGroup(id=%d, title=%q, color=%q, tabs=%d)",
		g.ID, g.Title, g.Color, g.TabCount)
}

// Name returns the group's display name, falling back to a stable placeholder
// for the unnamed groups Firefox permits, so a result is never blank in Alfred.
func (g TabGroup) Name() string {
	if t := strings.TrimSpace(g.Title); t != "" {
		return t
	}
	return fmt.Sprintf("Untitled group #%d", g.ID)
}

// ungroupedID is the GroupID Firefox reports for a tab that is not in any tab
// group. The extensions API defines TAB_GROUP_ID_NONE as -1; the extension also
// normalises a missing groupId to -1 (see extension/alfred.js).
const ungroupedID = -1

// ClosedTab is a recently-closed tab that can be reopened. Populated by the
// upgraded extension (v1.3.0+) via the sessions API.
type ClosedTab struct {
	SessionID    string `json:"sessionId"`    // session ID
	Title        string `json:"title"`        // tab's title
	URL          string `json:"url"`          // tab's URL
	LastModified int64  `json:"lastModified"` // ms since epoch the tab was closed
}

func (t ClosedTab) String() string {
	return fmt.Sprintf("ClosedTab(session=%q, title=%q, url=%q)", t.SessionID, t.Title, t.URL)
}

// Bookmark represents a Firefox bookmark. It contains a subset of the properties
// of the bookmarks.BookmarkTreeNode object from the extensions API.
// https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/API/bookmarks/BookmarkTreeNode
type Bookmark struct {
	ID       string `json:"id"`       // unique ID
	Title    string `json:"title"`    // bookmark title
	Type     string `json:"type"`     // "bookmark" or "folder"
	URL      string `json:"url"`      // only present for type "bookmark"
	ParentID string `json:"parentId"` // ID of folder bookmark belongs to
	Index    int    `json:"index"`    // position in containing folder
}

func (bm Bookmark) String() string {
	return fmt.Sprintf("Bookmark(id=%q, title=%q, url=%q)", bm.ID, bm.Title, bm.URL)
}

// IsBookmarklet returns true of bookmark URL starts with "javascript:"
func (bm Bookmark) IsBookmarklet() bool {
	return strings.HasPrefix(bm.URL, "javascript:")
}

// JavaScript extracts JS code from a bookmarklet's URL. Returns an empty string
// if Bookmark is not a bookmarklet.
func (bm Bookmark) JavaScript() string {
	if !bm.IsBookmarklet() {
		return ""
	}
	s := strings.TrimPrefix(bm.URL, "javascript:")
	s, _ = url.PathUnescape(s)
	return s
}

// History is an entry from the browser history. It contains a subset of the properties
// of a native history.HistoryItem object.
// https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/API/history/HistoryItem
type History struct {
	ID    string `json:"id"`    // unique ID
	Title string `json:"title"` // page title
	URL   string `json:"url"`   // page URL
	// LastVisitTime is ms since epoch of the most recent visit. Populated by
	// the upgraded extension (v1.3.3+); 0 with older extensions.
	LastVisitTime int64 `json:"lastVisitTime"`
}

func (h History) String() string {
	return fmt.Sprintf("History(id=%q, title=%q, url=%q)", h.ID, h.Title, h.URL)
}

// Download is a file downloaded by Firefox. Contains a subset of the properties
// of a Firefox downloads.DownloadItem object.
// https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/API/downloads/DownloadItem
type Download struct {
	ID       int    `json:"id"`     // unique ID
	Path     string `json:"path"`   // absolute filepath to downloaded file
	Size     int64  `json:"size"`   // size of file in bytes
	URL      string `json:"url"`    // URL file was downloaded from
	MimeType string `json:"mime"`   // mime type of file
	Exists   bool   `json:"exists"` // whether Path still exists on disk
	Err      string `json:"error"`  // error message
}

func (d Download) String() string {
	return fmt.Sprintf("Download(id=%q, path=%q, url=%q)", d.ID, d.Path, d.URL)
}
