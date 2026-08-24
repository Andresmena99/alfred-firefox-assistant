# Contributing / maintaining

Everything you need to change this project and ship a release. For what it does
and how to install it, see the [README](README.md).

## Contents

- [Two copies, and why that matters](#two-copies-and-why-that-matters)
- [Toolchain](#toolchain)
- [Git layout](#git-layout)
- [Build and test](#build-and-test)
- [Run it by hand](#run-it-by-hand)
- [Logs](#logs)
- [The edit → verify loop](#the-edit--verify-loop)
- [Repo map](#repo-map)
- [Architecture](#architecture)
- [Recipe: add a new keyword](#recipe-add-a-new-keyword)
- [Ship a release](#ship-a-release)
- [Traps and recovery](#traps-and-recovery)
- [Keep internal identifiers out](#keep-internal-identifiers-out)
- [Working with an AI coding agent](#working-with-an-ai-coding-agent)

## Two copies, and why that matters

There are **two copies of this workflow and they can diverge**: the git clone you
edit, and the installed copy Alfred and Firefox actually run. Editing the clone
alone changes nothing.

```bash
export FF_SRC="$HOME/build/alfred-firefox"     # wherever you cloned it

# the installed folder has a per-install UUID — never hard-code it
export FF_WF="$(for d in "$HOME/Library/Application Support/Alfred/Alfred.alfredpreferences/workflows"/*/; do
  [ "$(/usr/libexec/PlistBuddy -c 'Print :bundleid' "$d/info.plist" 2>/dev/null)" \
    = "net.deanishe.alfred.firefox-assistant" ] && echo "${d%/}"
done)"
echo "$FF_WF"
```

## Toolchain

| Tool | Needed for |
| --- | --- |
| Go (built with 1.26.1; `go.mod` declares 1.13) | Everything |
| Xcode command line tools | `codesign`, `lipo` |
| `zsh`, `zip`, `PlistBuddy` | `server.sh`, packaging, plist edits |
| [`web-ext`](https://extensionworkshop.com/documentation/develop/web-ext-command-reference/#web-ext-sign) | Signing a release `.xpi` |
| [`gh`](https://cli.github.com/) | Creating releases, uploading assets |
| [`mage`](https://magefile.org/), [`modd`](https://github.com/cortesi/modd) | Optional; `magefile.go` and `modd.conf` are unused in practice |

## Git layout

```
origin    your fork
upstream  https://github.com/deanishe/alfred-firefox.git   (push should be DISABLED)
```

```bash
git remote set-url --push upstream DISABLED     # avoid pushing to Dean's repo
git fetch upstream && git merge upstream/master # pull his future changes
```

Two settings this clone needs, because pushes otherwise fail with
`HTTP 400` / `send-pack: unexpected disconnect`:

```bash
git config http.postBuffer 524288000
git config http.version HTTP/1.1
```

Use a per-repo identity so no private address is published:

```bash
git config user.email "<your-github-user>@users.noreply.github.com"
```

## Build and test

```bash
cd "$FF_SRC"
go build -o alfred-firefox .   # `go build` alone names the output after the MODULE
go vet ./...
gofmt -l *.go                  # must print nothing
source ./env && go test ./...  # 20 tests, ~0.3s, no browser needed
```

**The test trap.** The package-level `wf = aw.New(...)` in `main.go` runs at import
time and [AwGo](https://github.com/deanishe/awgo) panics without Alfred's
environment variables, so a bare `go test ./...` aborts before any test runs:

```
panic: invalid Workflow environment: alfred_workflow_bundleid is not set,
alfred_workflow_cache is not set, alfred_workflow_data is not set
```

That's a property of the repo, not a broken test. `source ./env` fixes it. For a
run that doesn't touch the live workflow's cache, point
`alfred_workflow_cache`/`_data` at `$TMPDIR` paths and `mkdir -p` them first —
AwGo opens a log file inside the cache dir without creating parents.

## Run it by hand

`devrun.sh` sets the same environment against throwaway directories, so you can
read the raw [Script Filter JSON](https://www.alfredapp.com/help/workflows/inputs/script-filter/json/)
Alfred consumes:

```bash
./devrun.sh tab-groups
./devrun.sh -query docs tab-groups
BIN="$FF_WF/alfred-firefox" ./devrun.sh tab-groups   # test the INSTALLED binary
./devrun.sh tab-groups | jq '.items[] | {title, subtitle, arg}'
```

Firefox must be running with the extension connected; otherwise these print a
single *"Cannot Connect to Extension"* item rather than crashing.

## Logs

```bash
CACHE="$HOME/Library/Caches/com.runningwithcrayons.Alfred/Workflow Data/net.deanishe.alfred.firefox-assistant"
tail -f "$CACHE/net.deanishe.alfred.firefox-assistant.server.log"   # native host — the useful one
tail -f "$CACHE/net.deanishe.alfred.firefox-assistant.log"          # Alfred-side client
```

At server startup a server log over 1 MiB rotates to `.server.log.1`, so one long
Firefox session can grow it well past that. `devrun.sh` writes its client log into
`$TMPDIR` instead. Extension logs: `about:debugging#/runtime/this-firefox` →
**Inspect** → Console. Per-keystroke client output also appears in
[Alfred's debugger](https://www.alfredapp.com/help/workflows/advanced/debugger/).

## The edit → verify loop

### Go changes

```bash
cd "$FF_SRC" && source ./env
go build -o alfred-firefox . && gofmt -l *.go && go vet ./... && go test ./...
cp alfred-firefox "$FF_WF/alfred-firefox"
codesign --force --sign - "$FF_WF/alfred-firefox" && codesign -v "$FF_WF/alfred-firefox"
```

On **codesign**: Go already ad-hoc signs `darwin/arm64` at link time (`codesign -dv`
reports `adhoc,linker-signed`), so a fresh build is validly signed and re-signing
isn't strictly required. Re-sign anyway — it costs nothing and is genuinely needed
after `lipo`-merging a universal binary, and after replacing a binary in place where
macOS can hold a stale signature for the path. Apple Silicon refuses to execute an
arm64 binary with an invalid signature: the kernel kills it and Alfred shows an
empty result list with no error.

Client-path changes (`client.go`, `tabgroups.go` rendering, `models.go`) are live
immediately, because Alfred runs the binary fresh per keystroke. Server-path changes
(`server.go`, `rpc_server.go`, `firefox.go`) need the long-running process replaced:

```bash
pkill -f 'alfred-firefox serve'   # Firefox relaunches it on the next call
```

### Extension changes

Don't sign through Mozilla to test a change. `manifest.json` pins the extension ID,
so an unpacked copy connects to the native host just as the signed one does:

1. **Disable the signed extension** in `about:addons` first — two copies sharing an
   ID fight over the connection.
2. `about:debugging#/runtime/this-firefox` → **Load Temporary Add-on…** →
   `extension/manifest.json`.
3. Hit **Reload** after each edit. Temporary add-ons vanish on Firefox restart.

### `info.plist` changes

Alfred owns `info.plist` in the *installed* folder and rewrites it whenever you
touch the workflow in its UI.

- **Quit Alfred before editing the installed `info.plist` by script**, or the edit
  can be clobbered.
- The clone's and the installed `info.plist` are two separate files with **no sync**.
  Change one, copy to the other.
- Validate after every scripted edit: `plutil -lint info.plist`.

## Repo map

### Go

| File | Responsibility |
| --- | --- |
| `main.go` | Entry point. Registers every subcommand in `rootCmd.Subcommands`; global flags (`-query`, `-tab`, `-url`, `-action`, `-bookmark`); computes socket, PID-file and log paths; `setup()` writes the native-messaging manifest; `registerMagic` implements *Register Workflow with Browser* |
| `models.go` | Wire types shared by client, server and extension: `Tab`, `TabGroup`, `ClosedTab`, `Bookmark`, `History`, `Download`. JSON tags here must match what `extension/alfred.js` emits |
| `client.go` | Largest file. The matching engine (`tabTokens`, `queryTerms`, `bestTermScore`, `entryScore`, `filterTabs`, `rank*`) plus the Alfred-facing subcommands for tabs, history, bookmarks, bookmarklets, downloads, actions, status and URL handling. `connectOrWarn()` and `relativeTime()` live here |
| `tabgroups.go` | The `tg` feature: `deriveTabGroups`, `loadTabGroups`, `groupContentTokens`, `groupScore`, `filterTabGroups`, `groupIcon`, `groupSubtitle`, `runTabGroups`, `mruTabInGroup`, `runTabGroupAction` |
| `rpc_client.go` | Thin client wrappers over [`net/rpc`](https://pkg.go.dev/net/rpc) — one method per `Firefox.*` call |
| `rpc_server.go` | The `Firefox.*` RPC methods. Each turns an RPC call into an extension command string and unmarshals the reply |
| `firefox.go` | Native-messaging codec: length-prefixed JSON on stdin/stdout, request/response correlation by ID |
| `server.go` | The `serve` subcommand: PID file, stale-socket cleanup, clean exit when Firefox closes stdin |
| `actions.go`, `actions_custom.go` | Built-in and user-script actions for tabs and URLs |
| `icons.go` | Icon constants, including `tabGroupIcons` (Firefox colour name → tinted PNG) |
| `client_match_test.go`, `tabgroups_test.go` | 20 unit tests over fixtures |

### Everything else

| Path | Responsibility |
| --- | --- |
| `extension/manifest.json` | MV2 manifest: version, permissions, and the ID / `strict_min_version` / `update_url` under `browser_specific_settings` |
| `extension/alfred.js` | Background page. `receiveNative()` dispatches a command string to a handler; each handler calls WebExtension APIs and resolves to JSON |
| `extension/.amo-upload-uuid` | Mozilla `web-ext` upload state. **Gitignored — never commit.** Don't delete it either; it links this tree to its AMO submission |
| `updates.json` | Firefox update manifest. Its `update_link` must point at a real release asset |
| `info.plist` | The Alfred workflow: keywords, hotkeys, node graph, workflow variables, `version`, and the `readme` Alfred shows in its workflow list |
| `server.sh` | zsh wrapper Firefox executes; sets the AwGo environment, then `exec`s `alfred-firefox serve` |
| `env` | Sourceable bootstrap exporting the AwGo variables into your shell |
| `devrun.sh` | Runs the binary outside Alfred. Development only |
| `doc/` | Upstream documentation, unmodified |
| `dist/`, `build/` | Build output. Gitignored |

## Architecture

Alfred runs the binary in client mode; it talks over a UNIX socket to a
long-running `serve` process; that process talks to the extension over Firefox's
native-messaging channel. Four facts explain most confusing behaviour:

1. **Firefox starts the server, not Alfred.** When the extension loads it calls
   `runtime.connectNative`; Firefox reads `net.deanishe.alfred.firefox.json`,
   executes the `server.sh` it points at, and that script `exec`s
   `alfred-firefox serve`. No Firefox, no server — and every keyword fails with
   *"Cannot Connect to Extension"*.
2. **The server pins the binary it was started with.** Replacing the binary changes
   nothing until Firefox restarts or the extension reconnects. Alfred-side
   invocations run fresh per keystroke and pick up a new binary immediately.
3. **`server.sh` fakes an Alfred environment.** It reads `bundleid`, `version` and
   `name` out of `info.plist` with `PlistBuddy` and exports them as
   `alfred_workflow_*` variables, because AwGo refuses to initialise without them.
4. **AppleScript is used, but only to foreground Firefox.** Every tab and group
   operation goes through the extension.

## Recipe: add a new keyword

This is how `tg` was added. A keyword that only reshapes data the extension already
returns needs steps 1–3 and 6; one needing new browser data needs all of them.

**1. Add the wire type** — `models.go`. JSON tags must match what the extension emits.

**2. Add the subcommand:**

```go
var myThingCmd = &ffcli.Command{
    Name:      "my-thing",
    Usage:     "alfred-firefox [-query <query>] my-thing",
    ShortHelp: "search my things",
    Exec:      runMyThing,
}
```

Register it in `main.go` → `rootCmd.Subcommands`. **Forgetting this fails silently:**
the build succeeds and the subcommand prints usage instead of results.

**3. Emit Alfred items:**

```go
wf.Configure(aw.SuppressUIDs(true))      // keep OUR ordering; Alfred re-sorts by usage otherwise
c, ok := connectOrWarn()                 // renders "Cannot Connect to Extension" and returns false
if !ok { return nil }
wf.NewItem(name).
    Subtitle(subtitle).
    Arg(id).                             // becomes "$1" in the action script
    UID(fmt.Sprintf("mything:%s", id)).
    Valid(true).
    Icon(icon).
    Var("CMD", "my-thing-action")         // selects the action subcommand
wf.WarnEmpty("No Results", "Try a different query")
wf.SendFeedback()
```

**4. Add the RPC method** — `rpc_client.go` (client) and `rpc_server.go` (server,
which sends the command string to the extension):

```go
func (c *rpcClient) MyThings() ([]MyThing, error) {
    var out []MyThing
    err := c.client.Call("Firefox.MyThings", "", &out)
    return out, err
}
```

**5. Add the extension handler** — `extension/alfred.js`: a `case 'my-things':` in
`receiveNative()`'s switch plus a handler returning a Promise. Feature-detect
anything that might be missing:

```js
if (!(browser.tabGroups && browser.tabGroups.query)) {
  return Promise.reject(new Error('tabGroups API unavailable'));
}
```

Bump `extension/manifest.json` → `version`, and add any new permission.

**6. Add the Alfred keyword** — a
[Script Filter](https://www.alfredapp.com/help/workflows/inputs/script-filter/) node
in `info.plist` with `script` = `./alfred-firefox -query "$1" my-thing` and
[keyword-with-space](https://www.alfredapp.com/help/workflows/inputs/keyword/)
enabled, connected to the shared debug node. Every keyword flows through one
pipeline:

```
keyword Script Filter
  → debug
  → conditional  ({var:CMD} == "actions" ? Actions Script Filter : else)
  → hide Alfred
  → debug
  → run script:  ./alfred-firefox $CMD "$1"
```

The action subcommand is chosen by the `CMD`
[workflow variable](https://www.alfredapp.com/help/workflows/advanced/variables/)
your item sets; the item's `Arg` arrives as `"$1"`. No new action nodes needed.

**This step is the one that fails silently.** Skip it and the build, tests and
`devrun.sh` all still pass — but the keyword doesn't exist in Alfred, because Alfred
only knows about keywords in `info.plist`. Editing via Alfred's UI is the low-risk
path. If you have no GUI, copy the `tg` node and change the five marked values:

```xml
<dict>
  <key>config</key>
  <dict>
    <key>alfredfiltersresults</key><false/>
    <key>alfredfiltersresultsmatchmode</key><integer>0</integer>
    <key>argumenttreatemptyqueryasnil</key><true/>
    <key>argumenttrimmode</key><integer>0</integer>
    <key>argumenttype</key><integer>1</integer>
    <key>escaping</key><integer>102</integer>
    <key>keyword</key><string>tg</string>                                  <!-- CHANGE -->
    <key>queuedelaycustom</key><integer>3</integer>
    <key>queuedelayimmediatelyinitially</key><true/>
    <key>queuedelaymode</key><integer>0</integer>
    <key>queuemode</key><integer>1</integer>
    <key>runningsubtext</key><string>Loading tab groups…</string>          <!-- CHANGE -->
    <key>script</key><string>./alfred-firefox -query "$1" tab-groups</string> <!-- CHANGE -->
    <key>scriptargtype</key><integer>1</integer>
    <key>scriptfile</key><string></string>
    <key>subtext</key><string>Search Firefox Tab Groups</string>           <!-- CHANGE -->
    <key>title</key><string>Firefox Tab Groups</string>                    <!-- CHANGE -->
    <key>type</key><integer>5</integer>
    <key>withspace</key><true/>
  </dict>
  <key>type</key><string>alfred.workflow.input.scriptfilter</string>
  <key>uid</key><string>GENERATE-A-FRESH-UUID-HERE</string>
  <key>version</key><integer>3</integer>
</dict>
```

Append it to the `objects` array, generate a fresh `uid` (`uuidgen`), then add one
`connections` entry keyed by that uid pointing at the shared debug node
`56FBB613-EE25-4DE4-930D-C1F51B9235D8`:

```xml
<key>YOUR-NEW-UUID</key>
<array>
  <dict>
    <key>destinationuid</key><string>56FBB613-EE25-4DE4-930D-C1F51B9235D8</string>
    <key>modifiers</key><integer>0</integer>
    <key>modifiersubtext</key><string></string>
    <key>vitoclose</key><false/>
  </dict>
</array>
```

Editing with Python's `plistlib` is safer than splicing XML. Quit Alfred first, edit
**both** `info.plist` files, `plutil -lint` each.

Each connection also carries `vitoclose`, which is Alfred's
["Don't close the Alfred window on actioning result"](https://www.alfredapp.com/help/workflows/advanced/alternative-actions/)
checkbox. Where it's `true`, the
[Hide Alfred](https://www.alfredapp.com/help/workflows/utilities/hide-alfred/) node
does the dismissal instead. This graph mixes both (`tab`, `bm`, `bml`, `hist` use
`true`; `dl`, `ffass`, `tg` use `false`) and both work, because every path reaches the
Hide Alfred node.

**7. Test.** Add a `_test.go` covering ranking and edge cases (empty query, no
matches, missing metadata), then verify live with `./devrun.sh`.

**8. Degrade gracefully.** If the feature needs a newer extension than users have,
detect the RPC failure and fall back. `loadTabGroups` is the pattern: try the
authoritative call, log at `[INFO]`, reconstruct from data older extensions already
return.

Two things worth knowing before you start:

- **A "list it, then act on it" feature needs two subcommands.** `tg` is
  `tab-groups` (renders the list) *plus* `tabgroup` (performs the switch), selected
  by `CMD`.
- **`models.go` and `extension/alfred.js` contain commented-out `Window` /
  `all-windows` / `current-window` stubs** from upstream. They're declarations, not
  implementations — uncommenting them yields nothing.

### Keyword-to-subcommand map

The Alfred keyword is *not* the Go subcommand:

| Keyword | Script Filter title | Subcommand |
| --- | --- | --- |
| `tab` | Firefox Tabs | `tabs` |
| `tg` | Firefox Tab Groups | `tab-groups` |
| `hist` | Firefox History | `history` |
| `bm` | Firefox Bookmarks | `bookmarks` |
| `bml` | Firefox Bookmarklets | `bookmarklets` |
| `dl` | Firefox Downloads | `downloads` |
| `ffass` | Firefox Assistant | `options` |
| *(hotkey only)* | Current Tab Actions | `current-tab` |
| *(internal)* | Actions | `actions` |

Action subcommands reached via `$CMD`: `tab`, `url`, `tabgroup`, `run-bookmarklet`,
`open`, `reveal`, `tab-info`.

## Ship a release

### 1. Binary and workflow package

```bash
cd "$FF_SRC" && source ./env
go build -o alfred-firefox . && go vet ./... && gofmt -l *.go && go test ./...
cp "$FF_WF/alfred-firefox" "$FF_WF/alfred-firefox.bak-$(date +%Y%m%d-%H%M%S)"   # rollback
cp alfred-firefox "$FF_WF/alfred-firefox"
codesign --force --sign - "$FF_WF/alfred-firefox" && codesign -v "$FF_WF/alfred-firefox"
cp README.md NOTICE LICENSE "$FF_WF/"

# bump `version` in BOTH info.plist files first, then package
mkdir -p dist && rm -f dist/Firefox-Assistant-*.alfredworkflow
( cd "$FF_WF" && zip -r -q "$FF_SRC/dist/Firefox-Assistant-<version>.alfredworkflow" . \
    -x '*.DS_Store' -x '*.bak-*' -x '__MACOSX/*' )
```

Verify before shipping: `unzip -l dist/*.alfredworkflow` should list
`alfred-firefox`, `info.plist`, `server.sh`, `icon.png`, `icons/`, `scripts/`,
`LICENCE.txt`, `README.md`, `NOTICE` — and **no** `.bak-*` files.

### 2. Extension `.xpi`

```bash
cd "$FF_SRC/extension"
# bump "version" in manifest.json FIRST — AMO rejects re-uploading an existing version
web-ext sign --channel=unlisted --artifacts-dir=../dist
```

Credentials come from the environment (`AMO_JWT_ISSUER`, `AMO_JWT_SECRET`,
`WEB_EXT_API_KEY`, `WEB_EXT_API_SECRET`) — never put them on the command line,
where they'd show in process listings.

- **Bump the version every time.** AMO refuses duplicates with
  `This upload has already been submitted.`
- **It takes longer than two minutes** (upload plus automated validation). Run it in
  the background. If it's killed *after* the upload lands, the version counts as
  submitted and a retry fails with the same duplicate error — query the
  [AMO API](https://mozilla.github.io/addons-server/topics/api/auth.html) to check
  whether it was in fact signed before re-running.
- **Unlisted** means
  [self-distributed](https://extensionworkshop.com/documentation/publish/self-distribution/):
  not reviewed, not searchable on AMO, permanently installable on Firefox Release.
- `web-ext sign` **uploads the extension source to Mozilla**, even for unlisted builds.

### 3. Publish and wire up auto-update

```bash
cd "$FF_SRC"
R=<owner>/<repo>
V=v<workflow-version>

git tag -a "$V" -m "..." && git push origin "$V"

# always pass --repo (see gotchas below)
gh api -X POST "repos/$R/releases" -f tag_name="$V" -f name="..." -f body="..." \
  -F draft=false -F prerelease=false
gh release upload "$V" --repo "$R" \
  dist/Firefox-Assistant-<version>.alfredworkflow \
  dist/alfred-firefox-assistant-signed-<extversion>.xpi --clobber
gh release edit "$V" --repo "$R" --notes-file notes.md
```

**Two `gh` gotchas.** `gh release create` fails with *"workflow scope may be
required"* — a wrong guess; the `repo` scope is sufficient and creating the release
through `gh api` works. And because an `upstream` remote exists, `gh` resolves
release subcommands to Dean's repo and reports *"release not found"* — **always pass
`--repo`**.

Then **update `updates.json`** so its `update_link` points at the new `.xpi` asset
URL, and commit it. Firefox reads `updates.json` from `raw.githubusercontent.com` on
the default branch, so auto-update only works once that file matches a real,
published asset. Getting this wrong is silent — Firefox just never offers the update.

Verify the chain unauthenticated before calling it done — this is what Firefox does:

```bash
curl -sI -L "https://raw.githubusercontent.com/$R/main/updates.json" | head -1
curl -sI -L "$(curl -s -L "https://raw.githubusercontent.com/$R/main/updates.json" \
  | python3 -c 'import json,sys;d=json.load(sys.stdin);k=list(d["addons"])[0];print(d["addons"][k]["updates"][0]["update_link"])')" | head -1
```

Both must return `200`; the second must serve `content-type: application/x-xpinstall`.

> **Bootstrapping onto auto-update is manual, once.** Extension builds before 1.4.1
> have no `update_url` baked in, so Firefox can never auto-update *to* 1.4.1 —
> anyone on 1.4.0 or earlier installs the new `.xpi` by hand one final time. Every
> release after that updates itself.

## Traps and recovery

| Action | What happens | Recovery |
| --- | --- | --- |
| Committing internal identifiers | Public repo, public history; forks may already have it | Check before every commit — see [below](#keep-internal-identifiers-out) |
| `git push` fails `HTTP 400` | Pack size vs. buffer | `git config http.postBuffer 524288000` and `http.version HTTP/1.1` |
| `gh release …` says *"release not found"* | `gh` resolved the `upstream` remote | Always pass `--repo <owner>/<repo>` |
| `gh release create` says *"workflow scope may be required"* | A wrong guess by `gh`; `repo` scope is sufficient | Create via `gh api -X POST repos/<owner>/<repo>/releases` |
| Committing the built binary | 11 MB per commit, forever | `/alfred-firefox`, `/alfred-firefox-assistant` and `/dist` are gitignored. `go build` with no `-o` names output after the *module* |
| Committing `extension/.amo-upload-uuid` | Leaks upload state | Gitignored; don't force-add it |
| Replacing the binary without `codesign` | Apple Silicon may refuse to execute it; Alfred shows nothing | `codesign --force --sign - "$FF_WF/alfred-firefox"` |
| Editing the installed `info.plist` while Alfred runs | Alfred can rewrite the file and drop the edit | Quit Alfred, re-apply, `plutil -lint` |
| Editing only the clone's `info.plist` | Nothing changes — Alfred runs the installed copy | Copy it into `$FF_WF` |
| `ffass` → **Register Workflow with Browser** | Rewrites the native-messaging manifest, allow-listing only `alfred-firefox@amenagod` and dropping other IDs | Re-add them by hand; harmless for this fork |
| Signing an `.xpi` without bumping the version | AMO rejects it; a run killed after upload leaves the version submitted | Bump and sign again |
| `updates.json` pointing at a non-existent asset | Auto-update silently never happens | Fix `update_link` and commit |
| Two copies of the extension enabled | Both claim the native host; connection becomes unpredictable | Disable one in `about:addons` |
| `mage link` | Creates a second workflow folder with the same bundle ID; keywords collide | Delete `<workflows>/net.deanishe.alfred.firefox-assistant` |
| Pushing to `upstream` | Would target Dean's repo | Set its push URL to `DISABLED` |

## Keep internal identifiers out

This repo is **public**. Before it was published, test fixtures and code comments
contained real internal hostnames, wiki paths, project codenames and a
change-management ID. Those were replaced with `example.com` placeholders that
preserve the exact matching semantics the tests exercise — CamelCase splitting,
acronym matching, hyphenated identifiers, and name-vs-content ranking.

**Never reintroduce them.** Check before every commit:

```bash
grep -rniE "(amazon|a2z\.com|aws\.dev|corp\.|quip-|midway|isengard|phonetool|brazil)" \
  --include="*.go" --include="*.js" --include="*.json" --include="*.md" . | grep -v "^./doc/"
```

`doc/` is upstream's unmodified documentation and is excluded. A non-empty result
means don't commit.

## Working with an AI coding agent

4,116 lines of Go across 14 files plus a 23 KB JavaScript background page, no network
in the test path. Paste this brief:

```text
Repo: an MIT fork of deanishe/alfred-firefox — Alfred 5 workflow (Go) + Firefox MV2
extension (JS). Branch: main. upstream = deanishe/alfred-firefox (push disabled).

Layout: main.go registers subcommands; client.go holds the matching engine and most
Alfred-facing commands; tabgroups.go holds the `tg` feature; rpc_client.go and
rpc_server.go bridge Alfred <-> the native host; extension/alfred.js is the browser
side; info.plist is the Alfred keyword graph.

Build:  go build -o alfred-firefox . && go vet ./... && gofmt -l *.go
Test:   the package panics without Alfred's env vars (AwGo validates at import).
        source ./env && go test ./...      # expect 20 tests passing
Run:    ./devrun.sh [-query foo] <subcommand>   (needs Firefox + extension connected)

THIS REPO IS PUBLIC. Never commit employer-internal hostnames, wiki paths, project
codenames, ticket IDs or aliases. Test fixtures use example.com placeholders only.
Run the sanitization grep in CONTRIBUTING.md before committing; expect no hits.

TWO THINGS THAT PASS EVERY GATE WHILE THE FEATURE STILL DOES NOT WORK:
1. A new Alfred KEYWORD requires a Script Filter node in info.plist. Without it the
   build, tests and devrun.sh all succeed and the keyword does not exist in Alfred.
   Alfred owns the installed copy: quit Alfred, edit BOTH info.plist files, plutil
   -lint each, and connect the new node to debug node
   56FBB613-EE25-4DE4-930D-C1F51B9235D8. Copy the `tg` node as a template.
2. A new EXTENSION command only exists in the working tree, not in the signed .xpi.
   To exercise it: disable the signed extension in about:addons, then load
   extension/manifest.json via about:debugging#/runtime/this-firefox ->
   "Load Temporary Add-on...". Otherwise devrun.sh returns "Cannot Connect to
   Extension" and the change looks broken when it is not.

Invariants — do not break:
- Every new subcommand must be added to rootCmd.Subcommands in main.go.
- Go struct JSON tags in models.go must match the keys extension/alfred.js emits.
- Call wf.Configure(aw.SuppressUIDs(true)) in any Script Filter command whose order
  matters, or Alfred re-sorts results by its own usage learning.
- New extension APIs must be feature-detected, and the workflow must degrade to data
  older extension versions already return rather than erroring.
- Extension ID alfred-firefox@amenagod must stay in sync with the allow-list in
  main.go setup() and the on-disk native-messaging manifest.
- Bump extension/manifest.json version before any re-sign; AMO rejects duplicates.
- Derived files keep BOTH the upstream copyright and the Modifications Copyright line.
  NOTICE records the change list; keep it current.
- Never commit the built binary or extension/.amo-upload-uuid (both gitignored).
- Never run `mage link`. Never push to `upstream`.
- gofmt clean, go vet clean, all 20 tests passing before claiming done.

Editing the clone alone changes nothing Alfred or Firefox runs: the installed copy is
a separate folder (find it by bundle ID, never hard-code the UUID). After replacing
the installed binary, re-sign with `codesign --force --sign -`.

Verification for any change: go build && go vet ./... && gofmt -l *.go && go test ./...
then ./devrun.sh against live Firefox and confirm the JSON output.
```

Two properties make agent work land correctly: the 20 unit tests are pure functions
over fixtures and run in ~0.3s with no browser, and `devrun.sh` gives a real
end-to-end check against live Firefox without touching Alfred. Require both on every
change.
