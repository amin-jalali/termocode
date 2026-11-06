#!/bin/sh
# A fake agent CLI for e2e/race_test.go, run as "aider" or "opencode"
# (symlinks): it shows the input box of that agent's screen rules, reads
# one task per line, shows a working line, edits files in its folder
# (lane 1 and lane 2 differently) and shows the input box again. It never
# talks to any network or model.
name=$(basename "$0")
lane=$(basename "$PWD")
box() {
	if [ "$name" = aider ]; then printf '> '; else printf 'fake agent · enter send\n'; fi
}
echo "fake $name in $lane (a banner, like the real CLI's)"
box
while IFS= read -r task; do
	if [ "$name" = aider ]; then printf 'waiting for model'; else printf 'esc interrupt'; fi
	sleep 1
	case "$lane" in
	race-*-1-*) printf 'note from lane one: %s\n' "$task" >lane-note.txt ;;
	*) printf 'lane two\n' >other.txt ;;
	esac
	printf '\r\033[K'
	box
done
