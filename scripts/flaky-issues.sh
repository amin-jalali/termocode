#!/usr/bin/env bash
# flaky-issues.sh: file one GitHub issue per failed test in a `go test
# -json` output (the nightly flaky hunter, .github/workflows/nightly.yml).
# An open issue "Flaky: <TestName>" gets a comment; else a new issue.
#
#   scripts/flaky-issues.sh out.json "seed 1234 (TERMOCODE_MONKEY_SEED=1234)"
#
# Needs gh with GH_TOKEN (issues: write) and GITHUB_REPOSITORY. DRY_RUN=1
# prints the issues instead of filing them.
set -euo pipefail

json=$1
note=${2:-}
run_url="${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY:-}/actions/runs/${GITHUB_RUN_ID:-0}"
bin=$(mktemp -d)/failedtests
go build -o "$bin" ./cmd/failedtests

# gh calls read < /dev/null: they must not eat the list of failures.
"$bin" < "$json" | while IFS=$'\t' read -r pkg test runs; do
	name=${test:-$pkg}
	title="Flaky: $name"
	body=$(mktemp)
	{
		echo "\`$name\` in \`$pkg\` failed $runs runs in the nightly flaky hunter."
		echo
		echo "- Run: $run_url"
		echo "- Commit: ${GITHUB_SHA:-unknown}"
		if [ -n "$note" ]; then echo "- $note"; fi
		echo
		echo "Last lines of the failing run:"
		echo
		echo '```'
		"$bin" -pkg "$pkg" -test "$test" -tail 80 < "$json" | cut -c1-400
		echo '```'
	} > "$body"
	if [ "${DRY_RUN:-}" = 1 ]; then
		echo "== $title"
		cat "$body"
		continue
	fi
	num=$(gh issue list --state open --search "\"$title\" in:title" --json number,title \
		--jq ".[] | select(.title == \"$title\") | .number" < /dev/null | head -n1)
	if [ -n "$num" ]; then
		gh issue comment "$num" --body-file "$body" < /dev/null
	else
		gh issue create --title "$title" --body-file "$body" < /dev/null
	fi
done
