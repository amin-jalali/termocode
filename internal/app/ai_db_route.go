package app

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
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

// dbAIAsk opens a question on Update (open returns its answer channel)
// and waits for the answer; when ctx ends first, drop closes it.
func dbAIAsk[T any](ctx context.Context, call aiIDECaller, open func(m *Model) (chan T, error), drop func(m *Model, ch chan T)) (T, error) {
	var zero T
	var ch chan T
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		if m.confirmOpen || m.promptOpen {
			return "", nil, errDBAIAskBusy
		}
		if m.databases == nil {
			m.databases = newDBState()
		}
		var err error
		ch, err = open(m)
		return "", nil, err
	}); err != nil {
		return zero, err
	}
	select {
	case v := <-ch:
		return v, nil
	case <-ctx.Done():
		dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = call(dctx, func(m *Model) (string, tea.Cmd, error) {
			if m.databases != nil {
				drop(m, ch)
			}
			return "", nil, nil
		})
		return zero, ctx.Err()
	}
}

// dbAIAskTrust shows the view's trust card for e and waits for yes.
func dbAIAskTrust(ctx context.Context, call aiIDECaller, c dbclient.Conn, e *dbclient.UnknownHostKeyError) error {
	ok, err := dbAIAsk(ctx, call, func(m *Model) (chan bool, error) {
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
		return errors.New("the SSH host " + webDBHostName(e.Host) + " was not trusted (the user cancelled); the tunnel was not opened")
	}
	return nil
}

// dbAIAskSSHSecret shows the view's SSH password / passphrase prompt and
// waits for the typed value (kept in the secret store, as the view does).
func dbAIAskSSHSecret(ctx context.Context, call aiIDECaller, c dbclient.Conn, e *dbclient.SSHSecretError) (string, error) {
	v, err := dbAIAsk(ctx, call, func(m *Model) (chan string, error) {
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
	v, err := dbAIAsk(ctx, call, func(m *Model) (chan string, error) {
		ch := make(chan string, 1)
		title := "Database password · " + sanitizeDBText(dbConnName(c))
		if wrong {
			title += " · not accepted, try again"
		}
		m.databases.pendingSecretAI = &dbAIPasswordAsk{conn: c.ID, name: name, done: ch}
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
