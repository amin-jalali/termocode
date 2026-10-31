package mcp

// Parts of the 2026-07-28 revision on the client side (MCPX-20) that
// work with old and new servers alike (the stateless mode itself is in
// spec2026_client.go, MRTR in spec2026_mrtr.go, subscriptions/listen in
// spec2026_listen.go):
//
//   - Mcp-Method / Mcp-Name headers on every HTTP request, so gateways
//     and load balancers can route without reading the body;
//   - resultType: a result that is not "complete" (a multi round trip
//     step) is a clear error instead of an empty answer;
//   - _meta of tools and results is kept (Tool.Meta, CallResult.Meta);
//   - ttlMs of tools/list: the saved list expires (manager.go).

// routeHeaders are the routing headers of one request: Mcp-Method always,
// Mcp-Name with the tool / prompt name or the resource URI. A value that
// is not plain printable ASCII is left out.
func routeHeaders(method string, params any) map[string]string {
	h := map[string]string{}
	if headerSafe(method) {
		h["Mcp-Method"] = method
	}
	p, ok := params.(map[string]any)
	if !ok {
		return h
	}
	var name string
	switch method {
	case "tools/call", "prompts/get":
		name, _ = p["name"].(string)
	case "resources/read", "resources/subscribe", "resources/unsubscribe":
		name, _ = p["uri"].(string)
	}
	if name != "" && headerSafe(name) {
		h["Mcp-Name"] = name
	}
	return h
}

func headerSafe(s string) bool {
	if len(s) == 0 || len(s) > 512 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}
