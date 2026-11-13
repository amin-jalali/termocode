package chatbridge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeConn hands out one batch of messages per Poll; after the last
// batch it stops the bridge.
type fakeConn struct {
	batches  [][]Message
	pollErrs []error
	sent     []string
	started  bool
	startErr error
	cancel   context.CancelFunc
}

func (f *fakeConn) Name() string { return "fake" }

func (f *fakeConn) Start(context.Context) error { f.started = true; return f.startErr }

func (f *fakeConn) Poll(ctx context.Context) ([]Message, error) {
	if len(f.pollErrs) > 0 {
		err := f.pollErrs[0]
		f.pollErrs = f.pollErrs[1:]
		return nil, err
	}
	if len(f.batches) == 0 {
		f.cancel()
		return nil, ctx.Err()
	}
	b := f.batches[0]
	f.batches = f.batches[1:]
	return b, nil
}

func (f *fakeConn) Send(_ context.Context, chat, text string) error {
	f.sent = append(f.sent, chat+": "+text)
	return nil
}

func runBridge(t *testing.T, b *Bridge, f *fakeConn) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f.cancel = cancel
	b.Conn = f
	b.Sleep = func(context.Context, time.Duration) {}
	return b.Run(ctx)
}

func TestBridgeAnswersOnlyAllowedPeople(t *testing.T) {
	var asked []string
	f := &fakeConn{batches: [][]Message{{
		{Chat: "c", User: "7", Text: "what does main.go do?"},
		{Chat: "x", User: "666", Text: "rm -rf everything"},
		{Chat: "x", User: "666", Text: "again"},
	}}}
	b := &Bridge{Allow: []string{"7"}, Handle: func(_ context.Context, m Message) (string, error) {
		asked = append(asked, m.Text)
		return "It starts the app.", nil
	}}
	if err := runBridge(t, b, f); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0] != "what does main.go do?" {
		t.Fatalf("only the allowed user's message runs: %v", asked)
	}
	if len(f.sent) != 2 || f.sent[0] != "c: It starts the app." {
		t.Fatalf("sent: %q", f.sent)
	}
	// A stranger is told once, with their id (so the owner can find
	// their own id the first time), and never again.
	if !strings.HasPrefix(f.sent[1], "x: ") || !strings.Contains(f.sent[1], "666") {
		t.Fatalf("stranger reply: %q", f.sent[1])
	}
}

func TestBridgeRefusesToStartWithNobodyAllowed(t *testing.T) {
	f := &fakeConn{}
	err := runBridge(t, &Bridge{Handle: func(context.Context, Message) (string, error) { return "", nil }}, f)
	if err == nil || f.started {
		t.Fatalf("an empty allow list must not start (anyone could use the AI): %v", err)
	}
}

func TestBridgeHelpAndWhoami(t *testing.T) {
	f := &fakeConn{batches: [][]Message{{{Chat: "c", User: "7", Text: "/start"}, {Chat: "c", User: "7", Text: "/whoami"}}}}
	ran := false
	b := &Bridge{Allow: []string{"7"}, Handle: func(context.Context, Message) (string, error) { ran = true; return "", nil }}
	if err := runBridge(t, b, f); err != nil {
		t.Fatal(err)
	}
	if ran || len(f.sent) != 2 || !strings.Contains(f.sent[0], "Ask") || !strings.Contains(f.sent[1], "7") {
		t.Fatalf("ran=%v sent=%q", ran, f.sent)
	}
}

func TestBridgeRedactsAndReportsFailures(t *testing.T) {
	f := &fakeConn{batches: [][]Message{{{Chat: "c", User: "7", Text: "a"}, {Chat: "c", User: "7", Text: "b"}, {Chat: "c", User: "7", Text: "c"}}}}
	n := 0
	b := &Bridge{Allow: []string{"7"},
		Redact: func(s string) string { return strings.ReplaceAll(s, "sk-SECRET", "***") },
		Handle: func(context.Context, Message) (string, error) {
			n++
			switch n {
			case 1:
				return "key is sk-SECRET", nil
			case 2:
				return "", errors.New("provider said sk-SECRET is bad")
			}
			return "   ", nil
		}}
	if err := runBridge(t, b, f); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(f.sent, "\n")
	if strings.Contains(all, "sk-SECRET") || len(f.sent) != 3 {
		t.Fatalf("sent: %q", f.sent)
	}
	if !strings.Contains(f.sent[1], "failed") || !strings.Contains(f.sent[2], "no answer") {
		t.Fatalf("failure and empty answer: %q", f.sent)
	}
}

func TestBridgeStopsOnUnauthorizedAndRetriesOtherErrors(t *testing.T) {
	f := &fakeConn{pollErrs: []error{errors.New("timeout"), errors.New("timeout")}, batches: [][]Message{{{Chat: "c", User: "7", Text: "hi"}}}}
	var waits []time.Duration
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f.cancel = cancel
	b := &Bridge{Conn: f, Allow: []string{"7"}, Handle: func(context.Context, Message) (string, error) { return "ok", nil },
		Sleep: func(_ context.Context, d time.Duration) { waits = append(waits, d) }}
	if err := b.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(waits) < 2 || waits[1] <= waits[0] || len(f.sent) != 1 {
		t.Fatalf("errors back off and the bridge goes on: waits %v sent %q", waits, f.sent)
	}

	f2 := &fakeConn{pollErrs: []error{&APIError{Status: 401, Msg: "Unauthorized"}}}
	err := runBridge(t, &Bridge{Allow: []string{"7"}, Handle: func(context.Context, Message) (string, error) { return "", nil }}, f2)
	if err == nil || !IsUnauthorized(err) {
		t.Fatalf("a revoked token stops the bridge: %v", err)
	}
}

func TestSplitKeepsRunesAndPrefersLineBreaks(t *testing.T) {
	parts := Split("ab\ncd\nef", 6)
	if strings.Join(parts, "|") != "ab\ncd|ef" {
		t.Fatalf("parts: %q", parts)
	}
	parts = Split(strings.Repeat("ü", 7), 3)
	if len(parts) != 3 || parts[0] != "üüü" {
		t.Fatalf("runes: %q", parts)
	}
	if p := Split("", 5); len(p) != 0 {
		t.Fatalf("empty: %q", p)
	}
}
