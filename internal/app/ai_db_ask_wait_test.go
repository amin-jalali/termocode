package app

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Owner's bug (phone, 2026-11-09): an AI chat sat 4+ minutes on "Read
// the schema of 001" while its db_* tool waited on a password prompt
// nobody saw. The chat says what the tool waits for (on the page and in
// the TUI); nobody answering for 5 minutes ends the tool with a short
// error; calls that need the same answer at once share one question.

// aiDBFakeWait makes the long waits (a minute or more) of clock.After
// fire only when the test sends on the returned channel; asked gets
// each such wait's length. Shorter waits are real.
func aiDBFakeWait(t *testing.T) (fire chan time.Time, asked chan time.Duration) {
	t.Helper()
	fire, asked = make(chan time.Time), make(chan time.Duration, 8)
	restore := clock.SetAfter(func(d time.Duration) <-chan time.Time {
		if d < time.Minute {
			return time.After(d)
		}
		asked <- d
		return fire
	})
	t.Cleanup(restore)
	return fire, asked
}

// aiDBCountLogins counts the database logins (after aiDBRefuseBut).
func aiDBCountLogins(t *testing.T) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(ctx context.Context, c dbclient.Conn, pw string) (dbclient.Session, error) {
		n.Add(1)
		return old(ctx, c, pw)
	}
	t.Cleanup(func() { dbSQLSessionFactory = old })
	return &n
}

// Nobody answers the database's password for 5 minutes: the tool ends
// with a short clear error that tells the model not to call again at
// once, and the question closes on the laptop and in every page.
func TestAIDBAskTimesOutAfterFiveMinutes(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-81"
	defer srv.Close()
	aiTunForward(t, srv)
	e := newAIWebEnv(t, srv, aiTunBaaytona(), true)
	dbStoreSet(t, "db.baaytonadb.ssh", "tunnel-pw-81")
	aiDBRefuseBut(t, "never-typed", nil)
	fire, asked := aiDBFakeWait(t)

	ch := e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb", "schema": "public"})
	e.waitFor(t, "the laptop's password prompt", func(m *Model) bool { return m.promptOpen && m.promptKind == promptKindDBPassword })
	_ = e.askOf(t, e.phone)
	_ = e.askOf(t, e.desk)
	select {
	case d := <-asked:
		if d != 5*time.Minute {
			t.Fatalf("the tool waits %v, want 5m", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the tool waits with no time limit")
	}
	fire <- time.Now()
	r := aiTunWait(t, ch)
	if r.err == nil {
		t.Fatalf("no error after the wait: out %q", r.out)
	}
	msg := r.err.Error()
	if !strings.Contains(msg, "Waited 5 min for the database password of BaaytonaDB; nobody answered.") || !strings.Contains(msg, "Do not retry") || len(msg) > 240 {
		t.Fatalf("error %q", msg)
	}
	e.waitFor(t, "the laptop's prompt to close", func(m *Model) bool {
		m.webAfterUpdate(nil)
		return !m.promptOpen && m.databases.pendingSecretAI == nil
	})
	for _, c := range []*webConn{e.phone, e.desk} {
		if _, ans := e.drain(t, c); len(ans) != 1 {
			t.Errorf("%s heard %+v, want its ask closed", c.device, ans)
		}
	}
	e.viewerAsked(t)
}

// Two db_* calls at once need the same password: one question (one
// prompt on the laptop, one ask per page); the one answer reaches both
// calls, which both log in with it.
func TestAIDBParallelCallsShareOneAsk(t *testing.T) {
	const pw = "db-pw-82-secret"
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-82"
	defer srv.Close()
	aiTunForward(t, srv)
	e := newAIWebEnv(t, srv, aiTunBaaytona(), true)
	dbStoreSet(t, "db.baaytonadb.ssh", "tunnel-pw-82")
	aiDBRefuseBut(t, pw, nil)
	logins := aiDBCountLogins(t)

	ch1 := e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb", "schema": "public"})
	ch2 := e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb", "schema": "public"})
	e.waitFor(t, "the laptop's password prompt", func(m *Model) bool { return m.promptOpen && m.promptKind == promptKindDBPassword })
	deadline := time.Now().Add(5 * time.Second)
	for logins.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // the second call reaches the question
	select {
	case r := <-ch1:
		t.Fatalf("a call ended before the answer: %q, %v", r.out, r.err)
	case r := <-ch2:
		t.Fatalf("a call ended before the answer: %q, %v", r.out, r.err)
	default:
	}
	a := e.askOf(t, e.phone)
	if asks, _ := e.drain(t, e.phone); len(asks) != 0 {
		t.Fatalf("the phone was asked again: %+v", asks)
	}
	if f := e.call(t, e.phone, webproto.OpDBSecret, `{"conn":"baaytonadb","kind":"password","value":"`+pw+`"}`); webTestErr(f) != "" {
		t.Fatalf("db.secret: %v (ask %+v)", f, a)
	}
	for _, ch := range []<-chan aiTunResult{ch1, ch2} {
		if r := aiTunWait(t, ch); r.err != nil || !strings.Contains(r.out, "users") {
			t.Fatalf("out %q, err %v", r.out, r.err)
		}
	}
	e.ui(func(m *Model) {
		if m.promptOpen || m.databases.pendingSecretAI != nil {
			t.Error("a second prompt opened")
		}
	})
	if asks, _ := e.drain(t, e.phone); len(asks) != 0 {
		t.Errorf("the phone was asked again: %+v", asks)
	}
}

// While a db_* tool of the open chat's turn waits on the user, the chat
// says so: the newest message carries the wait (its text, connection and
// need) to every page, in the snapshot of a page that comes later too,
// and the TUI's activity line says "Waiting for you: …". Answered, the
// message goes out again without it.
func TestAIChatSaysWhatTheToolWaitsFor(t *testing.T) {
	const pw = "db-pw-83-secret"
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-83"
	defer srv.Close()
	aiTunForward(t, srv)
	e := newAIWebEnv(t, srv, aiTunBaaytona(), true)
	dbStoreSet(t, "db.baaytonadb.ssh", "tunnel-pw-83")
	aiDBRefuseBut(t, pw, nil)
	var sub *webConn
	e.ui(func(m *Model) {
		m.ai.conv = ai.Conversation{ID: "c-1", Title: "Schema", Messages: []ai.StoredMessage{
			{Role: ai.StoredUser, Text: "Read the schema of 001"},
			{Role: ai.StoredTool, Text: "⚙ Read the schema of 001"},
		}}
		m.ai.streaming = true
		m.ai.act.begin(clock.Now())
		m.ai.act.onEvent(ai.Event{Kind: ai.EventToolStart, Tool: "db_schema", Label: "db_schema"}, clock.Now())
		sub = webTestSub(t, m, "chat")
		webTestNow(t, sub)
	})

	ch := e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb", "schema": "public"})
	e.waitFor(t, "the laptop's password prompt", func(m *Model) bool { return m.promptOpen && m.promptKind == promptKindDBPassword })
	want := webproto.ChatWait{Text: "Database password for BaaytonaDB", Conn: "baaytonadb", Need: "password"}
	e.ui(func(m *Model) {
		m.webAfterUpdate(nil)
		var got *webproto.ChatWait
		for _, d := range webTestChatDeltas(t, webTestNow(t, sub)) {
			if d.Op == webproto.ChatMessage && d.Msg.I == 1 {
				got = d.Msg.Wait
			}
		}
		if got == nil || *got != want {
			t.Errorf("the tool line's wait is %+v, want %+v", got, want)
		}
		later := webTestSub(t, m, "chat")
		var s webproto.ChatSnap
		webTestData(t, webTestNow(t, later)[0], &s)
		if w := s.Messages[1].Wait; w == nil || *w != want || s.Messages[0].Wait != nil {
			t.Errorf("a later page's snapshot: %+v / %+v", s.Messages[1].Wait, s.Messages[0].Wait)
		}
		var tui strings.Builder
		for _, l := range m.aiActivityView(clock.Now()).lines("", 120) {
			tui.WriteString(l.text)
		}
		if !strings.Contains(tui.String(), "Waiting for you: Database password for BaaytonaDB") {
			t.Errorf("the TUI's activity line says %q", tui.String())
		}
	})
	if f := e.call(t, e.phone, webproto.OpDBSecret, `{"conn":"baaytonadb","kind":"password","value":"`+pw+`"}`); webTestErr(f) != "" {
		t.Fatalf("db.secret: %v", f)
	}
	if r := aiTunWait(t, ch); r.err != nil {
		t.Fatalf("out %q, err %v", r.out, r.err)
	}
	e.ui(func(m *Model) {
		m.webAfterUpdate(nil)
		cleared := false
		for _, d := range webTestChatDeltas(t, webTestNow(t, sub)) {
			if d.Op == webproto.ChatMessage && d.Msg.I == 1 {
				cleared = d.Msg.Wait == nil
			}
		}
		if !cleared {
			t.Error("the wait stayed on the tool line after the answer")
		}
		var tui strings.Builder
		for _, l := range m.aiActivityView(clock.Now()).lines("", 120) {
			tui.WriteString(l.text)
		}
		if strings.Contains(tui.String(), "Waiting for you") {
			t.Errorf("the TUI still says %q", tui.String())
		}
	})
}
