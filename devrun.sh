#!/bin/zsh
# devrun.sh — run the workflow binary outside Alfred, with the Alfred environment
# variables AwGo requires. Used for manual/end-to-end verification against the
# live Firefox extension.
#
# Usage:  ./devrun.sh [-query foo] tabs
#         ./devrun.sh tab-groups
#
# By default it runs the binary built in this source directory. Set BIN to point
# somewhere else (e.g. the installed workflow copy).
set -e

HERE="${0:A:h}"
export alfred_workflow_bundleid="net.deanishe.alfred.firefox-assistant"
export alfred_workflow_cache="${TMPDIR:-/tmp}/alfred-firefox-devrun/cache"
export alfred_workflow_data="${TMPDIR:-/tmp}/alfred-firefox-devrun/data"
export alfred_workflow_version="0.2.2"
export alfred_workflow_name="Firefox Assistant (dev)"
export alfred_debug="1"
mkdir -p "$alfred_workflow_cache" "$alfred_workflow_data"

BIN="${BIN:-$HERE/alfred-firefox}"
cd "$HERE"
exec "$BIN" "$@"
