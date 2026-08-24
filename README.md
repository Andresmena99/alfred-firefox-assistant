<div align="center">
  <img src="icon.png" alt="Alfred Firefox Assistant" title="Alfred Firefox Assistant"/>
</div>

# Alfred Firefox Assistant

Search and switch Firefox **tabs, tab groups, history, bookmarks, downloads and
recently-closed tabs** from [Alfred](https://www.alfredapp.com/) — with
URL-aware fuzzy matching, tab-group colours and relative "last opened"
timestamps.

> **This is a modified fork** of Dean Jackson's
> [`deanishe/alfred-firefox`](https://github.com/deanishe/alfred-firefox) (MIT).
> It is **not** the upstream release, and the extension here is **not** the
> [public add-on on addons.mozilla.org](https://addons.mozilla.org/firefox/addon/alfred-launcher-integration/)
> — that one belongs to upstream and does not talk to this build. See
> [NOTICE](NOTICE) for the full list of changes.

## What's different from upstream

| | Upstream | This fork |
|---|---|---|
| Searches | Tab **title** only | Title **and** full URL |
| Query terms | Single in-order match | Every term must match, **in any order** |
| Matching | Subsequence over the whole string | **Per-token**, with CamelCase splitting and a title-match bonus |
| Ordering | Alfred's usage learning | **Most-recently-used**, with recency tie-breaks |
| `tab` results | Open tabs | Open tabs **+ history + recently-closed**, de-duplicated |
| Tab groups | — | **`tg` keyword**: browse/search groups, switch to one |
| Timestamps | — | `just now`, `2h ago`, `3d ago` on every result |

Because matching is per-token, queries like `TASK-151538290` and acronyms like
`spui` → `SettingsPageUI` resolve precisely instead of matching everything.

## Requirements

- **macOS.** The prebuilt binary is Apple Silicon (`arm64`) only — Intel Macs
  need a [rebuild](#intel-macs).
- **[Alfred 5](https://www.alfredapp.com/) with the
  [Powerpack](https://www.alfredapp.com/powerpack/)** — workflows are a
  Powerpack feature.
- **Firefox 139 or later.** The `tg` keyword uses the
  [`tabGroups`](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/API/tabGroups)
  API, which shipped in Firefox 139. The extension sets
  `strict_min_version: 139.0`.

## Install

Two components, **both required**. Firefox exposes no AppleScript API for tabs,
so the workflow cannot read the browser on its own. Install only the workflow
and every keyword reports *"No Connection to Browser"*.

Download both from the [latest release](../../releases/latest):

1. **The Alfred workflow** — double-click
   `Firefox-Assistant-<version>.alfredworkflow`. Alfred imports it. On first run
   it registers its
   [native-messaging](https://developer.mozilla.org/en-US/docs/Mozilla/Add-ons/WebExtensions/Native_messaging)
   host, which is what lets Firefox launch it.
2. **The Firefox extension** — open `about:addons` → gear ⚙ →
   **Install Add-on From File…** → choose
   `alfred-firefox-assistant-signed-<version>.xpi`. It is signed by Mozilla, so
   it installs permanently on Firefox Release with no `about:config` changes.
3. **Restart Firefox.** This is the step people skip. Firefox only launches the
   workflow's background process when the extension loads.
4. **Verify** — run `ffass` in Alfred. The first row must read **Connected**.
   Then try `tab ` and `tg ` (note the trailing space).

### macOS may block the binary the first time

The workflow binary is ad-hoc signed, not notarized by Apple, so Gatekeeper may
report *"cannot be opened because the developer cannot be verified."* Either
approve it once in **System Settings → Privacy & Security → Allow Anyway**, or
clear the quarantine attribute:

```sh
xattr -dr com.apple.quarantine ~/Library/Application\ Support/Alfred/Alfred.alfredpreferences/workflows/
```

## Keywords

| Keyword | Does | Notes |
|---|---|---|
| `tab [query]` | Search and switch open tabs. At 3+ characters, matching history and recently-closed tabs are folded in | Empty query lists all tabs, most-recently-used first |
| `tg [query]` | Browse and search **tab groups**; `↩` switches to the group | Empty query lists all groups, most-recently-used first. Matches group **name** *and* the contents of its tabs |
| `hist [query]` | Search history | Empty query shows most-recent history |
| `bm <query>` | Search bookmarks | Needs 3+ characters |
| `bml [query]` | Run a bookmarklet in the current tab | Empty query lists all bookmarklets |
| `dl [query]` | Search downloads | Open, or reveal in Finder |
| `ffass [query]` | Connection status and workflow links | Start here if something isn't working |

Press `⌘↩` on a result for more actions (close tab, close duplicates,
mute/unmute, move to new window, open in another browser, copy as Markdown),
or `⌘C` to copy the URL.

## `tg` — tab-group search

- **Find a group by name or by what's inside it.** A group matches if the query
  hits its name *or* any token from its member tabs' titles and URLs. Name hits
  carry a scoring bonus, so a group found by name usually outranks one found
  only by content.
- **Ranking.** No query → most-recently-used first, tie-broken by position in
  the tab strip. With a query → relevance first, recency breaks ties.
- **Colour-coded**, with the tab count and a `⊟ collapsed` marker in the
  subtitle. All nine Firefox palette colours are mapped.
- **`↩` switches to the group**: focuses its window, expands it if collapsed,
  and activates its most-recently-used tab.
- Unnamed groups appear as `Untitled group #<id>`.

## What the extension can access

The extension requests these permissions because it searches all of it locally:

| Permission | Why |
|---|---|
| `tabs`, `tabGroups` | Read tab and group titles, URLs and layout; switch, close, mute and move tabs |
| `history`, `bookmarks`, `sessions` | Search history and bookmarks; reopen recently-closed tabs |
| `downloads` | Search the download list |
| `nativeMessaging` | Launch and talk to the workflow binary on your Mac |
| `<all_urls>` | Run bookmarklets (`bml`) and the `inject` command in the current page |

It declares
[`data_collection_permissions: {"required": ["none"]}`](https://extensionworkshop.com/documentation/develop/firefox-builtin-data-consent/)
and talks only over the local native-messaging pipe to the binary on the same
machine. The background script makes no outbound network calls — check for
yourself rather than taking it on trust:

```sh
grep -niE "fetch\(|XMLHttpRequest|WebSocket|https?://" extension/alfred.js   # returns nothing
```

Be aware of what `<all_urls>` means, though: `bml` and `inject` execute
JavaScript in page context by design. The bookmarklets they run are your own,
from your own bookmarks, but the capability is there and no grep of
`alfred.js` constrains it. If you don't want it, remove `<all_urls>` from
`extension/manifest.json` and rebuild — `tab` and `tg` don't need it.

## Build from source

For the full maintainer guide — dev loop, adding a keyword, release process, known
traps — see **[CONTRIBUTING.md](CONTRIBUTING.md)**.

```sh
go build            # -> ./alfred-firefox
go vet ./...
gofmt -l *.go       # must print nothing
```

The package panics without Alfred's environment variables, because
[AwGo](https://github.com/deanishe/awgo) validates them at import time. Source
the bundled bootstrap first:

```sh
source ./env && go test ./...      # 20 tests, no browser needed
```

Run the binary outside Alfred to see the raw
[Script Filter JSON](https://www.alfredapp.com/help/workflows/inputs/script-filter/json/)
Alfred consumes (needs Firefox running with the extension connected):

```sh
./devrun.sh tab-groups
./devrun.sh -query docs tab-groups
./devrun.sh tab-groups | jq '.items[] | {title, subtitle, arg}'
```

To iterate on the extension, load it unpacked at
`about:debugging#/runtime/this-firefox` → **Load Temporary Add-on…** →
`extension/manifest.json`, and disable the signed copy first so the two don't
fight over the native-messaging connection. Signing through Mozilla is only
needed to cut a release.

### Intel Macs

```sh
GOARCH=amd64 go build -o /tmp/ff-amd64 .
```

Or build one binary that runs on both, which is what a release should ship:

```sh
GOARCH=arm64 go build -o /tmp/ff-arm64 .
GOARCH=amd64 go build -o /tmp/ff-amd64 .
lipo -create -output /tmp/ff-universal /tmp/ff-arm64 /tmp/ff-amd64
codesign --force --sign - /tmp/ff-universal
```

Go ad-hoc signs the `arm64` build at link time but leaves `x86_64` unsigned, so
`codesign -v` on the merged binary reports it unsigned until you re-sign it.

## How it works

Alfred runs the binary in client mode; it talks over a UNIX socket to a
long-running `serve` process; that process talks to the extension over Firefox's
native-messaging channel.

```
Alfred ──runs per keystroke──> alfred-firefox (client)
                                     │  net/rpc over /tmp/alfred-firefox.$UID.sock
                                     ▼
                               alfred-firefox serve  <──native messaging──>  extension  <──>  Firefox
                                     ▲
              Firefox launches server.sh when the extension connects
```

Three consequences worth knowing:

1. **Firefox starts the server, not Alfred.** No Firefox, no server — and every
   keyword fails with *"Cannot Connect to Extension"*.
2. **The server pins the binary it started with.** Replacing the binary changes
   nothing until Firefox restarts. Client-side changes take effect immediately,
   because Alfred runs the binary fresh per keystroke.
3. **AppleScript is used only to bring Firefox to the front.** Every tab and
   group operation goes through the extension.

## Updates

The extension carries an
[`update_url`](https://extensionworkshop.com/documentation/manage/updating-your-extension/)
pointing at [`updates.json`](updates.json) in this repository, so Firefox checks
for new versions automatically. This takes effect from extension **v1.4.1**
onward; earlier builds have no update URL baked in and must be replaced by hand.

The Alfred workflow itself has no auto-updater — download a newer
`.alfredworkflow` from [Releases](../../releases) and double-click it.

## Licence and credits

Released under the [MIT licence](LICENSE), the same as the original. Upstream's original notice is preserved verbatim in [LICENCE.txt](LICENCE.txt).

Based on [`deanishe/alfred-firefox`](https://github.com/deanishe/alfred-firefox)
by Dean Jackson. Built with [AwGo](https://github.com/deanishe/awgo),
[go.deanishe.net/fuzzy](https://github.com/deanishe/go-fuzzy) and
[peterbourgon/ff](https://github.com/peterbourgon/ff). Icons based on
[Font Awesome](https://fontawesome.com/).

Upstream's [documentation](https://github.com/deanishe/alfred-firefox/blob/v0.2.2/doc/index.md)
still describes the base commands accurately — see
[setup](https://github.com/deanishe/alfred-firefox/blob/v0.2.2/doc/setup.md),
[usage](https://github.com/deanishe/alfred-firefox/blob/v0.2.2/doc/usage.md),
[custom scripts](https://github.com/deanishe/alfred-firefox/blob/v0.2.2/doc/scripts.md)
and [troubleshooting](https://github.com/deanishe/alfred-firefox/blob/v0.2.2/doc/troubleshooting.md).
