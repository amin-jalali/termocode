package mcptest

import (
	"encoding/json"
	"os"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// The stateless 2026-07-28 revision. FAKE_MODERN=1 makes the stdio fake
// dual-era: it answers server/discover, serves a request that carries
// the revision in its _meta statelessly, and still answers initialize.
// FAKE_MODERN=only refuses initialize (a modern-only server).

// ModernVersion is the stateless revision.
const ModernVersion = "2026-07-28"

// _meta keys of a stateless request.
const (
	MetaVersion = "io.modelcontextprotocol/protocolVersion"
	MetaClient  = "io.modelcontextprotocol/clientInfo"
	MetaCaps    = "io.modelcontextprotocol/clientCapabilities"
)

// codeUnsupportedVersion is UnsupportedProtocolVersionError.
const codeUnsupportedVersion = -32022

func modernMode() string { return os.Getenv("FAKE_MODERN") }

// RequestMeta reads the revision and client name of a request's _meta.
func RequestMeta(params json.RawMessage) (version, client string) {
	var p struct {
		Meta struct {
			Version string `json:"io.modelcontextprotocol/protocolVersion"`
			Client  struct {
				Name string `json:"name"`
			} `json:"io.modelcontextprotocol/clientInfo"`
		} `json:"_meta"`
	}
	_ = json.Unmarshal(params, &p)
	return p.Meta.Version, p.Meta.Client.Name
}

// DiscoverResult is the fake's server/discover answer.
func DiscoverResult() map[string]any {
	versions := []string{ModernVersion}
	if modernMode() != "only" {
		versions = append(versions, supported...)
	}
	return map[string]any{"resultType": "complete", "supportedVersions": versions,
		"capabilities": map[string]any{"tools": map[string]any{"listChanged": true},
			"resources": map[string]any{"listChanged": true}, "prompts": map[string]any{"listChanged": true}, "completions": map[string]any{}},
		"_meta": map[string]any{"io.modelcontextprotocol/serverInfo": map[string]any{"name": "fake-mcp-modern", "version": "2.0.0"}},
		"ttlMs": 60000, "cacheScope": "public"}
}

// unsupportedVersion is the error for a request without the revision.
func unsupportedVersion(requested string) *extproc.Error {
	data, _ := json.Marshal(map[string]any{"supported": []string{ModernVersion}, "requested": requested})
	return &extproc.Error{Code: codeUnsupportedVersion, Message: "Unsupported protocol version", Data: data}
}

// MRTR answers a stateless tools/call of the tools that need input from
// the client with an InputRequiredResult (Multi Round-Trip Requests),
// and the retry that carries the inputResponses with the final result:
//
//   - ask / ask_url: one elicitation/create (form / URL mode)
//   - roots: one roots/list
//   - sample: one sampling/createMessage (the client must refuse it)
//   - ask_twice: two rounds, "first" then "second", the state checked
//
// ok is false for every other tool.
func MRTR(name string, params json.RawMessage) (result any, rerr *extproc.Error, ok bool) {
	var p struct {
		InputResponses map[string]json.RawMessage `json:"inputResponses"`
		RequestState   *string                    `json:"requestState"`
	}
	_ = json.Unmarshal(params, &p)
	state := ""
	if p.RequestState != nil {
		state = *p.RequestState
	}
	need := func(key, method string, params any, st string) map[string]any {
		req := map[string]any{"method": method}
		if params != nil {
			req["params"] = params
		}
		return map[string]any{"resultType": "input_required", "inputRequests": map[string]any{key: req}, "requestState": st}
	}
	form := map[string]any{"mode": "form", "message": "Who are you?",
		"requestedSchema": json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","title":"Your name"}},"required":["name"]}`)}
	answer := func(raw json.RawMessage) string {
		var res struct {
			Action  string          `json:"action"`
			Content json.RawMessage `json:"content"`
		}
		_ = json.Unmarshal(raw, &res)
		out := res.Action
		if len(res.Content) > 0 {
			out += " " + string(res.Content)
		}
		return out
	}
	switch name {
	case "ask", "ask_url":
		r, have := p.InputResponses["who"]
		if !have || state != name+"-1" {
			q := form
			if name == "ask_url" {
				q = map[string]any{"mode": "url", "message": "Connect your account.", "url": "https://example.com/connect?x=1"}
			}
			return need("who", "elicitation/create", q, name+"-1"), nil, true
		}
		return text(answer(r)), nil, true
	case "roots":
		r, have := p.InputResponses["r"]
		if !have {
			return need("r", "roots/list", nil, "roots-1"), nil, true
		}
		var out struct {
			Roots []struct{ URI string } `json:"roots"`
		}
		_ = json.Unmarshal(r, &out)
		uris := ""
		for _, x := range out.Roots {
			uris += " " + x.URI
		}
		return text("roots:" + uris), nil, true
	case "sample":
		return need("s", "sampling/createMessage", map[string]any{"messages": []any{}, "maxTokens": 10}, "s-1"), nil, true
	case "ask_twice":
		switch state {
		case "":
			return need("a", "elicitation/create", form, "round-1"), nil, true
		case "round-1":
			if _, have := p.InputResponses["a"]; !have {
				return nil, &extproc.Error{Code: extproc.CodeInvalidParams, Message: "round 1 without its answer"}, true
			}
			return need("b", "elicitation/create", form, "round-2"), nil, true
		case "round-2":
			if _, have := p.InputResponses["a"]; have {
				return nil, &extproc.Error{Code: extproc.CodeInvalidParams, Message: "an old answer came back"}, true
			}
			return text("two rounds: " + answer(p.InputResponses["b"])), nil, true
		}
		return nil, &extproc.Error{Code: extproc.CodeInvalidParams, Message: "bad requestState"}, true
	}
	return nil, nil, false
}
