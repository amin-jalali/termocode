#!/bin/sh
# A tiny MCP stdio server for e2e/mcp_test.go: newline-delimited JSON-RPC
# on stdin / stdout. It creates $FAKE_MARKER while it runs (removed when
# it ends) and appends each method it gets to $FAKE_LOG. One tool: echo
# (read-only), which answers "echo from sh: <text>".
[ -n "$FAKE_MARKER" ] && echo $$ > "$FAKE_MARKER"
trap '[ -n "$FAKE_MARKER" ] && rm -f "$FAKE_MARKER"' EXIT
trap 'exit 0' TERM INT

reply() { printf '{"jsonrpc":"2.0","id":%s,"result":%s}\n' "$1" "$2"; }

while IFS= read -r line; do
	method=$(printf '%s' "$line" | sed -n 's/.*"method":"\([^"]*\)".*/\1/p')
	id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
	[ -n "$FAKE_LOG" ] && echo "$method" >> "$FAKE_LOG"
	case "$method" in
	initialize)
		reply "$id" '{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"fake-sh","version":"1"}}' ;;
	tools/list)
		reply "$id" '{"tools":[{"name":"echo","description":"Echo the text back.","inputSchema":{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]},"annotations":{"readOnlyHint":true}}]}' ;;
	tools/call)
		text=$(printf '%s' "$line" | sed -n 's/.*"text":"\([^"]*\)".*/\1/p')
		reply "$id" "{\"content\":[{\"type\":\"text\",\"text\":\"echo from sh: $text\"}]}" ;;
	ping)
		reply "$id" '{}' ;;
	*)
		# Notifications need no answer; unknown requests get an error.
		if [ -n "$id" ]; then
			printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
		fi ;;
	esac
done
