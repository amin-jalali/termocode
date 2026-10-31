package mcptest

import (
	"encoding/json"
	"os"
	"sync"

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

// The fake's one subscriptions/listen stream (stdio).
var listen struct {
	sync.Mutex
	id     json.RawMessage // the listen request's id; nil: none
	filter map[string]any
}

// Listen opens the subscription: the acknowledgement goes out, the
// request stays open until EndListen.
func Listen(c Notifier, id, params json.RawMessage) {
	var p struct {
		Notifications map[string]any `json:"notifications"`
	}
	_ = json.Unmarshal(params, &p)
	agreed := map[string]any{}
	for _, k := range []string{"toolsListChanged", "resourcesListChanged", "promptsListChanged"} {
		if v, _ := p.Notifications[k].(bool); v {
			agreed[k] = true
		}
	}
	listen.Lock()
	listen.id, listen.filter = id, agreed
	listen.Unlock()
	_ = c.Notify("notifications/subscriptions/acknowledged", map[string]any{
		"_meta": map[string]any{"io.modelcontextprotocol/subscriptionId": id}, "notifications": agreed})
}

// ListenCall runs the stateless tools that use the subscription:
// notify_changed / notify_resources send their notification on it (only
// when the client asked for that kind), end_listen ends it gracefully.
func ListenCall(c *extproc.Conn, name string) (any, bool) {
	listen.Lock()
	id, filter := listen.id, listen.filter
	listen.Unlock()
	tag := map[string]any{"io.modelcontextprotocol/subscriptionId": id}
	switch name {
	case "notify_changed", "notify_resources":
		kind, method := "toolsListChanged", "notifications/tools/list_changed"
		if name == "notify_resources" {
			kind, method = "resourcesListChanged", "notifications/resources/list_changed"
		}
		if id == nil || filter[kind] != true {
			return text("not subscribed"), true
		}
		_ = c.Notify(method, map[string]any{"_meta": tag})
		return text("notified"), true
	case "end_listen":
		if id == nil {
			return text("not subscribed"), true
		}
		listen.Lock()
		listen.id = nil
		listen.Unlock()
		_ = c.Reply(id, map[string]any{"resultType": "complete", "_meta": tag}, nil)
		return text("ended"), true
	}
	return nil, false
}

// The tasks extension: in stateless mode, when the client declares
// io.modelcontextprotocol/tasks, long_task and task_ask answer with a
// task handle; tasks/get reports "working" (with "step n") twice, then
// the result. task_ask asks a question (input_required) before it ends;
// tasks/update takes the answer. tasks/cancel ends a task.
var tasks struct {
	sync.Mutex
	polls  map[string]int
	answer map[string]string
	kind   map[string]string
	n      int
}

// ClientHasTasks reports a request whose _meta declares the extension.
func ClientHasTasks(params json.RawMessage) bool {
	var p struct {
		Meta struct {
			Caps struct {
				Extensions map[string]json.RawMessage `json:"extensions"`
			} `json:"io.modelcontextprotocol/clientCapabilities"`
		} `json:"_meta"`
	}
	_ = json.Unmarshal(params, &p)
	_, ok := p.Meta.Caps.Extensions["io.modelcontextprotocol/tasks"]
	return ok
}

// StartTask answers a task tool with a task handle (ok false: not one).
func StartTask(name string, params json.RawMessage) (any, bool) {
	if (name != "long_task" && name != "task_ask") || !ClientHasTasks(params) {
		return nil, false
	}
	tasks.Lock()
	defer tasks.Unlock()
	if tasks.polls == nil {
		tasks.polls, tasks.answer, tasks.kind = map[string]int{}, map[string]string{}, map[string]string{}
	}
	tasks.n++
	id := "task-" + string(rune('0'+tasks.n))
	tasks.polls[id], tasks.kind[id] = 0, name
	return map[string]any{"resultType": "task", "task": map[string]any{"taskId": id, "status": "working", "ttlMs": 60000, "pollIntervalMs": 20}}, true
}

// TaskMethod serves tasks/get, tasks/update and tasks/cancel.
func TaskMethod(method string, params json.RawMessage) (any, *extproc.Error) {
	var p struct {
		TaskID         string                     `json:"taskId"`
		InputResponses map[string]json.RawMessage `json:"inputResponses"`
	}
	_ = json.Unmarshal(params, &p)
	tasks.Lock()
	defer tasks.Unlock()
	kind, ok := tasks.kind[p.TaskID]
	if !ok {
		return nil, &extproc.Error{Code: extproc.CodeInvalidParams, Message: "unknown task " + p.TaskID}
	}
	switch method {
	case "tasks/cancel":
		tasks.kind[p.TaskID] = "cancelled"
		return map[string]any{}, nil
	case "tasks/update":
		if r, ok := p.InputResponses["who"]; ok {
			var a struct {
				Action  string          `json:"action"`
				Content json.RawMessage `json:"content"`
			}
			_ = json.Unmarshal(r, &a)
			tasks.answer[p.TaskID] = a.Action + " " + string(a.Content)
		}
		return map[string]any{}, nil
	}
	task := map[string]any{"taskId": p.TaskID}
	n := tasks.polls[p.TaskID]
	tasks.polls[p.TaskID] = n + 1
	switch {
	case kind == "cancelled":
		task["status"] = "cancelled"
	case n < 2:
		task["status"], task["statusMessage"] = "working", "step "+string(rune('1'+n))
	case kind == "task_ask" && tasks.answer[p.TaskID] == "":
		task["status"] = "input_required"
		task["inputRequests"] = map[string]any{"who": map[string]any{"method": "elicitation/create", "params": map[string]any{
			"mode": "form", "message": "Who are you?",
			"requestedSchema": json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)}}}
	default:
		out := "task done"
		if a := tasks.answer[p.TaskID]; a != "" {
			out += ": " + a
		}
		task["status"], task["result"] = "completed", text(out)
	}
	return map[string]any{"resultType": "complete", "task": task}, nil
}
