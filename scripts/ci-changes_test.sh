#!/usr/bin/env bash
# Tests for scripts/ci-changes.sh (run by the "checks" CI job).
set -euo pipefail
cd "$(dirname "$0")/.."

fail=0
# check "<event>" "<files, one per line>" "<expected output>"
check() {
	local got
	got=$(printf '%s' "$2" | scripts/ci-changes.sh "$1" | tr '\n' ' ')
	if [ "$got" != "$3" ]; then
		printf 'FAIL %s with files:\n%s\n  got:  %s\n  want: %s\n' "$1" "$2" "$got" "$3" >&2
		fail=1
	fi
}

all='code=true web=true heavy=true e2e=true '
none='code=false web=false heavy=false e2e=false '

# Pushes and runs by hand test everything that is code.
check push 'internal/app/app.go' "$all"
check workflow_dispatch '' "$all"
check push 'README.md' "$none"
# Unknown base (empty list) on a PR: everything.
check pull_request '' "$all"
# Docs no test reads.
check pull_request $'README.md\ndocs/adr/0001.md' "$none"
# Go-only change outside the web code: no web jobs.
check pull_request 'internal/app/app.go' 'code=true web=false heavy=true e2e=true '
check pull_request $'internal/git/log.go\ninternal/devtools/shotkit/testdata/golden/a.ans' 'code=true web=false heavy=true e2e=true '
# Web code: everything (the app embeds and serves it).
check pull_request 'internal/webgw/hub.go' "$all"
check pull_request 'internal/session/store.go' "$all"
# Browser sources only: no golden screens or macOS unit tests, but e2e
# (it loads the page through the real binary).
check pull_request $'internal/webgw/assets/src/ui/chat.ts\ninternal/webgw/assets/app.js' 'code=true web=true heavy=false e2e=true '
# The browser suite only.
check pull_request $'e2e/web/chat_test.go\ne2e/web/helpers.go' 'code=true web=true heavy=false e2e=false '
# CI itself and modules: everything.
check pull_request '.github/workflows/test.yml' "$all"
check pull_request 'scripts/ci-test-shard.sh' "$all"
check pull_request 'go.sum' "$all"
check pull_request 'e2e/go.mod' "$all"

[ "$fail" -eq 0 ] && echo "ci-changes: ok"
exit "$fail"
