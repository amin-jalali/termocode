#!/usr/bin/env bash
# ci-changes.sh — which CI jobs a change needs.
#
#   git diff --name-only BASE...HEAD | scripts/ci-changes.sh <event>
#
# Prints code=, web=, heavy= and e2e= (true/false). Only a pull_request
# is filtered: a push or a run started by hand runs every job (main always
# tests everything), and so does a PR whose file list is empty (unknown
# base).
#   code   false when the change only touches docs that no test reads
#   web    the web-e2e jobs: web code (WEB_DEPS) or the browser suite changed
#   heavy  golden screens and the macOS unit tests: false when only browser
#          sources (not Go) or e2e files changed
#   e2e    the e2e suite: false when only the browser suite (e2e/web/) changed
# No pipefail: "grep -q" may close a pipe early on purpose.
set -eu

# Docs no test reads: top-level *.md, ADRs, concept pages, media.
# docs/guides, docs/reference … are embedded in the app (docs/embed.go).
DOCS='^([^/]+\.md|docs/(adr|concepts)/.*|assets/(gifs|screenshots)/.*|\.github/PULL_REQUEST_TEMPLATE\.md|LICENSE.*|requirements-docs\.txt)$'
# Changes that run every job: modules, vendored code, CI itself.
ALL='^(go\.(mod|sum)|e2e/go\.(mod|sum)|third_party/.*|\.github/workflows/.*|scripts/ci-.*)$'
# The packages the browser suite (e2e/web) builds on, the web commands and
# the web tests of the e2e module. The "checks" job fails when e2e/web
# starts to import a package that is missing here.
WEB_DEPS='^(internal/(webgw|webproto|webhl|webdevices|webtools|webtotp|webpush|blocks|services|session|workspace|spawn|crash|lazyre|termfont|relay|relaytun)/|cmd/web[^/]*/|e2e/web|e2e/ssh_web)'
# Browser sources: embedded in the binary, but not Go code.
BROWSER='^internal/webgw/assets/.*\.(js|ts|css|html|svg|json|webmanifest|woff2?|ttf|txt|lock|png)$'

event=${1:-}
files=$(grep -v '^$' || true)

out() { printf 'code=%s\nweb=%s\nheavy=%s\ne2e=%s\n' "$1" "$2" "$3" "$4"; }

code=true
if [ -n "$files" ] && ! printf '%s\n' "$files" | grep -qvE "$DOCS"; then
	code=false
fi
if [ "$code" = false ]; then
	out false false false false
	exit 0
fi
if [ "$event" != pull_request ] || [ -z "$files" ] || printf '%s\n' "$files" | grep -qE "$ALL"; then
	out true true true true
	exit 0
fi

# The code files only (docs no test reads do not count below).
code_files=$(printf '%s\n' "$files" | grep -vE "$DOCS")
web=false heavy=false e2e=false
if printf '%s\n' "$code_files" | grep -qE "$WEB_DEPS|$BROWSER"; then
	web=true
fi
# heavy and e2e: anything outside the browser sources / the e2e module.
if printf '%s\n' "$code_files" | grep -vE "$BROWSER" | grep -qvE '^e2e/'; then
	heavy=true
fi
if printf '%s\n' "$code_files" | grep -qvE '^e2e/web/'; then
	e2e=true
fi
out true "$web" "$heavy" "$e2e"
