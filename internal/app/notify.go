package app

import (
	"fmt"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/nvim"
)

// ── nvim → Go event channel ─────────────────────────────────────────────
//
// Lua pushes events to Go with
//
//	_G.termocode_notify('<method>', arg1, arg2, ...)
//
// (a wrapper around vim.rpcnotify on vim.g.termocode_channel, installed by
// nvim.Client.Attach). Each method has ONE Go handler, registered from an
// init() in the feature's own file so shared files stay untouched:
//
//	func init() {
//		registerNotifyHandler("termocode_ai_changed", func(m *Model, args []any) tea.Cmd {
//			return m.debounce.Do("ai-inline", 300*time.Millisecond, func() tea.Msg { … })
//		})
//	}
//
// New() subscribes every registered method with the client; the resulting
// nvim.NotifyMsg is dispatched by handleNvimMsg → dispatchNotify inside
// Update, so handlers may mutate the Model freely. Use m.debounce for
// chatty events (TextChangedI, CursorMoved, …).

// notifyHandler handles one notification. args are the raw msgpack values
// (see the notifyArg* helpers). The returned Cmd may be nil.
type notifyHandler func(m *Model, args []any) tea.Cmd

// notifyHandlers maps method name → handler. Written only from init().
var notifyHandlers = map[string]notifyHandler{}

// registerNotifyHandler binds a handler to an rpcnotify method name. Call
// it from an init() function. Registering the same method twice panics —
// two features silently fighting over one event is always a bug.
func registerNotifyHandler(method string, h notifyHandler) {
	if method == "" || h == nil {
		panic("registerNotifyHandler: empty method or nil handler")
	}
	if _, dup := notifyHandlers[method]; dup {
		panic("registerNotifyHandler: duplicate method " + method)
	}
	notifyHandlers[method] = h
}

// notifyMethods returns the registered method names, sorted (stable order
// for subscription and tests).
func notifyMethods() []string {
	out := make([]string, 0, len(notifyHandlers))
	for k := range notifyHandlers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// subscribeNotify registers every known method with the nvim client so its
// notifications reach Next(). Called once from New().
func (m *Model) subscribeNotify() {
	if m.nvim == nil {
		return
	}
	for _, method := range notifyMethods() {
		if err := m.nvim.RegisterNotify(method, nil); err != nil {
			recordError(fmt.Sprintf("[notify] subscribe %s: %v", method, err))
		}
	}
}

// dispatchNotify runs the handler for msg.Method. Unknown methods are
// logged and dropped.
func (m *Model) dispatchNotify(msg nvim.NotifyMsg) tea.Cmd {
	h, ok := notifyHandlers[msg.Method]
	if !ok {
		recordError("[notify] no handler for " + msg.Method)
		return nil
	}
	return h(m, msg.Args)
}

// notifyArgString returns args[i] as a string ("" when missing / not a
// string).
func notifyArgString(args []any, i int) string {
	if i < 0 || i >= len(args) {
		return ""
	}
	switch v := args[i].(type) {
	case string:
		return v
	case []byte:
		return string(v)
	}
	return ""
}

// notifyArgInt returns args[i] as an int (0 when missing / not numeric).
func notifyArgInt(args []any, i int) int {
	if i < 0 || i >= len(args) {
		return 0
	}
	switch v := args[i].(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// notifyArgBool returns args[i] as a bool (false when missing).
func notifyArgBool(args []any, i int) bool {
	if i < 0 || i >= len(args) {
		return false
	}
	b, _ := args[i].(bool)
	return b
}

// notifyArgMap returns args[i] as a Lua table decoded to a map (nil when
// missing / not a table with string keys).
func notifyArgMap(args []any, i int) map[string]any {
	if i < 0 || i >= len(args) {
		return nil
	}
	mp, _ := args[i].(map[string]any)
	return mp
}

// Built-in method: Lua can write to an Output channel with
//
//	_G.termocode_notify('termocode_output', '<channel>', '<text>')
//
// Text may contain newlines; each becomes one line.
func init() {
	registerNotifyHandler("termocode_output", func(m *Model, args []any) tea.Cmd {
		ch := notifyArgString(args, 0)
		if ch == "" {
			ch = outputDefaultChannel
		}
		m.output.Append(ch, notifyArgString(args, 1))
		return nil
	})
}
