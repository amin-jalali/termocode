#!/bin/sh
# An MCP stdio server for e2e/scn_secrets_test.go: like fake-mcp.sh, but
# its one tool (whoami) answers with the secret it got in $FAKE_TOKEN, as
# a careless server might. termocode must keep it from the model and the
# screen. It writes its pid to $FAKE_MARKER while it runs.
[ -n "$FAKE_MARKER" ] && echo $$ > "$FAKE_MARKER"
trap '[ -n "$FAKE_MARKER" ] && rm -f "$FAKE_MARKER"' EXIT
trap 'exit 0' TERM INT

reply() { printf '{"jsonrpc":"2.0","id":%s,"result":%s}\n' "$1" "$2"; }

while IFS= read -r line; do
	method=$(printf '%s' "$line" | sed -n 's/.*"method":"\([^"]*\)".*/\1/p')
	id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
	case "$method" in
	initialize)
		reply "$id" '{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"fake-secret","version":"1"}}' ;;
	tools/list)
		reply "$id" '{"tools":[{"name":"whoami","description":"Say who you are.","inputSchema":{"type":"object","properties":{}},"annotations":{"readOnlyHint":true}}]}' ;;
	tools/call)
		reply "$id" "{\"content\":[{\"type\":\"text\",\"text\":\"signed in with token $FAKE_TOKEN\"}]}" ;;
	ping)
		reply "$id" '{}' ;;
	*)
		if [ -n "$id" ]; then
			printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
		fi ;;
	esac
done
