// Copyright (c) 2020 Dean Jackson <deanishe@deanishe.net>
// Modifications Copyright (c) 2026 Andres Mena Godino
// MIT Licence applies http://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	aw "github.com/deanishe/awgo"
	"github.com/deanishe/awgo/util"
)

var (
	tabActions = map[string]tabAction{}
	urlActions = map[string]urlAction{}
)

type tabAction interface {
	Name() string
	Icon() *aw.Icon
	Run(tabID int) error
}

type urlAction interface {
	Name() string
	Icon() *aw.Icon
	Run(URL string) error
}

func init() {
	for _, a := range []tabAction{
		tAction{name: "Activate Tab", action: "activate", icon: iconTab},
		tAction{name: "Close Tab", action: "close", icon: iconTab},
		tAction{name: "Close Duplicate Tabs", action: "close-dupes", icon: iconTab},
		tAction{name: "Mute / Unmute Tab", action: "mute", icon: iconTab},
		tAction{name: "Move Tab to New Window", action: "move-new-window", icon: iconTab},
		tAction{name: "Close Tabs to Left", action: "close-left", icon: iconTab},
		tAction{name: "Close Tabs to Right", action: "close-right", icon: iconTab},
		tAction{name: "Close Other Tabs", action: "close-other", icon: iconTab},
	} {
		tabActions[a.Name()] = a
	}

	for _, a := range []urlAction{
		openIncognito{},
		copyAction{name: "Copy Link as Markdown", icon: iconURL, format: "markdown"},
		copyAction{name: "Copy Title", icon: iconURL, format: "title"},
	} {
		urlActions[a.Name()] = a
	}
}

func loadURLActions() error {
	var (
		scripts = map[string]string{}
		infos   []os.FileInfo
		err     error
	)
	for _, dir := range scriptDirs {
		if infos, err = ioutil.ReadDir(dir); err != nil {
			return err
		}

		for _, fi := range infos {
			if fi.IsDir() {
				continue
			}

			var (
				path     = filepath.Join(dir, fi.Name())
				ext      = strings.ToLower(filepath.Ext(fi.Name()))
				name     = fi.Name()[0 : len(fi.Name())-len(ext)]
				_, known = util.DefaultInterpreters[ext]
				exe      = fi.Mode()&0111 != 0
			)
			if exe || known {
				scripts[name] = path
			}

			if imageExts[ext] {
				scriptIcons[name] = &aw.Icon{Value: path}
			}
		}
	}

	for name, path := range scripts {
		log.Printf("loaded URL action %q from %q", name, util.PrettyPath(path))
		a := uAction{
			name:   name,
			icon:   actionIcon(name, iconURL),
			script: path,
		}
		urlActions[name] = a
	}

	return nil
}

type tAction struct {
	name   string
	icon   *aw.Icon
	action string
}

func (a tAction) Name() string   { return a.name }
func (a tAction) Icon() *aw.Icon { return a.icon }
func (a tAction) Run(tabID int) error {
	c := mustClient()
	switch a.action {
	case "activate":
		_, err := util.RunAS(fmt.Sprintf(`tell application "%s" to activate`, c.appName))
		if err != nil {
			return err
		}
		return c.ActivateTab(tabID)
	case "close-left":
		return c.CloseTabsLeft(tabID)
	case "close-right":
		return c.CloseTabsRight(tabID)
	case "close-other":
		return c.CloseTabsOther(tabID)
	case "close":
		return c.CloseTab(tabID)
	case "mute":
		return c.MuteTab(tabID)
	case "move-new-window":
		return c.MoveTabToNewWindow(tabID)
	case "close-dupes":
		return closeDuplicateTabs(c, tabID)
	default:
		return fmt.Errorf("unknown action %q", action)
	}
}

// closeDuplicateTabs closes all but the first tab for each distinct URL. The
// tab the user acted on (keepTabID) is never closed.
func closeDuplicateTabs(c *rpcClient, keepTabID int) error {
	tabs, err := c.Tabs()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var firstErr error
	for _, t := range tabs {
		key := normalizeURL(t.URL)
		if !seen[key] {
			seen[key] = true
			continue
		}
		if t.ID == keepTabID {
			continue
		}
		if err := c.CloseTab(t.ID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

type uAction struct {
	name   string
	icon   *aw.Icon
	script string
}

func (a uAction) Name() string   { return a.name }
func (a uAction) Icon() *aw.Icon { return a.icon }
func (a uAction) Run(URL string) error {
	c, err := newClient()
	if err == nil {
		os.Setenv("BROWSER", c.appName)
	}
	data, err := util.Run(a.script, URL)
	if err != nil {
		return err
	}
	s := string(data)
	if s != "" {
		log.Print(util.Pad(fmt.Sprintf(" output: %q ", a.name), "-", 50))
		log.Print(s)
	}
	return nil
}

// URL action to open a URL in a new incognito window
type openIncognito struct{}

func (a openIncognito) Name() string   { return "Open in Incognito Window" }
func (a openIncognito) Icon() *aw.Icon { return iconIncognito }
func (a openIncognito) Run(URL string) error {
	mustClient().OpenIncognito(URL)
	return nil
}

// copyAction copies the URL (and, for some formats, the page title from the
// TITLE workflow variable) to the clipboard.
type copyAction struct {
	name   string
	icon   *aw.Icon
	format string // "markdown" or "title"
}

func (a copyAction) Name() string   { return a.name }
func (a copyAction) Icon() *aw.Icon { return a.icon }
func (a copyAction) Run(URL string) error {
	title := os.Getenv("TITLE")
	var text string
	switch a.format {
	case "markdown":
		if title == "" {
			title = URL
		}
		text = fmt.Sprintf("[%s](%s)", title, URL)
	case "title":
		text = title
	default:
		text = URL
	}
	log.Printf("copying to clipboard: %q", text)
	return pbcopy(text)
}

// pbcopy writes s to the macOS clipboard.
func pbcopy(s string) error {
	cmd := exec.Command("/usr/bin/pbcopy")
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

var (
	_ tabAction = (*tAction)(nil)
	_ urlAction = (*uAction)(nil)
	_ urlAction = openIncognito{}
	_ urlAction = copyAction{}
)
