package app

import (
	"context"
	"errors"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The AI's way to a database behind an SSH tunnel (owner 2026-11-09:
// never "connect it in the Databases view first"). When the view has no
// tunnel open for the connection, a db_* tool opens one the way the web
// page does: the same webDBNet code and cache (the web hub's while it is
// on), the saved SSH settings and secrets, the known_hosts check, and a
// container on an ssh:// context found by its name. What the view would
// ask the user, the tool asks with the view's own card or prompt and
// waits: an unknown host key (the trust card), an SSH password or key
// passphrase, the database's password (masked prompts). The model never
// sees or gives a secret; a cancel ends the tool with a short error.
// While it waits, the chat says what for (dbAIWaitChat: the page's tool
// line and the TUI's activity line); calls that need the same answer at
// once share one question; nobody answering for dbAIAskWait ends them.

// dbThenAI marks a trust card or SSH secret prompt a db_* tool waits on.
const dbThenAI = "ai"

// dbAIMaxAsks caps the questions one open asks (host, jump host, secret,
// a wrong secret again).
const dbAIMaxAsks = 5

// errDBAIAskBusy is the answer when another question is on the screen.
var errDBAIAskBusy = errors.New("another question is open on the screen; once it is answered, call this tool again")

// dbAIPasswordAsk is the AI's open database password prompt.
type dbAIPasswordAsk struct {
	conn, name string // the connection; the secret store name it is kept as
	wrong      bool   // the one tried was not accepted
	done       chan string
}

// dbSharedNet is the tunnels the AI opens: the web hub's while it is on
// (the page and the AI share them), else the model's own.
func (m *Model) dbSharedNet() *webDBNet {
	if m.web != nil {
		return &m.web.dbNet
	}
	if m.aiDBNet == nil {
		m.aiDBNet = &webDBNet{}
	}
	return m.aiDBNet
}

// dbAINeedsTunnel: c reaches its database through an SSH tunnel.
func dbAINeedsTunnel(c dbclient.Conn) bool {
	return c.SSH != nil && c.Driver != "sqlite" && c.Driver != "duckdb"
}

// dbAIReach opens (or reuses) c's tunnel on net and returns its dialer;
// an unknown host key or a missing SSH secret is asked of the user, then
// the open is tried again.
func dbAIReach(ctx context.Context, root string, call aiIDECaller, net *webDBNet, c dbclient.Conn, sec dbSSHSecrets) (dbclient.DialFunc, error) {
	for range dbAIMaxAsks {
		octx, cancel := context.WithTimeout(ctx, dbTestTimeout)
		dial, _, err := net.dial(octx, c, root, sec)
		cancel()
		if err == nil {
			return dial, nil
		}
		if ctx.Err() != nil {
			return nil, err
		}
		var unknown *dbclient.UnknownHostKeyError
		var secret *dbclient.SSHSecretError
		switch {
		case errors.As(err, &unknown):
			if err := dbAIAskTrust(ctx, call, c, unknown); err != nil {
				return nil, err
			}
		case errors.As(err, &secret):
			v, err := dbAIAskSSHSecret(ctx, call, c, secret)
			if err != nil {
				return nil, err
			}
			if secret.Kind == "passphrase" {
				sec.passphrase = v
			} else {
				sec.password = v
			}
		default:
			return nil, err
		}
	}
	return nil, errors.New("the SSH tunnel of " + c.ID + " did not open")
}

// dbAIAskWait is how long a db_* tool waits for the user's answer (owner
// 2026-11-09: a chat sat 4+ minutes on a prompt nobody saw). Then the
// tool ends with a short error and the question closes everywhere.
const dbAIAskWait = 5 * time.Minute

// dbAIWait is the question db_* tools wait on: the calls that need the
// same answer at once (the model fired several) share it, so the user
// is asked once and every waiting call gets the answer.
type dbAIWait struct {
	key  string        // the connection and what it needs
	what string        // "the database password of BaaytonaDB" (the timeout's error)
	n    int           // the calls waiting on it (Update only)
	quit chan struct{} // closed (on Update) when the last call left before an answer
	done chan struct{} // closed once val or err is set
	val  any
	err  error
}

// ended: the question was answered, timed out or left.
func (w *dbAIWait) ended() bool {
	select {
	case <-w.done:
		return true
	case <-w.quit:
		return true
	default:
		return false
	}
}

// dbAIAsk opens a question on Update (open returns its answer channel)
// and waits for the answer. A call that needs the same (key) while it is
// open waits on it too, with no second question. Nobody answering for
// dbAIAskWait closes it (drop) and ends the calls with a short error;
// when the last waiting call's ctx ends first, drop closes it too.
func dbAIAsk[T any](ctx context.Context, call aiIDECaller, key, what string, open func(m *Model) (chan T, error), drop func(m *Model, ch chan T)) (T, error) {
	var zero T
	var w *dbAIWait
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		if m.databases == nil {
			m.databases = newDBState()
		}
		if cur := m.databases.aiWait; cur != nil && cur.key == key && !cur.ended() {
			cur.n++
			w = cur
			return "", nil, nil
		}
		if m.confirmOpen || m.promptOpen {
			return "", nil, errDBAIAskBusy
		}
		ch, err := open(m)
		if err != nil {
			return "", nil, err
		}
		w = &dbAIWait{key: key, what: what, n: 1, quit: make(chan struct{}), done: make(chan struct{})}
		m.databases.aiWait = w
		crash.Go("db.ai-ask", func() { dbAIAnswerOf(call, w, ch, drop) })
		return "", nil, nil
	}); err != nil {
		return zero, err
	}
	select {
	case <-w.done:
		if w.err != nil {
			return zero, w.err
		}
		return w.val.(T), nil
	case <-ctx.Done():
		dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = call(dctx, func(m *Model) (string, tea.Cmd, error) {
			if w.n--; w.n == 0 && !w.ended() {
				close(w.quit) // dbAIAnswerOf drops the question
			}
			return "", nil, nil
		})
		return zero, ctx.Err()
	}
}

// dbAIAnswerOf waits for w's answer on ch and hands it to every waiting
// call: the value, a timeout after dbAIAskWait (the question closes on
// the laptop, so in every page too) or the calls having left.
func dbAIAnswerOf[T any](call aiIDECaller, w *dbAIWait, ch chan T, drop func(m *Model, ch chan T)) {
	defer close(w.done)
	closeIt := func() {
		dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = call(dctx, func(m *Model) (string, tea.Cmd, error) {
			if m.databases != nil {
				drop(m, ch)
			}
			return "", nil, nil
		})
	}
	select {
	case v := <-ch:
		w.val = v
		return
	case <-w.quit:
		closeIt()
		w.err = context.Canceled
		return
	case <-clock.After(dbAIAskWait):
	}
	closeIt()
	select {
	case v := <-ch: // answered just before it closed
		w.val = v
	default:
		w.err = dbAINoAnswer("Waited " + strconv.Itoa(int(dbAIAskWait/time.Minute)) + " min for " + w.what +
			"; nobody answered. Do not retry this call at once: tell the user what it needs and wait for their reply.")
	}
}

// dbAINoAnswer is the error of a question nobody answered in time. It is
// termocode's own text (no server's): the tools pass it on as it is, so
// "password of BaaytonaDB" is not taken for a secret.
type dbAINoAnswer string

func (e dbAINoAnswer) Error() string { return string(e) }

// dbAIAskTrust shows the view's trust card for e and waits for yes.
func dbAIAskTrust(ctx context.Context, call aiIDECaller, c dbclient.Conn, e *dbclient.UnknownHostKeyError) error {
	host := webDBHostName(e.Host)
	what := "the user to trust the SSH host " + host + " of " + sanitizeDBText(dbConnName(c))
	ok, err := dbAIAsk(ctx, call, c.ID+"\x00host_key\x00"+e.Fingerprint, what, func(m *Model) (chan bool, error) {
		ch := make(chan bool, 1)
		m.databaseTunnelError(e, c.ID, dbThenAI)
		m.databases.hostAsk.done = ch
		return ch, nil
	}, func(m *Model, ch chan bool) {
		if a := m.databases.hostAsk; a != nil && a.done == ch {
			m.databases.hostAsk = nil
			if m.confirmOpen && m.confirmKind == confirmKindDBTrustHost {
				m.confirmOpen = false
			}
		}
	})
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("the SSH host " + host + " was not trusted (the user cancelled); the tunnel was not opened")
	}
	return nil
}

// dbAIAskSSHSecret shows the view's SSH password / passphrase prompt and
// waits for the typed value (kept in the secret store, as the view does).
func dbAIAskSSHSecret(ctx context.Context, call aiIDECaller, c dbclient.Conn, e *dbclient.SSHSecretError) (string, error) {
	what := "the SSH " + dbAISecretWord(e.Kind) + " of " + sanitizeDBText(dbConnName(c))
	v, err := dbAIAsk(ctx, call, c.ID+"\x00ssh_"+e.Kind, what, func(m *Model) (chan string, error) {
		ch := make(chan string, 1)
		m.databaseTunnelError(e, c.ID, dbThenAI)
		m.databases.secretAsk.done = ch
		return ch, nil
	}, func(m *Model, ch chan string) {
		if a := m.databases.secretAsk; a != nil && a.done == ch {
			m.databases.secretAsk = nil
			if m.promptOpen && m.promptKind == promptKindDBSSHSecret {
				m.promptOpen = false
			}
		}
	})
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("no SSH " + dbAISecretWord(e.Kind) + " was typed (the user cancelled); the tunnel was not opened")
	}
	return v, nil
}

func dbAISecretWord(kind string) string {
	if kind == "passphrase" {
		return "key passphrase"
	}
	return "password"
}

// dbAIAskPassword shows the view's masked database password prompt for
// c and waits for the typed value (kept in the secret store as name).
func dbAIAskPassword(ctx context.Context, call aiIDECaller, c dbclient.Conn, name string, wrong bool) (string, error) {
	what := "the database password of " + sanitizeDBText(dbConnName(c))
	v, err := dbAIAsk(ctx, call, c.ID+"\x00password", what, func(m *Model) (chan string, error) {
		ch := make(chan string, 1)
		title := "Database password · " + sanitizeDBText(dbConnName(c))
		if wrong {
			title += " · not accepted, try again"
		}
		m.databases.pendingSecretAI = &dbAIPasswordAsk{conn: c.ID, name: name, wrong: wrong, done: ch}
		m.databaseAddPrompt(promptKindDBPassword, title, "Password for "+sanitizeDBText(dbAITarget(c))+" (kept in the secret store):", "", true)
		return ch, nil
	}, func(m *Model, ch chan string) {
		if a := m.databases.pendingSecretAI; a != nil && a.done == ch {
			m.databases.pendingSecretAI = nil
			if m.promptOpen && m.promptKind == promptKindDBPassword {
				m.promptOpen = false
			}
		}
	})
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", errors.New("no database password was typed (the user cancelled)")
	}
	return v, nil
}

// dbAILogin logs in to c with pw. A db_* tool opens its session before
// its statement timeout starts (the user may be asked for the password
// on the way, and that wait is not counted): each login it tries is
// bounded by that timeout on its own. The web page's opens (no call)
// are bounded by their caller.
func dbAILogin(ctx context.Context, c dbclient.Conn, snap *dbAISnapshot, pw string) (dbclient.Session, error) {
	if snap.call == nil {
		return dbSQLSessionFactory(ctx, c, pw)
	}
	lctx, cancel := context.WithTimeout(ctx, loadDBConsoleSettings().timeout())
	defer cancel()
	sess, err := dbSQLSessionFactory(lctx, c, pw)
	if err != nil && ctx.Err() == nil && errors.Is(lctx.Err(), context.DeadlineExceeded) && dbclient.ReachText(err) == "" {
		err = dbNoAnswer(c, c.SSH != nil) // the database said nothing in time
	}
	return sess, err
}

// databaseAIPasswordTyped answers the AI's database password prompt:
// the value is kept in the secret store (when it is open) and handed to
// the waiting tool, never to the model. ok=false: not the AI's prompt.
func (m *Model) databaseAIPasswordTyped(value string) (tea.Cmd, bool) {
	s := m.databases
	if s == nil || s.pendingSecretAI == nil {
		return nil, false
	}
	a := s.pendingSecretAI
	s.pendingSecretAI = nil
	var cmd tea.Cmd
	if value != "" {
		ai.RegisterSecret(value)
		if store := databaseSecretStore(); store == nil || store.Locked() {
			cmd = m.pushToastDetail(toast.Warn, "Password not saved", "The secret store is locked: it is used for this read only.")
		} else if err := store.Set(a.name, value); err != nil {
			cmd = m.pushToastDetail(toast.Warn, "Password not saved", sanitizeDBText(err.Error()))
		}
	}
	a.done <- value
	return cmd, true
}

// databaseAISSHSecretTyped answers a db_* tool's SSH secret prompt (the
// view's own handler does the rest). ok=false: not the AI's prompt.
func (m *Model) databaseAISSHSecretTyped(ask *dbSecretAsk, value string) (tea.Cmd, bool) {
	if ask.done == nil {
		return nil, false
	}
	var cmd tea.Cmd
	if value != "" {
		ai.RegisterSecret(value)
		if store := databaseSecretStore(); store == nil || store.Locked() {
			cmd = m.pushToastDetail(toast.Warn, "SSH secret not saved", "The secret store is locked: it is used for this tunnel only.")
		} else if err := store.Set(dbSSHSecretName(ask.conn, ask.kind), value); err != nil {
			cmd = m.pushToastDetail(toast.Warn, "SSH secret not saved", sanitizeDBText(err.Error()))
		}
	}
	ask.done <- value
	return cmd, true
}

// databaseAIAskClosed: Esc closed a card or prompt a db_* tool waits on;
// the tool hears a cancel.
func (m *Model) databaseAIAskClosed() {
	s := m.databases
	if s == nil {
		return
	}
	if a := s.hostAsk; a != nil && a.done != nil && !(m.confirmOpen && m.confirmKind == confirmKindDBTrustHost) {
		s.hostAsk = nil
		a.done <- false
	}
	if a := s.secretAsk; a != nil && a.done != nil && !(m.promptOpen && m.promptKind == promptKindDBSSHSecret) {
		s.secretAsk = nil
		a.done <- ""
	}
	if a := s.pendingSecretAI; a != nil && !(m.promptOpen && m.promptKind == promptKindDBPassword) {
		s.pendingSecretAI = nil
		a.done <- ""
	}
}
