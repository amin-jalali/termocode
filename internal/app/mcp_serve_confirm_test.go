package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

type mcpServeTestCall struct {
	r   mcp.CallResult
	err error
}

// mcpServeStartCall runs one call on a goroutine.
func mcpServeStartCall(src *mcpServeSource, client, tool string, args map[string]any) chan mcpServeTestCall {
	ch := make(chan mcpServeTestCall, 1)
	go func() {
		r, err := src.Call(mcpServeCtx(client), tool, args)
		ch <- mcpServeTestCall{r, err}
	}()
	return ch
}

// mcpServePumpUntil plays Update for the hub until cond holds.
func mcpServePumpUntil(t *testing.T, m *Model, hub *mcpServeHub, what string, cond func() bool) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for !cond() {
		select {
		case msg := <-hub.ch:
			m.mcpServeTestHandle(msg)
		case <-time.After(20 * time.Millisecond):
			m.onMCPServeTick() // the tick the dropped cmd would send
		case <-deadline:
			t.Fatal("timed out waiting for " + what)
		}
	}
}

// mcpServeAwait plays Update until the call returns.
func mcpServeAwait(t *testing.T, m *Model, hub *mcpServeHub, ch chan mcpServeTestCall) mcpServeTestCall {
	t.Helper()
	var got mcpServeTestCall
	done := false
	mcpServePumpUntil(t, m, hub, "the call", func() bool {
		if done {
			return true
		}
		select {
		case got = <-ch:
			done = true
		default:
		}
		return done
	})
	return got
}

// mcpServeTestModel is a model serving MCP over a hub (no socket).
func mcpServeTestModel(t *testing.T) (*Model, *mcpServeSource, *mcpServeHub) {
	t.Helper()
	m := aiTestModel(t)
	m.output = newOutputStore()
	hub := newMCPServeHub()
	t.Cleanup(hub.stop)
	m.mcpServe = &mcpServeState{hub: hub}
	return &m, newMCPServeSource(hub, t.TempDir()), hub
}

// mcpServeCardOpen reports an open MCP card whose title has text.
func mcpServeCardOpen(m *Model, text string) bool {
	return m.confirmOpen && m.confirmKind == confirmKindMCPServe && strings.Contains(m.confirm.View(), text)
}

// mcpServeAnswer presses a card button as Update does.
func mcpServeAnswer(m *Model, id string) {
	m.confirmOpen = false
	m.handleConfirmSelect(id)
}

// Spec (MS-1): two calls that ask → one card at a time, in order; deny
// is an MCP error and the call does not run; the second card opens
// after the first closes; allow runs it. Every call is an audit line
// with by=mcp:<client>.
func TestMCPServeConfirmFIFO(t *testing.T) {
	m, src, hub := mcpServeTestModel(t)
	clear := map[string]any{"op": "clear"}
	first := mcpServeStartCall(src, "first", "debug_breakpoint", clear)
	mcpServePumpUntil(t, m, hub, "first card", func() bool { return mcpServeCardOpen(m, "first") })
	second := mcpServeStartCall(src, "second", "debug_breakpoint", clear)
	mcpServePumpUntil(t, m, hub, "second queued", func() bool { return len(m.mcpServe.queue) == 1 })
	if !mcpServeCardOpen(m, "first") {
		t.Fatal("the second call replaced the first card")
	}
	// An auto call still runs while a card is open.
	notify := mcpServeAwait(t, m, hub, mcpServeStartCall(src, "third", "notify", map[string]any{"title": "hi"}))
	if notify.err != nil || mcpServeText(notify.r) != "shown" {
		t.Fatalf("auto call behind a card: %+v", notify)
	}
	mcpServeAnswer(m, "deny")
	got := mcpServeAwait(t, m, hub, first)
	var ce *mcp.CallError
	if !errors.As(got.err, &ce) || ce.Code != mcp.CodeDeclined || !strings.Contains(ce.Message, "declined") {
		t.Fatalf("deny: %+v", got)
	}
	mcpServePumpUntil(t, m, hub, "second card", func() bool { return mcpServeCardOpen(m, "second") })
	mcpServeAnswer(m, "allow")
	got = mcpServeAwait(t, m, hub, second)
	if got.err != nil || len(got.r.Content) == 0 { // ran (no debugger here: the tool's own error text)
		t.Fatalf("allow: %+v", got)
	}
	if m.confirmOpen || m.mcpServe.active != nil || len(m.mcpServe.queue) != 0 {
		t.Fatal("a card is left over")
	}
	lines := strings.Join(m.output.Lines(aiAuditChannel), "\n")
	for _, want := range []string{
		"debug_breakpoint  op=clear  gate=confirm  by=mcp:first  → declined",
		"notify  title=hi  gate=auto  by=mcp:third  → shown",
		"debug_breakpoint  op=clear  gate=confirm:allowed  by=mcp:second",
	} {
		if !strings.Contains(lines, want) {
			t.Errorf("audit has no %q:\n%s", want, lines)
		}
	}
}

// Spec (MS-1): Esc on the card denies.
func TestMCPServeConfirmEscDenies(t *testing.T) {
	m, src, hub := mcpServeTestModel(t)
	call := mcpServeStartCall(src, "c", "debug_breakpoint", map[string]any{"op": "clear"})
	mcpServePumpUntil(t, m, hub, "card", func() bool { return mcpServeCardOpen(m, "c") })
	m.confirmOpen = false
	m.onAIConfirmClosed()
	var ce *mcp.CallError
	if got := mcpServeAwait(t, m, hub, call); !errors.As(got.err, &ce) || ce.Code != mcp.CodeDeclined {
		t.Fatalf("esc: %+v", got)
	}
}

// Spec (MS-1): a card nobody answers times out (-32002) and closes; the
// next call's card opens.
func TestMCPServeConfirmTimeout(t *testing.T) {
	old := mcpServeConfirmTimeout
	mcpServeConfirmTimeout = 150 * time.Millisecond
	t.Cleanup(func() { mcpServeConfirmTimeout = old })
	m, src, hub := mcpServeTestModel(t)
	call := mcpServeStartCall(src, "slow", "debug_breakpoint", map[string]any{"op": "clear"})
	mcpServePumpUntil(t, m, hub, "card", func() bool { return mcpServeCardOpen(m, "slow") })
	got := mcpServeAwait(t, m, hub, call)
	var ce *mcp.CallError
	if !errors.As(got.err, &ce) || ce.Code != mcp.CodeTimeout {
		t.Fatalf("timeout: %+v", got)
	}
	mcpServePumpUntil(t, m, hub, "card closed", func() bool { return !m.confirmOpen && m.mcpServe.active == nil })
}

// Spec (MS-1): a card waits while another dialog is open, and a card
// replaced by another dialog counts as a no.
func TestMCPServeConfirmWaitsForOtherDialog(t *testing.T) {
	m, src, hub := mcpServeTestModel(t)
	m.confirmOpen, m.confirmKind = true, confirmKindAITool // the chat asks
	call := mcpServeStartCall(src, "c", "debug_breakpoint", map[string]any{"op": "clear"})
	mcpServePumpUntil(t, m, hub, "queued", func() bool { return len(m.mcpServe.queue) == 1 })
	if m.confirmKind != confirmKindAITool || m.mcpServe.active != nil {
		t.Fatal("the MCP card covered the chat's card")
	}
	m.confirmOpen = false // the chat's card closes
	mcpServePumpUntil(t, m, hub, "card", func() bool { return mcpServeCardOpen(m, "c") })
	m.confirmKind = confirmKindAITool // another dialog replaces it
	var ce *mcp.CallError
	if got := mcpServeAwait(t, m, hub, call); !errors.As(got.err, &ce) || ce.Code != mcp.CodeDeclined {
		t.Fatalf("replaced card: %+v", got)
	}
}

// Spec (MS-1): stopping declines every waiting call.
func TestMCPServeDeclineAll(t *testing.T) {
	m, src, hub := mcpServeTestModel(t)
	a := mcpServeStartCall(src, "a", "debug_breakpoint", map[string]any{"op": "clear"})
	mcpServePumpUntil(t, m, hub, "card", func() bool { return mcpServeCardOpen(m, "a") })
	b := mcpServeStartCall(src, "b", "debug_breakpoint", map[string]any{"op": "clear"})
	mcpServePumpUntil(t, m, hub, "queued", func() bool { return len(m.mcpServe.queue) == 1 })
	m.mcpServe.declineAll()
	for _, ch := range []chan mcpServeTestCall{a, b} {
		if got := mcpServeAwait(t, m, hub, ch); got.err == nil {
			t.Fatal("a waiting call ran")
		}
	}
}

// Spec (MS-1): the card shows the call's text without terminal escapes,
// and at most mcpServeMaxQueue calls wait; more are declined at once.
func TestMCPServeConfirmCleanTextAndCap(t *testing.T) {
	m, _, hub := mcpServeTestModel(t)
	tool := ai.Tool{Name: "debug_evaluate", Mutating: true, Class: ai.ClassRun}
	first := &mcpServeAsk{ctx: context.Background(), client: "c", tool: tool, human: "x\x1b]52;c;cHduZWQ=\x07\x1b[2Jy", reply: make(chan bool, 1)}
	m.onMCPServeAsk(mcpServeAskMsg{hub: hub, ask: first})
	if !mcpServeCardOpen(m, "debug_evaluate") || strings.Contains(m.confirm.View(), "\x1b]52") || strings.Contains(m.confirm.View(), "\x1b[2J") {
		t.Fatalf("card: %q", m.confirm.View())
	}
	for i := 0; i < mcpServeMaxQueue; i++ {
		m.onMCPServeAsk(mcpServeAskMsg{hub: hub, ask: &mcpServeAsk{ctx: context.Background(), client: "c", tool: tool, reply: make(chan bool, 1)}})
	}
	extra := &mcpServeAsk{ctx: context.Background(), client: "c", tool: tool, reply: make(chan bool, 1)}
	m.onMCPServeAsk(mcpServeAskMsg{hub: hub, ask: extra})
	select {
	case ok := <-extra.reply:
		if ok {
			t.Fatal("an ask past the cap was allowed")
		}
	default:
		t.Fatal("an ask past the cap waits")
	}
	if len(m.mcpServe.queue) != mcpServeMaxQueue {
		t.Fatalf("queue = %d", len(m.mcpServe.queue))
	}
	m.mcpServe.declineAll()
}
