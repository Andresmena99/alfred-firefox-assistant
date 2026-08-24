/*
 * Copyright (c) 2019-2021 Dean Jackson <deanishe@deanishe.net>
 * Modifications Copyright (c) 2026 Andres Mena Godino
 *
 * MIT Licence applies http://opensource.org/licenses/MIT
 *
 * This file is a modified version of the original from
 * https://github.com/deanishe/alfred-firefox
 */
/* global browser */

/**
 * Name of native application according to application manifest.
 * @var {string} appName
 */
const appName = 'net.deanishe.alfred.firefox';

const iconConnected = 'icons/bowler.svg';
const iconDisconnected = 'icons/bowler-red.svg';

/**
 * Tab object.
 * @param {tabs.Tab} tab - Native tab object to create Tab from.
 * @return {Object} - API Tab object.
 */
const Tab = tab => {
  let obj = {};

  tab = tab || {};

  obj.id         = tab.id          || 0;
  obj.windowId   = tab.windowId    || 0;
  obj.index      = tab.index       || 0;
  obj.title      = tab.title       || '';
  obj.url        = new URL(tab.url || '');
  // obj.favicon = tab.favIconUrl  || '';
  obj.active     = tab.active      || false;
  obj.pinned     = tab.pinned      || false;
  obj.audible    = tab.audible     || false;
  obj.muted      = (tab.mutedInfo && tab.mutedInfo.muted) || false;
  obj.lastAccessed = tab.lastAccessed || 0;
  obj.groupId    = (typeof tab.groupId === 'number') ? tab.groupId : -1;
  obj.groupTitle = '';
  obj.groupColor = '';

  obj.toString = function() {
    return `#${this.id} (${this.windowId}x${this.index}) "${this.title}" - ${this.url}`;
  };

  return obj;
};

/**
 * Bookmark object.
 * @param {bookmarks.BookmarkTreeNode} bm - Native object to create Bookmark from.
 * @return {Object} - API Bookmark object.
 */
const Bookmark = bm => {
  let obj = {};
  bm = bm || {};

  obj.id       = bm.id       || 0;
  obj.index    = bm.index    || 0;
  obj.title    = bm.title    || '';
  obj.parentId = bm.parentId || 0;
  obj.type     = bm.type     || '';
  obj.url      = bm.url      || '';

  obj.toString = function() {
    return `#${this.id} "${this.title}" - ${this.url}`;
  };

  return obj;
};

/**
 * HistoryEntry object.
 * @param {history.HistoryItem} hi - Native object to create HistoryEntry from.
 * @return {Object} - API History object.
 */
const HistoryEntry = hi => {
  let obj = {};
  hi = hi || {};

  obj.id    = hi.id    || 0;
  obj.url   = hi.url   || '';
  obj.title = hi.title || hi.url;
  obj.lastVisitTime = hi.lastVisitTime || 0;

  obj.toString = function() {
    return `#${this.id} "${this.title}" - ${this.url}`;
  };

  return obj;
};

/**
 * Download object.
 * @param {downloads.DownloadItem} di - Native object to create Download from.
 * @return {Object} - API Download object.
 */
const Download = di => {
  let obj = {};
  di = di || {};

  obj.id     = di.id       || 0;
  obj.path   = di.filename || '';
  obj.size   = di.fileSize || 0;
  obj.url    = di.url      || '';
  obj.mime   = di.mime     || '';
  obj.exists = di.exists   || false;
  obj.error  = di.error    || '';

  obj.toString = function() {
    return `#${this.id} "${this.path}" - ${this.url}`;
  };

  return obj;
};

/**
 * Extension application object.
 * @constructor
 */
const Background = function() {
  const self = this;

  self.port = null,
    self.nativePort = null,
    self.connected = false;

  self.onConnected = port => {
    self.port = port;
    port.onMessage.addListener(self.receive);
    console.debug('connected to popup');
  };

  self.send = msg => {
    self.port.postMessage(msg);
    console.debug('sent message', msg);
  };

  self.receive = msg => {
    console.debug('received message', msg);
    if ('command' in msg) {
      switch (msg.command) {
        case 'status':
          self.send({ status: self.connected ? 'connected' : 'disconnected' });
          return;
        case 'reconnect':
          console.debug('reconnecting to native app ...');
          self.connectNative();
          return;
        case 'reload':
          console.debug('reloading extension ...');
          browser.runtime.reload();
          return;
      }
    }
  };

  self.connectNative = () => {
    self.connected = false;

    let listener = payload => {
      if (!self.connected) {
        self.connected = true;
        // self.nativePort.onDisconnect.removeListener(self.connectNativeFailed);
        self.onConnectedNative();
      }
      self.receiveNative(payload);
    };

    self.nativePort = browser.runtime.connectNative(appName);
    self.nativePort.onMessage.addListener(listener);
    self.nativePort.onDisconnect.addListener(self.connectNativeFailed);
  };

  /**
   * Callback for connection failure.
   * Logs an error message to the console.
   */
  self.connectNativeFailed = port => {
    let msg = '';
    if (port.error) {
      msg = port.error.message;
    }
    self.connected = false;
    console.error(`native client connection failed: ${msg}`);
    browser.browserAction.setIcon({ path: iconDisconnected });
  };

  /**
   * Callback for successful connection to native application.
   * Logs a message to the console.
   */
  self.onConnectedNative = () => {
    console.log('connected to native client');
    browser.browserAction.setIcon({ path: iconConnected });
  };

  /**
   * Handle commands from native application.
   * @param {Object} msg - Data from native application.
   * @param {string} msg.id - Command/response ID.
   * @param {Object} msg.params - Arguments to command.
   */
  self.receiveNative = msg => {
    console.log(`received:`, msg);
    let p = null;
    if ('command' in msg) {
      switch (msg.command) {
        case 'ping':
          p = self.ping();
          break;
        // case 'all-windows':
        //   p = self.allWindows();
        //   break;
        // case 'current-window':
        //   p = self.currentWindow();
        //   break;
        case 'all-tabs':
          p = self.allTabs();
          break;
        case 'all-tab-groups':
          p = self.allTabGroups();
          break;
        case 'activate-tab-group':
          p = self.activateTabGroup(msg.params);
          break;
        // DEPRECATED - replaced by self.tab(); unused by newer
        // versions 0.2.0+ of workflow
        // Remove from future versions
        case 'current-tab':
          p = self.tab(0);
          break;
        case 'tab':
          p = self.tab(msg.params);
          break;
        case 'all-bookmarks':
          p = self.allBookmarks();
          break;
        case 'search-bookmarks':
          p = self.searchBookmarks(msg.params);
          break;
        case 'search-history':
          p = self.searchHistory(msg.params);
          break;
        case 'search-downloads':
          p = self.searchDownloads(msg.params);
          break;
        case 'activate-tab':
          p = self.activateTab(msg.params);
          break;
        case 'close-tabs-left':
          p = self.closeTabsLeft(msg.params);
          break;
        case 'close-tabs-right':
          p = self.closeTabsRight(msg.params);
          break;
        case 'close-tabs-other':
          p = self.closeTabsOther(msg.params);
          break;
        case 'execute-js':
          p = self.executeJS(msg.params);
          break;
        case 'run-bookmarklet':
          p = self.runBookmarklet(msg.params);
          break;
        case 'open-incognito':
          p = self.openIncognito(msg.params);
          break;
        case 'close-tab':
          p = self.closeTab(msg.params);
          break;
        case 'recently-closed':
          p = self.recentlyClosed();
          break;
        case 'restore-session':
          p = self.restoreSession(msg.params);
          break;
        case 'mute-tab':
          p = self.muteTab(msg.params);
          break;
        case 'move-tab-new-window':
          p = self.moveTabNewWindow(msg.params);
          break;
        default:
          console.error(`unknown command: ${msg.command}`);
          self.sendError(msg.id, 'unknown command');
          return;
      }
      p.then(payload => {
        self.sendNative({ id: msg.id, payload: payload });
      }).catch(err => {
        self.sendError(msg.id, err.message);
      });
    } else {
      self.sendError(msg.id, 'no command given');
    }
  };

  /**
   * Send response to native application.
   * @param {Object} msg - Data to send to native application.
   * @param {string} msg.id - Command/response ID.
   * @param {string|bool|Object} msg.payload - Actual response data.
   * @param {string} msg.error - Error message if command failed.
   */
  self.sendNative = msg => {
    if (self.nativePort) {
      self.nativePort.postMessage(msg)
        .then(resp => {
          console.log(`sent:`, msg);
          console.log(`response:`, resp);
        })
        .catch(err => {
          console.error(`send error: ${err.message}`);
      });
    }
  };

  /**
   * Send error respones to native application.
   * @param {string} id - Command/response ID.
   * @param {string} msg - Error message.
   */
  self.sendError = (id, msg) => {
    self.sendNative({ id: id, error: msg });
  };

  /**
   * Handle "ping" command.
   * @return {Promise} - Resolves to string "pong".
   */
  self.ping = () => {
    return new Promise(resolve => {
      resolve('pong');
    });
  };

  /**
   * Handle "all-tabs" command.
   * @return {Promise} - Resolves to array of Tab objects for all tabs sorted
   * by most recently used.
   */
  self.allTabs = () => {
    // Look up tab groups (Firefox 139+) so each tab can carry its group's
    // title and colour. Guarded + best-effort: if the API is missing or fails,
    // tabs are returned without group info.
    let groupsById = {};
    let loadGroups = Promise.resolve();
    if (browser.tabGroups && browser.tabGroups.query) {
      loadGroups = browser.tabGroups
        .query({})
        .then(groups => {
          groups.forEach(g => {
            groupsById[g.id] = g;
          });
        })
        .catch(err => {
          console.debug(`tabGroups.query failed: ${err}`);
        });
    }
    return loadGroups
      .then(() => browser.tabs.query({}))
      .then(tabs =>
        tabs
          .sort((a, b) => (b?.lastAccessed ?? 0) - (a?.lastAccessed ?? 0))
          .map(t => {
            let obj = Tab(t);
            let g = groupsById[obj.groupId];
            if (g) {
              obj.groupTitle = g.title || '';
              obj.groupColor = g.color || '';
            }
            return obj;
          })
      );
  };

  /**
   * Handle "all-tab-groups" command.
   *
   * Returns every tab group (Firefox 139+) enriched with aggregates over its
   * member tabs — tab count, the most-recently-accessed member (used both for
   * ordering groups by recency and as the tab to activate), and the group's
   * position in the tab strip (minimum tab index). Guarded + best-effort: if
   * the tabGroups API is missing the promise rejects, and the native side falls
   * back to deriving groups from all-tabs.
   *
   * @return {Promise} - Resolves to an array of group objects:
   *   {id, title, color, windowId, collapsed, collapsedKnown,
   *    tabCount, lastAccessed, minIndex, activeTabId}
   */
  self.allTabGroups = () => {
    if (!(browser.tabGroups && browser.tabGroups.query)) {
      return Promise.reject(new Error('tabGroups API unavailable'));
    }
    return Promise.all([
      browser.tabGroups.query({}),
      browser.tabs.query({}),
    ]).then(([groups, tabs]) => {
      // Aggregate member tabs per group in a single pass.
      let agg = {};
      tabs.forEach(t => {
        let gid = (typeof t.groupId === 'number') ? t.groupId : -1;
        if (gid < 0) return;
        let a = agg[gid];
        if (!a) {
          a = agg[gid] = { count: 0, lastAccessed: 0, minIndex: Infinity, activeTabId: 0 };
        }
        a.count += 1;
        let la = t.lastAccessed || 0;
        if (la >= a.lastAccessed) {
          a.lastAccessed = la;
          a.activeTabId = t.id || 0;
        }
        let idx = (typeof t.index === 'number') ? t.index : Infinity;
        if (idx < a.minIndex) a.minIndex = idx;
      });
      return groups.map(g => {
        let a = agg[g.id] || { count: 0, lastAccessed: 0, minIndex: 0, activeTabId: 0 };
        return {
          id: g.id,
          title: g.title || '',
          color: g.color || '',
          windowId: (typeof g.windowId === 'number') ? g.windowId : 0,
          collapsed: g.collapsed || false,
          collapsedKnown: true,
          tabCount: a.count,
          lastAccessed: a.lastAccessed,
          minIndex: (a.minIndex === Infinity) ? 0 : a.minIndex,
          activeTabId: a.activeTabId,
        };
      });
    });
  };

  /**
   * Handle "activate-tab-group" command.
   *
   * Switches to a tab group: focuses the group's window, expands the group if
   * it is collapsed, and activates the group's most-recently-used tab so the
   * group becomes the visible, active context. Activating a member tab is what
   * makes Firefox scroll the group into view and mark it current.
   *
   * @param {number} groupId - ID of the group to switch to.
   */
  self.activateTabGroup = groupId => {
    console.debug(`activating tab group #${groupId} ...`);
    let group = null;
    return browser.tabGroups
      .get(groupId)
      .then(g => {
        group = g;
        // Expand the group first so its tabs are visible once activated.
        if (g && g.collapsed && browser.tabGroups.update) {
          return browser.tabGroups.update(groupId, { collapsed: false }).catch(err => {
            console.debug(`tabGroups.update failed: ${err}`);
          });
        }
        return null;
      })
      .then(() => browser.tabs.query({ groupId: groupId }))
      .then(tabs => {
        if (!tabs || !tabs.length) throw new Error('tab group is empty');
        // Activate the most-recently-used tab in the group.
        tabs.sort((a, b) => (b.lastAccessed || 0) - (a.lastAccessed || 0));
        let target = tabs[0];
        return browser.tabs
          .update(target.id, { active: true })
          .then(() => browser.windows.update(target.windowId, { focused: true }));
      });
  };

  /**
   * Handle "activate-tab" command.
   * @param {number} id - ID of tab to activate.
   */
  self.activateTab = id => {
    return browser.tabs
      .update(id, { active: true })
      .then(() => {
        return browser.tabs.get(id);
      })
      .then(tab => {
        return browser.windows.update(tab.windowId, { focused: true });
      });
  };

  /**
   * Handle "current-tab" command.
   * @return {Promise} - Resolves to Tab for current tab.
   * Throws an error if there is no current tab.
   */
  // self.currentTab = () => {
  //   return self.activeTab(null).then(t => {
  //     if (!t) throw 'no current tab';
  //     let tab = Tab(t);
  //     console.log(`[current-tab] ${tab}`);
  //     return tab;
  //   });
  // };

  /**
   * Handle "tab" command.
   * @param {number} tabId - ID of tab to return.
   * @return {Promise} - Resolves to Tab for current tab.
   * Throws an error if there is no current tab.
   */
  self.tab = tabId => {
    if (!tabId) {
      return self.activeTab(null).then(t => {
        if (!t) throw 'no current tab';
        let tab = Tab(t);
        console.log(`[current-tab] ${tab}`);
        return tab;
      });
    }

    return browser.tabs
      .get(tabId)
      .then(t => {
        return Tab(t);
      })
  };

  /**
   * Handle "all-bookmarks" command.
   * @return {Promise} - Resolves to array of Bookmark objects for all bookmarks
   * and folders.
   */
  self.allBookmarks = () => {
    let bookmarks = [];
    let addBookmarks = node => {
      if (node.url) bookmarks.push(Bookmark(node));
      if (node.children) node.children.map(n => addBookmarks(n));
    };

    return browser.bookmarks.getTree().then(root => {
      addBookmarks(root[0]);
      return bookmarks;
    });
  };

  /**
   * Handle "search-bookmarks" command.
   * @param {string} query - Search query.
   * @return {Promies} - Resolves to array of Bookmark objects matching query.
   */
  self.searchBookmarks = query => {
    return browser.bookmarks.search(query).then(nodes => {
      let bookmarks = nodes.filter(n => n.url).map(n => Bookmark(n));
      console.debug(`${bookmarks.length} bookmark(s) for "${query}"`);
      return bookmarks;
    });
  };

  /**
   * Handle "search-history" command.
   * @param {string} query - Search query.
   * @return {Promies} - Resolves to array of History objects matching query.
   */
  self.searchHistory = query => {
    return browser.history.search({ text: query, startTime: 0 }).then(items => {
      let history = items.filter(it => it.url).map(it => HistoryEntry(it));
      console.debug(`${history.length} history item(s) for "${query}"`);
      return history;
    });
  };

  /**
   * Handle "search-downloads" command.
   * @param {string} query - Search query.
   * @return {Promise} - Resolves to array of Download objects matching query.
   */
  self.searchDownloads = query => {
    return browser.downloads
      .search({
        query: [query],
        exists: true,
      })
      .then(items => {
        console.debug(`${items.length} download(s) for "${query}"`);
        return items.map(it => Download(it));
      });
  };

  /**
   * Handle "close-tabs-left" command.
   * @param {number} tabId - ID of tab whose neighbours to close.
   * @return {Promise} - Result of browser.tabs.remove()
   */
  self.closeTabsLeft = tabId => {
    console.debug(`closing tabs to left of tab #${tabId} ...`);
    let activeTab = null;
    return browser.tabs
      .get(tabId)
      .then(tab => {
        if (!tab) throw 'no current tab';
        activeTab = tab;
        return browser.tabs.query({ windowId: tab.windowId });
      })
      .then(tabs => {
        let ids = tabs.filter(t => t.index < activeTab.index).map(t => t.id);
        return browser.tabs.remove(ids);
      });
  };

  /**
   * Handle "close-tabs-right" command.
   * @param {number} tabId - ID of tabs whose neighbours to close.
   * @return {Promise} - Result of browser.tabs.remove()
   */
  self.closeTabsRight = tabId => {
    console.debug(`closing tabs to right of tab #${tabId} ...`);
    let activeTab = null;
    return browser.tabs
      .get(tabId)
      .then(tab => {
        if (!tab) throw 'no current tab';
        activeTab = tab;
        return browser.tabs.query({ windowId: tab.windowId });
      })
      .then(tabs => {
        let ids = tabs.filter(t => t.index > activeTab.index).map(t => t.id);
        return browser.tabs.remove(ids);
      });
  };

  /**
   * Handle "close-tabs-other" command.
   * @param {number} tabId - ID of window to close tabs in.
   * @return {Promise} - Result of browser.tabs.remove()
   */
  self.closeTabsOther = tabId => {
    console.debug(`closing other tabs in window of tab #${tabId} ...`);
    let activeTab = null;
    return browser.tabs
      .get(tabId)
      .then(tab => {
        activeTab = tab;
        return browser.tabs.query({ windowId: tab.windowId });
      })
      .then(tabs => {
        let ids = tabs.filter(t => t.id !== activeTab.id).map(t => t.id);
        return browser.tabs.remove(ids);
      });
  };

  /** Handle "execute-js" command. */
  // self.executeJS = js => {
  //   return browser.tabs.executeScript({ code: js }).then(results => {
  //     console.debug(`js=${js}, results=`, results);
  //   });
  // };
  /**
   * Handle "execute-js" command.
   * @param {Object} params - Tab and bookmarklet IDs.
   * @param {number} params.tabId - ID of tab to execute JS in.
   * If tabId is 0, JS is executed in the active tab.
   * @param {string} params.js - JavaScript to execute.
   */
  self.executeJS = params => {
    console.debug(`execute-js`, params);
    var p;
    if (params.tabId) {
      p = browser.tabs.executeScript(params.tabId, { code: params.js });
    } else {
      p = browser.tabs.executeScript({ code: params.js });
    }
    return p.then(result => {
      return JSON.stringify(result);
    });
  };

  /**
   * Handle "run-bookmarklet" command.
   * @param {Object} params - Tab and bookmarklet IDs.
   * @param {number} params.tabId - ID of tab to execute bookmarklet in.
   * If tabId is 0, bookmarklet is executed in the active tab.
   * @param {string} params.bookmarkId - ID of bookmarklet to execute.
   */
  self.runBookmarklet = params => {
    console.debug(`run-bookmarklet`, params);
    return browser.bookmarks.get(params.bookmarkId).then(bookmarks => {
      if (!bookmarks.length) throw 'bookmark not found';
      let bm = bookmarks[0];
      if (!bm.url.startsWith('javascript:')) throw 'not a bookmarklet';
      let js = decodeURI(bm.url.slice(11));
      if (params.tabId) browser.tabs.executeScript(params.tabId, { code: js });
      else browser.tabs.executeScript({ code: js });
    });
  };

  /**
   * Handle "open-incognito" command.
   * @param {string} url - URL to open in a new Incognito window.
   * @return {Promise} - Promise that resolves to null.
   */
  self.openIncognito = url => {
    console.debug(`open-incognito ${url}`);
    return browser.windows.create({ incognito: true, url: url });
  };

  /**
   * Handle "close-tab" command.
   * @param {number} tabId - ID of tab to close.
   */
  self.closeTab = tabId => {
    console.debug(`closing tab #${tabId} ...`);
    return browser.tabs.remove(tabId);
  };

  /**
   * Handle "recently-closed" command.
   * @return {Promise} - Array of {sessionId,title,url,lastModified}, newest first.
   */
  self.recentlyClosed = () => {
    return browser.sessions.getRecentlyClosed().then(sessions => {
      let out = [];
      sessions.forEach(s => {
        if (s.tab) {
          out.push({
            sessionId: s.tab.sessionId || '',
            title: s.tab.title || s.tab.url || '',
            url: s.tab.url || '',
            lastModified: s.lastModified || 0,
          });
        }
      });
      console.debug(`${out.length} recently-closed tab(s)`);
      return out;
    });
  };

  /**
   * Handle "restore-session" command.
   * @param {string} sessionId - Session to restore (empty = most recent).
   */
  self.restoreSession = sessionId => {
    console.debug(`restoring session ${sessionId} ...`);
    if (sessionId) return browser.sessions.restore(sessionId);
    return browser.sessions.restore();
  };

  /**
   * Handle "mute-tab" command. Toggles the tab's muted state.
   * @param {number} tabId - ID of tab to (un)mute.
   */
  self.muteTab = tabId => {
    return browser.tabs.get(tabId).then(tab => {
      let muted = !(tab.mutedInfo && tab.mutedInfo.muted);
      console.debug(`setting muted=${muted} on tab #${tabId} ...`);
      return browser.tabs.update(tabId, { muted: muted });
    });
  };

  /**
   * Handle "move-tab-new-window" command. Moves the tab into a new window.
   * @param {number} tabId - ID of tab to move.
   */
  self.moveTabNewWindow = tabId => {
    console.debug(`moving tab #${tabId} to a new window ...`);
    return browser.windows.create({ tabId: tabId });
  };

  /**
   * Return active tab.
   * @param {number} winId - ID of window to get active tab of.
   * If 0 or null, current window is used.
   * @return {Promise} - Promise resolves to null or a tabs.Tab.
   */
  self.activeTab = winId => {
    winId = winId || browser.windows.WINDOW_ID_CURRENT;
    return browser.tabs
      .query({
        active: true,
        windowId: winId,
      })
      .then(tabs => {
        if (tabs.length) return tabs[0];
        return null;
      });
  };

  browser.runtime.onConnect.addListener(self.onConnected);
  self.connectNative();
  console.log(`started`);
};

browser.browserAction.setIcon({ path: iconDisconnected });
new Background();
