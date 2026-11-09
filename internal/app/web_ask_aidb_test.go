package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// aiWebEnv is aiTunEnv with the web hub on: two controller pages (a
// phone and a desktop) that approve their own actions, and a viewer.
// Every frame a page got is kept in its log.
type aiWebEnv struct {
	*aiTunEnv
	phone, desk, viewer *webConn
	logs                map[*webConn][]map[string]any
	read                map[*webConn]int // how much of each log drain handed out
}

func newAIWebEnv(t *testing.T, srv *dbtest.SSHServer, c dbclient.Conn, trusted bool) *aiWebEnv {
	t.Helper()
	e := &aiWebEnv{aiTunEnv: newAITunEnv(t, srv, c, trusted), logs: map[*webConn][]map[string]any{}, read: map[*webConn]int{}}
	e.ui(func(m *Model) {
		dbs := m.databases
		*m = aiTestModel(t)
		m.ws.Root = e.root
		m.output = newOutputStore()
		m.databases = dbs
		h := webTestHub(t, m)
		h.approveOnLaptop = false
		e.phone, e.desk, e.viewer = webTestSub(t, m), webTestSub(t, m), webTestSub(t, m)
		e.desk.device = "desktop"
		e.viewer.device, e.viewer.role = "viewer", webproto.RoleViewer
	})
	return e
}

// take moves what each page got into its log (on Update).
func (e *aiWebEnv) take(t *testing.T) {
	for _, x := range []*webConn{e.phone, e.desk, e.viewer} {
		e.logs[x] = append(e.logs[x], webTestNow(t, x)...)
	}
}

// drain returns the ask and answered events c got since the last drain
// of c (nil c: only takes).
func (e *aiWebEnv) drain(t *testing.T, c *webConn) (asks []webproto.AskCard, answered []webproto.Answered) {
	t.Helper()
	var got []map[string]any
	e.ui(func(*Model) {
		e.take(t)
		if c != nil {
			got = e.logs[c][e.read[c]:]
			e.read[c] = len(e.logs[c])
		}
	})
	for _, f := range got {
		switch {
		case f["t"] == "ev" && f["kind"] == webproto.EventAsk:
			var a webproto.AskCard
			webTestData(t, f, &a)
			asks = append(asks, a)
		case f["t"] == "ev" && f["kind"] == webproto.EventAnswered:
			var a webproto.Answered
			webTestData(t, f, &a)
			answered = append(answered, a)
		}
	}
	return asks, answered
}

// call runs op from page c as the hub does (the op, its Cmd, then the
// Update after it) and returns its answer; every frame stays in the logs.
func (e *aiWebEnv) call(t *testing.T, c *webConn, op, args string) map[string]any {
	t.Helper()
	var res map[string]any
	e.ui(func(m *Model) {
		e.take(t)
		a, err := webproto.DecodeArgs(op, json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s %s: %v", op, args, err)
		}
		c.lastID++
		call := &webCall{ctx: c.ctx, conn: c, id: c.lastID, op: op, args: a}
		c.inflight.Add(1)
		cmd, found := m.webRunCall(m.web, call)
		if !found {
			t.Fatalf("no handler for %s", op)
		}
		if cmd != nil {
			if am, ok := cmd().(applyMsg); ok {
				_ = am(m)
			}
		}
		m.webAfterUpdate(nil)
		n := len(e.logs[c])
		e.take(t)
		for _, f := range e.logs[c][n:] {
			if f["id"] == float64(call.id) {
				res = f
			}
		}
	})
	if res == nil {
		t.Fatalf("%s gave no answer", op)
	}
	return res
}

// askOf waits for the page's ask of a db_* tool and returns it.
func (e *aiWebEnv) askOf(t *testing.T, c *webConn) webproto.AskCard {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		asks, _ := e.drain(t, c)
		for _, a := range asks {
			if a.Need != nil {
				return a
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the page was not asked")
	return webproto.AskCard{}
}

// noSecret fails when value is in any frame a page got, the audit or the
// tool's answer.
func (e *aiWebEnv) noSecret(t *testing.T, value, out string) {
	t.Helper()
	e.drain(t, nil)
	for c, fs := range e.logs {
		for _, f := range fs {
			if b, _ := json.Marshal(f); strings.Contains(string(b), value) {
				t.Errorf("the secret is in a frame %s got: %s", c.device, b)
			}
		}
	}
	var audit string
	e.ui(func(m *Model) { audit = webTestAudit(m) })
	if strings.Contains(audit, value) {
		t.Errorf("the secret is in the audit:\n%s", audit)
	}
	if strings.Contains(out, value) {
		t.Errorf("the secret is in the tool's answer: %q", out)
	}
}

func (e *aiWebEnv) viewerAsked(t *testing.T) {
	t.Helper()
	e.drain(t, nil)
	for _, f := range e.logs[e.viewer] {
		if f["t"] == "ev" && (f["kind"] == webproto.EventAsk || f["kind"] == webproto.EventAnswered) {
			t.Errorf("the viewer got %v", f)
		}
	}
}

// Owner (2026-11-08, a paired control device is the owner): an AI db_*
// tool that meets an unknown SSH host key asks every controller page
// too (phone and desktop), with the key the laptop's card shows; Trust
// and save on the phone (db.trust_host, exactly that key) saves it, the
// tool answers, the laptop's card closes and the desktop's ask ends. A
// viewer is never asked.
func TestWebAIDBTrustCardInPage(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-91"
	defer srv.Close()
	aiTunForward(t, srv)
	e := newAIWebEnv(t, srv, aiTunBaaytona(), false)
	dbStoreSet(t, "db.baaytonadb.ssh", "tunnel-pw-91")

	ch := e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb", "schema": "public"})
	e.waitFor(t, "the laptop's trust card", func(m *Model) bool { return m.confirmOpen && m.confirmKind == confirmKindDBTrustHost })
	a := e.askOf(t, e.phone)
	d := e.askOf(t, e.desk)
	if a.Conn != "baaytonadb" || a.Need.Need != "host_key" || a.Need.Host != "65.109.212.14" || a.Need.Fingerprint == "" || a.Need.KeyType != "ED25519" {
		t.Fatalf("phone ask %+v %+v", a, a.Need)
	}
	if !strings.Contains(a.Title, "BaaytonaDB") || d.Need == nil || d.Need.Fingerprint != a.Need.Fingerprint {
		t.Fatalf("asks %+v / %+v", a, d)
	}
	if f := e.call(t, e.phone, webproto.OpDBTrustHost, `{"conn":"baaytonadb","fingerprint":"`+a.Need.Fingerprint+`"}`); webTestErr(f) != "" {
		t.Fatalf("db.trust_host: %v", f)
	}
	r := aiTunWait(t, ch)
	if r.err != nil || !strings.Contains(r.out, "users") {
		t.Fatalf("after trust in the page: out %q, err %v", r.out, r.err)
	}
	e.ui(func(m *Model) {
		if m.confirmOpen || m.databases.hostAsk != nil {
			t.Error("the laptop's trust card stayed open")
		}
	})
	data, err := os.ReadFile(filepath.Join(e.home, ".ssh", "known_hosts"))
	if err != nil || !strings.Contains(string(data), "65.109.212.14 ssh-ed25519 ") {
		t.Fatalf("known_hosts = %q, %v", data, err)
	}
	if _, ans := e.drain(t, e.desk); len(ans) != 1 || ans[0].ID != d.ID {
		t.Fatalf("the desktop heard %+v, want its ask %s answered", ans, d.ID)
	}
	e.viewerAsked(t)
}

// Cancel in the page ends the tool with a short error, as the laptop's
// Esc does: nothing is trusted, the laptop's card closes, the other
// page's ask ends.
func TestWebAIDBTrustCardCancelInPage(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-92"
	defer srv.Close()
	aiTunForward(t, srv)
	e := newAIWebEnv(t, srv, aiTunBaaytona(), false)
	dbStoreSet(t, "db.baaytonadb.ssh", "tunnel-pw-92")

	ch := e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb"})
	d := e.askOf(t, e.desk)
	_ = e.askOf(t, e.phone)
	if f := e.call(t, e.desk, webproto.OpCardsAnswer, `{"id":"`+d.ID+`","choice":"cancel"}`); webTestErr(f) != "" {
		t.Fatalf("cards.answer: %v", f)
	}
	r := aiTunWait(t, ch)
	if r.err == nil || !strings.Contains(r.err.Error(), "not trusted") || len(r.err.Error()) > 200 {
		t.Fatalf("cancel: out %q, err %v", r.out, r.err)
	}
	e.ui(func(m *Model) {
		if m.confirmOpen {
			t.Error("the laptop's trust card stayed open")
		}
	})
	if _, ans := e.drain(t, e.phone); len(ans) != 1 {
		t.Fatalf("the phone heard %+v", ans)
	}
	if srv.AuthAttempts() != 0 || e.logins.Load() != 0 {
		t.Fatal("a login was sent after cancel")
	}
}

// The laptop answers first: Trust and save on its card answers the tool
// and ends both pages' asks (answered by the laptop).
func TestWebAIDBLaptopAnswerClosesThePage(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-93"
	defer srv.Close()
	aiTunForward(t, srv)
	e := newAIWebEnv(t, srv, aiTunBaaytona(), false)
	dbStoreSet(t, "db.baaytonadb.ssh", "tunnel-pw-93")

	ch := e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb", "schema": "public"})
	p, d := e.askOf(t, e.phone), e.askOf(t, e.desk)
	e.ui(func(m *Model) {
		m.confirmOpen = false
		_ = m.handleConfirmSelect("trust")
		m.webAfterUpdate(nil)
	})
	if r := aiTunWait(t, ch); r.err != nil || !strings.Contains(r.out, "users") {
		t.Fatalf("out %q, err %v", r.out, r.err)
	}
	for c, id := range map[*webConn]string{e.phone: p.ID, e.desk: d.ID} {
		if _, ans := e.drain(t, c); len(ans) != 1 || ans[0].ID != id || ans[0].By != "laptop" {
			t.Errorf("%s heard %+v", c.device, ans)
		}
	}
	// The page's late Trust and save is refused: the question is gone.
	if f := e.call(t, e.phone, webproto.OpDBTrustHost, `{"conn":"baaytonadb","fingerprint":"`+p.Need.Fingerprint+`"}`); webTestErr(f) != "conflict" {
		t.Fatalf("a late db.trust_host: %v", f)
	}
}

// An SSH password the tunnel needs is typed in the page: masked, the
// value goes to the secret store and the waiting tool only (no frame
// any page or the viewer got, no audit line, not the tool's answer),
// and the laptop's prompt closes.
func TestWebAIDBSSHPasswordInPage(t *testing.T) {
	const pw = "tunnel-pw-94-secret"
	srv := dbtest.NewSSHServer()
	srv.Password = pw
	defer srv.Close()
	aiTunForward(t, srv)
	e := newAIWebEnv(t, srv, aiTunBaaytona(), true)

	ch := e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb", "schema": "public"})
	e.waitFor(t, "the laptop's SSH password prompt", func(m *Model) bool { return m.promptOpen && m.promptKind == promptKindDBSSHSecret })
	a := e.askOf(t, e.phone)
	if a.Conn != "baaytonadb" || a.Need.Need != "ssh_password" || a.Need.Wrong {
		t.Fatalf("ask %+v %+v", a, a.Need)
	}
	_ = e.askOf(t, e.desk)
	if f := e.call(t, e.phone, webproto.OpDBSecret, `{"conn":"baaytonadb","kind":"ssh_password","value":"`+pw+`"}`); webTestErr(f) != "" {
		t.Fatalf("db.secret: %v", f)
	}
	r := aiTunWait(t, ch)
	if r.err != nil || !strings.Contains(r.out, "users") {
		t.Fatalf("out %q, err %v", r.out, r.err)
	}
	e.ui(func(m *Model) {
		if m.promptOpen || m.databases.secretAsk != nil {
			t.Error("the laptop's prompt stayed open")
		}
	})
	if v, ok, _ := databaseSecretStore().Get("db.baaytonadb.ssh"); !ok || v != pw {
		t.Fatalf("store has %q, %v", v, ok)
	}
	if _, ans := e.drain(t, e.desk); len(ans) != 1 {
		t.Fatalf("the desktop heard %+v", ans)
	}
	e.noSecret(t, pw, r.out)
	e.viewerAsked(t)
}

// The database refused the login (no password, or a wrong one): the
// page asks for the database's password; typed there it reaches the
// tool, which logs in with it, and nowhere else.
func TestWebAIDBPasswordInPage(t *testing.T) {
	const pw = "db-pw-95-secret"
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-95"
	defer srv.Close()
	aiTunForward(t, srv)
	e := newAIWebEnv(t, srv, aiTunBaaytona(), true)
	dbStoreSet(t, "db.baaytonadb.ssh", "tunnel-pw-95")
	aiDBRefuseBut(t, pw, nil)

	ch := e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb", "schema": "public"})
	e.waitFor(t, "the laptop's password prompt", func(m *Model) bool { return m.promptOpen && m.promptKind == promptKindDBPassword })
	d := e.askOf(t, e.desk)
	if d.Conn != "baaytonadb" || d.Need.Need != "password" || !strings.Contains(d.Body, "needs a password") {
		t.Fatalf("ask %+v %+v", d, d.Need)
	}
	_ = e.askOf(t, e.phone)
	if f := e.call(t, e.desk, webproto.OpDBSecret, `{"conn":"baaytonadb","kind":"password","value":"`+pw+`"}`); webTestErr(f) != "" {
		t.Fatalf("db.secret: %v", f)
	}
	r := aiTunWait(t, ch)
	if r.err != nil || !strings.Contains(r.out, "users") {
		t.Fatalf("out %q, err %v", r.out, r.err)
	}
	if v, ok, _ := databaseSecretStore().Get("db.baaytonadb"); !ok || v != pw {
		t.Fatalf("store has %q, %v", v, ok)
	}
	e.ui(func(m *Model) {
		if m.promptOpen || m.databases.pendingSecretAI != nil {
			t.Error("the laptop's prompt stayed open")
		}
	})
	if _, ans := e.drain(t, e.phone); len(ans) != 1 {
		t.Fatalf("the phone heard %+v", ans)
	}
	e.noSecret(t, pw, r.out)
	e.viewerAsked(t)
}

// With web_approve_on_laptop on, the pages are not asked: the laptop
// approves.
func TestWebAIDBAskStaysOnLaptopWhenApproveOnLaptop(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-96"
	defer srv.Close()
	aiTunForward(t, srv)
	e := newAIWebEnv(t, srv, aiTunBaaytona(), false)
	dbStoreSet(t, "db.baaytonadb.ssh", "tunnel-pw-96")
	e.ui(func(m *Model) { m.web.approveOnLaptop = true })
	ch := e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb"})
	e.waitFor(t, "the laptop's trust card", func(m *Model) bool { return m.confirmOpen && m.confirmKind == confirmKindDBTrustHost })
	e.ui(func(m *Model) { m.webAfterUpdate(nil) })
	for _, c := range []*webConn{e.phone, e.desk} {
		if asks, _ := e.drain(t, c); len(asks) != 0 {
			t.Errorf("%s was asked: %+v", c.device, asks)
		}
	}
	e.ui(func(m *Model) {
		m.confirmOpen = false
		_ = m.handleConfirmSelect("cancel")
	})
	_ = aiTunWait(t, ch)
}

// aiDBRefuseBut makes every login fail as PostgreSQL does on a wrong
// password (SQLSTATE 28P01) unless it carries pw; a login whose context
// already ended fails too. tried, when set, gets each password tried.
func aiDBRefuseBut(t *testing.T, pw string, tried *[]string) {
	t.Helper()
	var mu sync.Mutex
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(ctx context.Context, _ dbclient.Conn, got string) (dbclient.Session, error) {
		mu.Lock()
		if tried != nil {
			*tried = append(*tried, got)
		}
		mu.Unlock()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if got != pw {
			return nil, errors.New("failed to connect to `user=app database=baaytona`: failed SASL auth: FATAL: password authentication failed for user \"app\" (SQLSTATE 28P01)")
		}
		return &aiDBCtxSession{aiDBSession: &aiDBSession{name: "ai-pw"}}, nil
	}
	t.Cleanup(func() { dbSQLSessionFactory = old })
}

// aiDBCtxSession is aiDBSession whose reads fail once their context
// ended (a statement past its timeout).
type aiDBCtxSession struct{ *aiDBSession }

func (s *aiDBCtxSession) Introspect(ctx context.Context, req dbclient.IntrospectionRequest) (dbclient.IntrospectionResult, error) {
	if err := ctx.Err(); err != nil {
		return dbclient.IntrospectionResult{}, err
	}
	return s.aiDBSession.Introspect(ctx, req)
}

func (s *aiDBCtxSession) ReadOnlySession() (dbclient.Session, error) { return s, nil }

// The database's password prompt on the laptop (no web page): the
// login was refused, the view's masked prompt opens, the value typed
// there reaches the tool (it logs in once more with it) and the secret
// store, never the tool's answer. The wait for the answer is not
// counted in the statement timeout: answered after it, the tool still
// answers.
func TestAIDBPasswordPromptOnLaptop(t *testing.T) {
	const pw = "db-pw-97-secret"
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-97"
	defer srv.Close()
	aiTunForward(t, srv)
	e := newAITunEnv(t, srv, aiTunBaaytona(), true)
	dbStoreSet(t, "db.baaytonadb.ssh", "tunnel-pw-97")
	writeUserConfig(t, `{"db_timeout":1}`)
	var tried []string
	aiDBRefuseBut(t, pw, &tried)

	ch := e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb", "schema": "public"})
	e.waitFor(t, "the password prompt", func(m *Model) bool { return m.promptOpen && m.promptKind == promptKindDBPassword })
	time.Sleep(1500 * time.Millisecond) // past the 1 s statement timeout
	e.ui(func(m *Model) {
		m.promptOpen = false
		_ = m.onDBPrompt(promptKindDBPassword, pw)
	})
	r := aiTunWait(t, ch)
	if r.err != nil || !strings.Contains(r.out, "users") {
		t.Fatalf("answered after the statement timeout: out %q, err %v (tried %d logins)", r.out, r.err, len(tried))
	}
	if len(tried) != 2 || tried[1] != pw {
		t.Fatalf("%d logins", len(tried))
	}
	if v, ok, _ := databaseSecretStore().Get("db.baaytonadb"); !ok || v != pw {
		t.Fatalf("store has %q, %v", v, ok)
	}
	if strings.Contains(r.out, pw) {
		t.Fatalf("the password is in the answer: %q", r.out)
	}
}

// Esc on the laptop's password prompt ends the tool with a short error.
func TestAIDBPasswordPromptCancel(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-98"
	defer srv.Close()
	aiTunForward(t, srv)
	e := newAITunEnv(t, srv, aiTunBaaytona(), true)
	dbStoreSet(t, "db.baaytonadb.ssh", "tunnel-pw-98")
	aiDBRefuseBut(t, "never-typed", nil)
	ch := e.runBg(t, "db_schema", map[string]any{"conn": "baaytonadb"})
	e.waitFor(t, "the password prompt", func(m *Model) bool { return m.promptOpen && m.promptKind == promptKindDBPassword })
	e.ui(func(m *Model) {
		m.promptOpen = false
		m.databaseAIAskClosed()
	})
	r := aiTunWait(t, ch)
	if r.err == nil || !strings.Contains(r.err.Error(), "no database password") || len(r.err.Error()) > 200 {
		t.Fatalf("out %q, err %v", r.out, r.err)
	}
}
