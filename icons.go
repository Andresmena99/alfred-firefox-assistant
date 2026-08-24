// Copyright (c) 2020 Dean Jackson <deanishe@deanishe.net>
// Modifications Copyright (c) 2026 Andres Mena Godino
// MIT Licence applies http://opensource.org/licenses/MIT

package main

import aw "github.com/deanishe/awgo"

var (
	iconAddon           = &aw.Icon{Value: "icons/addon.png"}
	iconBookmark        = &aw.Icon{Value: "icons/bookmark.png"}
	iconBookmarklet     = &aw.Icon{Value: "icons/bookmarklet.png"}
	iconDocs            = &aw.Icon{Value: "icons/docs.png"}
	iconError           = &aw.Icon{Value: "icons/error.png"}
	iconHistory         = &aw.Icon{Value: "icons/history.png"}
	iconIncognito       = &aw.Icon{Value: "icons/incognito.png"}
	iconInstall         = &aw.Icon{Value: "icons/install.png"}
	iconIssue           = &aw.Icon{Value: "icons/issue.png"}
	iconMore            = &aw.Icon{Value: "icons/more.png"}
	iconScript          = &aw.Icon{Value: "icons/script.png"}
	iconTab             = &aw.Icon{Value: "icons/tab.png"}
	iconTabOpen         = &aw.Icon{Value: "icons/tab-open.png"}       // blue: currently-open tab
	iconHistoryReopen   = &aw.Icon{Value: "icons/history-reopen.png"} // amber: reopen from history
	iconUpdateAvailable = &aw.Icon{Value: "icons/update-available.png"}
	iconUpdateOK        = &aw.Icon{Value: "icons/update-ok.png"}
	iconURL             = &aw.Icon{Value: "icons/url.png"}
	iconWarning         = &aw.Icon{Value: "icons/warning.png"}

	// tabGroupIcons maps a Firefox tab-group colour name to a tinted tab icon,
	// so a tab in a group shows that group's colour in Alfred.
	tabGroupIcons = map[string]*aw.Icon{
		"blue":   {Value: "icons/tab-group-blue.png"},
		"cyan":   {Value: "icons/tab-group-cyan.png"},
		"green":  {Value: "icons/tab-group-green.png"},
		"grey":   {Value: "icons/tab-group-grey.png"},
		"gray":   {Value: "icons/tab-group-grey.png"},
		"orange": {Value: "icons/tab-group-orange.png"},
		"pink":   {Value: "icons/tab-group-pink.png"},
		"purple": {Value: "icons/tab-group-purple.png"},
		"red":    {Value: "icons/tab-group-red.png"},
		"yellow": {Value: "icons/tab-group-yellow.png"},
	}

	// populated by loadURLActions
	scriptIcons = map[string]*aw.Icon{}

	imageExts = map[string]bool{
		".png":  true,
		".jpg":  true,
		".jpeg": true,
		".gif":  true,
		".icns": true,
	}
)

func init() {
	aw.IconError = iconError
	aw.IconWarning = iconWarning
}

// return custom icon or fallback
func actionIcon(name string, fallback *aw.Icon) *aw.Icon {
	if icon, ok := scriptIcons[name]; ok {
		return icon
	}
	return fallback
}
