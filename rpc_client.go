// Copyright (c) 2020 Dean Jackson <deanishe@deanishe.net>
// Modifications Copyright (c) 2026 Andres Mena Godino
// MIT Licence applies http://opensource.org/licenses/MIT

package main

import (
	"log"
	"net/rpc"
)

// RPC client used by workflow to execute extension actions.
type rpcClient struct {
	client  *rpc.Client
	appName string
}

// Create new RPC client. Returns an error if connection to server fails.
func newClient() (*rpcClient, error) {
	c, err := rpc.Dial("unix", socketPath)
	if err != nil {
		return nil, err
	}
	client := &rpcClient{client: c}
	client.appName, err = client.AppName()
	if err != nil {
		return nil, err
	}
	log.Printf("RPC client connected to %q", client.appName)
	return client, nil
}

// return new RPC client, panicking if it can't connect to server
func mustClient() *rpcClient {
	c, err := newClient()
	if err != nil {
		log.Printf("[ERROR] %v", err)
		panic("Cannot Connect to Extension")
	}
	return c
}

// AppName returns the name of the application running the server.
func (c *rpcClient) AppName() (string, error) {
	var s string
	err := c.client.Call("Firefox.AppName", "", &s)
	return s, err
}

// Ping checks connection to Firefox extension.
func (c *rpcClient) Ping() error {
	var s string
	return c.client.Call("Firefox.Ping", "", &s)
}

// Bookmarks returns all Firefox bookmarks matching query.
func (c *rpcClient) Bookmarks(query string) ([]Bookmark, error) {
	var bookmarks []Bookmark
	err := c.client.Call("Firefox.Bookmarks", query, &bookmarks)
	return bookmarks, err
}

// History searches Firefox browsing history.
func (c *rpcClient) History(query string) ([]History, error) {
	var history []History
	err := c.client.Call("Firefox.History", query, &history)
	return history, err
}

// Downloads searches Firefox downloads.
func (c *rpcClient) Downloads(query string) ([]Download, error) {
	var downloads []Download
	err := c.client.Call("Firefox.Downloads", query, &downloads)
	return downloads, err
}

// Tabs returns all Firefox tabs.
func (c *rpcClient) Tabs() ([]Tab, error) {
	var tabs []Tab
	err := c.client.Call("Firefox.Tabs", "", &tabs)
	return tabs, err
}

// Tab returns the specified tab. If tabID is 0, returns the active tab.
func (c *rpcClient) Tab(tabID int) (Tab, error) {
	var tab Tab
	err := c.client.Call("Firefox.Tab", tabID, &tab)
	return tab, err
}

// TabGroups returns all Firefox tab groups. Errors on extensions older than
// v1.4.0, in which case the caller derives groups from Tabs() instead.
func (c *rpcClient) TabGroups() ([]TabGroup, error) {
	var groups []TabGroup
	err := c.client.Call("Firefox.TabGroups", "", &groups)
	return groups, err
}

// ActivateTabGroup switches to the specified tab group, expanding it if
// collapsed. Errors on extensions older than v1.4.0.
func (c *rpcClient) ActivateTabGroup(groupID int) error {
	return c.client.Call("Firefox.ActivateTabGroup", groupID, nil)
}

/*
// CurrentTab returns the currently-active tab.
func (c *rpcClient) CurrentTab() (Tab, error) {
	var tab Tab
	err := c.client.Call("Firefox.CurrentTab", "", &tab)
	return tab, err
}
*/

// ActivateTab brings the specified tab to the front.
func (c *rpcClient) ActivateTab(tabID int) error {
	return c.client.Call("Firefox.ActivateTab", tabID, nil)
}

// CloseTabsLeft closes tabs to the left of specified tab.
func (c *rpcClient) CloseTabsLeft(tabID int) error {
	return c.client.Call("Firefox.CloseTabsLeft", tabID, nil)
}

// CloseTabsRight closes tabs to the right of specified tab.
func (c *rpcClient) CloseTabsRight(tabID int) error {
	return c.client.Call("Firefox.CloseTabsRight", tabID, nil)
}

// CloseTabsOther closes other tabs in same window as the specified one.
func (c *rpcClient) CloseTabsOther(tabID int) error {
	return c.client.Call("Firefox.CloseTabsOther", tabID, nil)
}

// OpenIncognito opens a URL in a new Incognito window.
func (c *rpcClient) OpenIncognito(URL string) error {
	return c.client.Call("Firefox.OpenIncognito", URL, nil)
}

// CloseTab closes the specified tab.
func (c *rpcClient) CloseTab(tabID int) error {
	return c.client.Call("Firefox.CloseTab", tabID, nil)
}

// MuteTab toggles the muted state of the specified tab.
func (c *rpcClient) MuteTab(tabID int) error {
	return c.client.Call("Firefox.MuteTab", tabID, nil)
}

// MoveTabToNewWindow moves the specified tab into a new window.
func (c *rpcClient) MoveTabToNewWindow(tabID int) error {
	return c.client.Call("Firefox.MoveTabToNewWindow", tabID, nil)
}

// RecentlyClosed returns recently-closed tabs (most recent first).
func (c *rpcClient) RecentlyClosed() ([]ClosedTab, error) {
	var tabs []ClosedTab
	err := c.client.Call("Firefox.RecentlyClosed", "", &tabs)
	return tabs, err
}

// RestoreSession restores a closed tab/window by session ID.
func (c *rpcClient) RestoreSession(sessionID string) error {
	return c.client.Call("Firefox.RestoreSession", sessionID, nil)
}

// RunJS executes JavaScript in the specified tab. If tabID is 0, the
// script is executed in the current tab.
func (c *rpcClient) RunJS(arg RunJSArg) (string, error) {
	var s string
	err := c.client.Call("Firefox.RunJS", arg, &s)
	return s, err
}

// RunBookmarklet executes a given bookmarklet in a given tab.
func (c *rpcClient) RunBookmarklet(arg RunBookmarkletArg) error {
	return c.client.Call("Firefox.RunBookmarklet", arg, nil)
}
