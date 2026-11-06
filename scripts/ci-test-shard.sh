#!/usr/bin/env bash
# ci-test-shard.sh — run shard I of N of a go test run (CI splits the slow
# test jobs over parallel runners with this).
#
#   scripts/ci-test-shard.sh I N [go test flags...]
#
# CI_DEAL_PKGS  package patterns dealt out whole, round-robin (default ./...)
# CI_SPLIT_PKGS packages split by top-level test name, round-robin over the
#               sorted names (default: the slow ones). They are left out of
#               the dealt packages.
# CI_TAGS       build tags for go list (pass -tags to go test as well)
#
# Every package of CI_DEAL_PKGS and every Test/Fuzz/Example function of
# CI_SPLIT_PKGS runs in exactly one shard. The test names are read from the
# *_test.go files (gofmt keeps "func TestX(" on one line); a name that is
# not built on this system matches nothing. -list I N prints the plan only.
set -euo pipefail

list=false
if [ "${1:-}" = "-list" ]; then
	list=true
	shift
fi
if [ $# -lt 2 ]; then
	echo "usage: $0 [-list] I N [go test flags...]" >&2
	exit 2
fi
i=$1 n=$2
shift 2
case "$i$n" in *[!0-9]*) echo "I and N must be numbers" >&2; exit 2 ;; esac
if [ "$n" -lt 1 ] || [ "$i" -lt 1 ] || [ "$i" -gt "$n" ]; then
	echo "need 1 <= I <= N" >&2
	exit 2
fi

deal=${CI_DEAL_PKGS-./...}
split=${CI_SPLIT_PKGS-./internal/app ./internal/testkit ./internal/mdview ./internal/git}
golist() { go list ${CI_TAGS:+-tags "$CI_TAGS"} "$@"; }

# The import paths of the split packages, to drop them from the dealt list.
split_paths=""
if [ -n "$split" ]; then
	# shellcheck disable=SC2086
	split_paths=$(golist $split)
fi

# Plain assignments, so a failing go list stops the script (set -e)
# instead of leaving an empty, green shard.
deal_list="" split_dirs=""
# shellcheck disable=SC2086
if [ -n "$deal" ]; then deal_list=$(golist $deal); fi
# shellcheck disable=SC2086
if [ -n "$split" ]; then split_dirs=$(golist -f '{{.Dir}}' $split); fi

pkgs=()
if [ -n "$deal" ]; then
	k=0
	while IFS= read -r p; do
		if [ -n "$split_paths" ] && printf '%s\n' "$split_paths" | grep -qxF "$p"; then
			continue
		fi
		if [ $((k % n)) -eq $((i - 1)) ]; then
			pkgs+=("$p")
		fi
		k=$((k + 1))
	done <<<"$deal_list"
fi

names=()
if [ -n "$split" ]; then
	k=0
	while IFS= read -r t; do
		if [ $((k % n)) -eq $((i - 1)) ]; then
			names+=("$t")
		fi
		k=$((k + 1))
	done < <(
		for d in $split_dirs; do
			cat "$d"/*_test.go 2>/dev/null || true
		done | sed -nE 's/^func ((Test|Fuzz|Example)[A-Za-z0-9_]*)\(.*/\1/p' | LC_ALL=C sort -u
	)
fi

if $list; then
	for p in "${pkgs[@]+"${pkgs[@]}"}"; do echo "pkg $p"; done
	for t in "${names[@]+"${names[@]}"}"; do echo "test $t"; done
	exit 0
fi

echo "shard $i/$n: ${#pkgs[@]} packages whole, ${#names[@]} tests of: $split"

status=0
pid_deal="" pid_split=""
if [ ${#pkgs[@]} -gt 0 ]; then
	go test "$@" "${pkgs[@]}" &
	pid_deal=$!
fi
if [ ${#names[@]} -gt 0 ]; then
	re="^($(IFS='|'; echo "${names[*]}"))\$"
	# shellcheck disable=SC2086
	go test "$@" -run "$re" $split &
	pid_split=$!
fi
if [ -n "$pid_deal" ]; then wait "$pid_deal" || status=1; fi
if [ -n "$pid_split" ]; then wait "$pid_split" || status=1; fi
exit "$status"
