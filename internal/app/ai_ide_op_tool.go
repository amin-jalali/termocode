package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// ide_op: the AI runs one of termocode's own IDE operations, the very
// handler the web page runs for it (AI parity): the same checks, the
// same result, the same files written. It covers what no typed tool does
// (undo in a tab, bookmarks, snippets, layouts, keys, theme colors, user
// commands, stopping a task or the tests, debugging one test, stack
// frames and loaded sources, inlay hints, HTTP cancel / curl / import /
// save body, the list of paired devices, …).
//
// It asks exactly where the GUI asks: an op the page sends to a laptop
// card (Policy.LaptopCard) asks every time, with no "Always allow", and
// then runs that card's own action; every other op runs at once, as on
// the page. Only the ops in aiIDEOps are offered; the rest of the web's
// ops either have a typed tool or are not for the AI (ai_parity.go).

// aiIDEOps are the web ops ide_op runs, each with what it does.
var aiIDEOps = map[string]string{
	// Editor and files
	"buf.undo":            "undo the last change in an open tab",
	"buf.reload":          "reload an open tab from disk",
	"buf.close":           "close an open tab",
	"buf.markdown":        "the Markdown preview of a tab, as HTML",
	"buf.head":            "a file as it is at git HEAD",
	"files.recent":        "the recent files",
	"bookmarks.list":      "the bookmarks",
	"bookmarks.toggle":    "set or clear a bookmark on a line",
	"bookmarks.clear":     "clear every bookmark",
	"snippets.list":       "the snippets for a tab's language",
	"snippets.user":       "the user's own snippets",
	"snippets.save":       "save a user snippet",
	"snippets.delete":     "delete a user snippet",
	"editor.vim_maps":     "the editor's key maps",
	"editor.vim_buf_maps": "a tab's own key maps",
	"search.replace":      "replace text in the workspace (asks)",
	// Terminal
	"term.colors": "a terminal tab's colors",
	// Services, tasks and tests
	"tasks.terminate":    "stop a running task (by its label)",
	"tests.stop":         "stop the running tests",
	"tests.debug":        "debug one test",
	"tests.refresh":      "find the tests again",
	"services.configure": "create .termocode/services.json with a template",
	"services.refresh":   "read the services again",
	// Debugger
	"debug.select":  "pick the debug configuration",
	"debug.run":     "run a configuration without the debugger",
	"debug.frame":   "show a stack frame",
	"debug.expand":  "the children of a variable",
	"debug.watch":   "add or remove a watch expression",
	"debug.sources": "the loaded sources",
	// LSP
	"lsp.inlay_hints": "the inlay hints of a range",
	// HTTP client
	"http.cancel":      "stop the request that runs",
	"http.curl":        "a request as a curl command",
	"http.import_curl": "a curl command as a request in a .http file",
	"http.save_body":   "save the last response body to a file",
	"http.saved":       "the saved requests",
	"http.env_file":    "create http-client.env.json with a template",
	// Settings, layouts, keys, theme, user commands, extensions
	"keys.list":       "the key bindings",
	"keys.bind":       "bind a key",
	"layout.list":     "the saved layouts",
	"layout.open":     "open a saved layout",
	"layout.save":     "save the layout",
	"layout.reset":    "reset the layout",
	"layout.delete":   "delete a saved layout (asks)",
	"theme.list":      "the themes",
	"theme.colors":    "the theme's colors",
	"theme.color_set": "set one theme color",
	"usercmds.list":   "the user's own commands",
	"usercmds.save":   "save a user command",
	"ext.panel":       "an extension's panel",
	"ext.select":      "pick a row of an extension's panel",
	// Web devices (list only: pairing and approvals stay with the user)
	"web.devices": "the paired devices",
}

// aiIDEOpWait caps an op that answers later.
var aiIDEOpWait = 60 * time.Second

func aiIDEOpList() []string {
	out := make([]string, 0, len(aiIDEOps))
	for op := range aiIDEOps {
		out = append(out, op)
	}
	sort.Strings(out)
	return out
}

// aiIDEOpFields are the JSON fields of an op's args.
func aiIDEOpFields(p webproto.Policy) string {
	if p.New == nil {
		return ""
	}
	t := reflect.TypeOf(p.New())
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return ""
	}
	var fs []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" || !f.IsExported() {
			continue
		}
		fs = append(fs, name+":"+aiIDEKind(f.Type))
	}
	return strings.Join(fs, ", ")
}

func aiIDEKind(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint32, reflect.Uint64, reflect.Float64:
		return "number"
	case reflect.Slice:
		return "list"
	}
	return "object"
}

// aiIDEOpCheck decodes the call: the op's policy and its checked args.
func aiIDEOpCheck(a map[string]any) (webproto.Policy, any, error) {
	op := ai.ArgString(a, "op")
	if _, ok := aiIDEOps[op]; !ok {
		return webproto.Policy{}, nil, errors.New("give op: one of the ops listed in the tool")
	}
	p, ok := webproto.Lookup(op)
	if !ok {
		return p, nil, errors.New("unknown op")
	}
	raw := json.RawMessage(`{}`)
	if v, ok := a["args"]; ok && v != nil {
		b, err := json.Marshal(v)
		if err != nil {
			return p, nil, errors.New("args must be an object")
		}
		raw = b
	}
	args, err := webproto.DecodeArgs(op, raw)
	if err != nil {
		return p, nil, fmt.Errorf("args are not valid for %s (%s): %v", op, aiIDEOpFields(p), err)
	}
	return p, args, nil
}

// aiIDEOpRun runs op on the UI goroutine as the page would, and waits
// for its answer.
func aiIDEOpRun(ctx context.Context, call aiIDECaller, p webproto.Policy, args any) (string, error) {
	ch := make(chan webproto.Msg, 1)
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		conn := &webConn{hub: m.web, id: "ai", role: p.Role, device: "AI", name: "AI", ctx: ctx, subs: map[string]*webSub{}}
		inner := &webCall{ctx: ctx, conn: conn, op: p.Op, args: args, sink: func(msg webproto.Msg) {
			select {
			case ch <- msg:
			default:
			}
		}}
		if ask := webAskOps[p.Op]; ask != nil { // the AI's card was the laptop card
			var a *webAsk
			func() {
				defer func() {
					if r := recover(); r != nil {
						recordError(fmt.Sprintf("[ai] ide_op %s panic: %v", p.Op, r))
						inner.fail(webproto.CodeInternal, "Something went wrong.")
					}
				}()
				a = ask(m, inner)
			}()
			if a == nil {
				return "", nil, nil // it failed: the answer is in ch
			}
			cmd := a.run(m)
			inner.reply(map[string]string{"done": a.title})
			return "", cmd, nil
		}
		fn := webOps[p.Op]
		if fn == nil {
			return "", nil, errors.New(p.Op + " is not available here")
		}
		return "", m.webRunOp(fn, inner), nil
	}); err != nil {
		return "", err
	}
	select {
	case msg := <-ch:
		switch r := msg.(type) {
		case *webproto.ErrMsg:
			return "", errors.New(r.Msg)
		case *webproto.Res:
			if len(r.Data) == 0 {
				return "done", nil
			}
			return string(r.Data), nil
		}
		return "", errors.New("no answer")
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(aiIDEOpWait):
		return "", errors.New(p.Op + " gave no answer in time")
	}
}

// aiIDEOpTool is ide_op.
func aiIDEOpTool(call aiIDECaller) ai.Tool {
	var lines []string
	for _, op := range aiIDEOpList() {
		p, _ := webproto.Lookup(op)
		line := op + " — " + aiIDEOps[op]
		if f := aiIDEOpFields(p); f != "" {
			line += " (args " + f + ")"
		}
		lines = append(lines, line)
	}
	return ai.Tool{
		Name:    "ide_op",
		Display: "IDE operation",
		Description: "Run one of termocode's own IDE operations, exactly as its web page runs it (the result is JSON). An op " +
			"marked (asks) asks the user every time; the others run at once. A tab is named by its buffer (get_editor_state " +
			"or open_file gives it). Ops:\n" + strings.Join(lines, "\n"),
		Params: map[string]any{
			"op":   map[string]any{"type": "string", "enum": aiIDEOpList(), "description": "the operation"},
			"args": map[string]any{"type": "object", "description": "the operation's args (see the list)"},
		},
		Required: []string{"op"},
		Validate: func(a map[string]any) error { _, _, err := aiIDEOpCheck(a); return err },
		Describe: func(a map[string]any) string {
			p, _, err := aiIDEOpCheck(a)
			if err != nil {
				return "ide_op (" + aiCardText(err.Error(), 80) + ")"
			}
			b, _ := json.Marshal(a["args"])
			s := aiIDEOps[p.Op]
			if p.LaptopCard {
				s = strings.TrimSuffix(s, " (asks)") + "?"
			}
			if len(b) > 2 && string(b) != "null" {
				s += "\n" + aiCardText(string(b), 300)
			}
			return strings.ToUpper(s[:1]) + s[1:]
		},
		Gate: func(a map[string]any) (bool, string) {
			p, ok := webproto.Lookup(ai.ArgString(a, "op"))
			if _, offered := aiIDEOps[p.Op]; !ok || !offered {
				return true, ""
			}
			return p.LaptopCard, ""
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			p, args, err := aiIDEOpCheck(a)
			if err != nil {
				return "", err
			}
			if call == nil {
				return "", errors.New("the IDE is not available")
			}
			return aiIDEOpRun(ctx, call, p, args)
		},
	}
}
