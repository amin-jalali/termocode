#!/usr/bin/env bash
# dev-cleanup.sh — free CPU, RAM and disk after test runs.
#
# Kills processes left behind by tests and removes old test temp folders.
# Only ORPHANS are stopped: processes whose parent is gone (re-parented to
# init / systemd). A test binary that `go test` still runs, or an nvim
# whose test is still running, is never touched. It only touches:
#   - Go test binaries (*.test, e2e.test) older than MIN_AGE seconds;
#   - nvim / gopls / dlv / termocode whose working folder is a test temp
#     folder (/tmp/termocode-*, Go's /tmp/Test*) or an agent worktree
#     (.claude/worktrees/);
#   - temp folders /tmp/termocode-testkit-*, termocode-demo-*,
#     termocode-app-test-*, go-build*, gopls-tempmod* older than
#     30 minutes.
# It never touches processes started from a normal folder (your own
# termocode, its nvim, editors' gopls) or the session socket folder.
#
#   scripts/dev-cleanup.sh          # do it
#   scripts/dev-cleanup.sh -n       # only show what it would do
set -u
dry=0
[ "${1:-}" = "-n" ] && dry=1
MIN_AGE=${MIN_AGE:-120} # seconds a process must have run to count as left behind
me=$(id -u)

# orphan: the parent is init or a systemd (user) manager.
is_orphan() {
	local ppid pcomm
	ppid=$(ps -o ppid= -p "$1" 2>/dev/null | tr -d ' ')
	[ -n "$ppid" ] || return 1
	[ "$ppid" = 1 ] && return 0
	pcomm=$(ps -o comm= -p "$ppid" 2>/dev/null)
	case "$pcomm" in systemd | init | launchd) return 0 ;; esac
	return 1
}

is_test_dir() {
	case "$1" in
	/tmp/termocode-* | /tmp/Test* | /tmp/go-build* | */.claude/worktrees/*) return 0 ;;
	esac
	return 1
}

killed=0
for pid in $(ps -u "$me" -o pid=); do
	[ "$pid" = "$$" ] && continue
	comm=$(ps -o comm= -p "$pid" 2>/dev/null) || continue
	etimes=$(ps -o etimes= -p "$pid" 2>/dev/null | tr -d ' ') || continue
	[ -n "$etimes" ] && [ "$etimes" -ge "$MIN_AGE" ] || continue
	cwd=$(readlink "/proc/$pid/cwd" 2>/dev/null || true)
	reason=""
	case "$comm" in
	*.test | e2e.test) reason="test binary" ;;
	nvim | gopls | dlv | termocode | __debug_bin*)
		if is_test_dir "$cwd"; then reason="$comm in a test folder"; fi
		;;
	esac
	[ -n "$reason" ] || continue
	is_orphan "$pid" || continue # its test (or go test) still runs it
	args=$(ps -o args= -p "$pid" | cut -c1-80)
	echo "kill $pid ($reason, ${etimes}s, cwd=$cwd): $args"
	if [ $dry = 0 ]; then
		kill "$pid" 2>/dev/null
		killed=$((killed + 1))
	fi
done

removed=0
while IFS= read -r -d '' d; do
	echo "remove $d"
	if [ $dry = 0 ]; then
		rm -rf "$d" && removed=$((removed + 1))
	fi
done < <(find /tmp -maxdepth 1 -user "$me" -mmin +30 \( -name 'termocode-testkit-[0-9]*' \
	-o -name 'termocode-demo-*' -o -name 'termocode-app-test-*' -o -name 'go-build*' \
	-o -name 'gopls-tempmod*' \) -print0 2>/dev/null)

echo "done: $killed process(es) stopped, $removed folder(s) removed$([ $dry = 1 ] && echo ' (dry run)')"
