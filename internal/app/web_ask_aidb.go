package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The AI's database questions in the page (owner 2026-11-08: a paired
// control device is the owner, with the laptop's trust; the page asks
// wherever the TUI asks, and nothing needs the laptop). When a db_* tool
// waits on the user (ai_db_route.go) for an unknown SSH host key (the
// trust card), an SSH password or key passphrase, or the database's
// password (masked prompts), every controller page that approves its
// own actions gets the same question: an ask ev with the connection and
// its DBNeed, through the one path of web_ask.go. The page shows the
// Databases view's own card (ui/DBAsk.tsx) and answers with db.trust_host
// (exactly the key shown) or db.secret (the value goes to the secret
// store and to the waiting tool only: never to the model, an answer, an
// audit line or another page), or cancels with cards.answer. The laptop
// still asks; the first answer on any side wins and closes the others.
// A viewer gets nothing. The model never answers: only a page's request
// or the laptop's keys reach these answers (web_ai_never_test.go).

func init() {
	webAfterUpdates = append(webAfterUpdates, (*Model).webAIDBAskPump)
}

// webAIDBAsk is the db_* tool question the pages are asked now.
type webAIDBAsk struct {
	key   any // the TUI's question: *dbHostAsk, *dbSecretAsk or *dbAIPasswordAsk
	host  *dbclient.UnknownHostKeyError
	card  webproto.AskCard // its text, connection and need (no id)
	pages map[*webConn]bool
	asks  []*webAsk // the pages' questions
	by    string    // the device whose page answered it ("": the laptop)
}

// webAIDBAskNow is the question a db_* tool waits on now (nil: none).
func (m *Model) webAIDBAskNow() *webAIDBAsk {
	s := m.databases
	if s == nil {
		return nil
	}
	name := func(id string) string {
		if c, ok := s.connection(id); ok {
			return sanitizeDBText(dbConnName(c))
		}
		return sanitizeDBText(id)
	}
	ask := func(key any, conn, msg, action string, need webproto.DBNeed) *webAIDBAsk {
		return &webAIDBAsk{key: key, pages: map[*webConn]bool{}, card: webproto.AskCard{
			Title: "AI chat · " + name(conn), Body: msg, Action: action, Conn: conn, Need: &need}}
	}
	if a := s.hostAsk; a != nil && a.done != nil && a.err != nil {
		host, what := webDBHostName(a.err.Host), "SSH host"
		if a.err.Jump != "" {
			what = "SSH jump host"
		}
		w := ask(a, a.conn, "Unknown "+what+" "+host+". Check its key before you trust it.", "Trust and save",
			webproto.DBNeed{Need: "host_key", Host: host, KeyType: sanitizeDBText(dbKeyTypeName(a.err.KeyType)), Fingerprint: sanitizeDBText(a.err.Fingerprint), Jump: a.err.Jump != ""})
		w.host = a.err
		return w
	}
	if a := s.secretAsk; a != nil && a.done != nil {
		if a.kind == "passphrase" {
			msg := "The SSH key " + a.key + " needs its passphrase."
			if a.wrong {
				msg = "Wrong passphrase for the SSH key " + a.key + "."
			}
			return ask(a, a.conn, msg, "Save and connect", webproto.DBNeed{Need: "ssh_passphrase", Key: a.key, Wrong: a.wrong})
		}
		msg := "The SSH server needs a password."
		if a.wrong {
			msg = "The SSH server did not accept the password."
		}
		return ask(a, a.conn, msg, "Save and connect", webproto.DBNeed{Need: "ssh_password", Wrong: a.wrong})
	}
	if a := s.pendingSecretAI; a != nil {
		msg := "The database needs a password."
		if a.wrong {
			msg = "The database did not accept the password."
		}
		return ask(a, a.conn, msg, "Save and connect", webproto.DBNeed{Need: "password", Wrong: a.wrong})
	}
	return nil
}

// webAIDBAskPump runs after every Update: a question that ended (on the
// laptop, in a page, or the tool stopped) ends in every page; a new one,
// or a controller page that came since, is asked.
func (m *Model) webAIDBAskPump(tea.Msg) tea.Cmd {
	h := m.web
	now := m.webAIDBAskNow()
	cur := h.asks.aidb
	if cur != nil && (now == nil || now.key != cur.key) {
		m.webAIDBAskEnd(cur)
		h.asks.aidb, cur = nil, nil
	}
	if now == nil {
		return nil
	}
	if cur == nil {
		cur = now
		h.asks.aidb = cur
		if cur.host != nil {
			h.dbNet.askHostKey(cur.card.Conn, cur.host) // db.trust_host checks the page's key against it
		}
	}
	for c := range h.live {
		if cur.pages[c] || !h.askHere(c) || c.ctx == nil || c.ctx.Err() != nil || c.out == nil {
			continue
		}
		cur.pages[c] = true // asked once, even when it could not be
		a := &webAsk{op: "db.ai_ask", summary: cur.card.Conn + " " + cur.card.Need.Need, title: cur.card.Title, body: cur.card.Body, action: cur.card.Action,
			dbConn: cur.card.Conn, dbNeed: cur.card.Need}
		a.run = func(m *Model) tea.Cmd { return m.webAIDBAnswer(cur, a.device, cur.host != nil) }
		a.cancel = func(m *Model) tea.Cmd { return m.webAIDBAnswer(cur, a.device, false) }
		if _, code, msg := m.webAskHereAdd(c, a, true); code != "" {
			recordError("[web] db.ai_ask: " + msg)
			continue
		}
		cur.asks = append(cur.asks, a)
	}
	return nil
}

// webAIDBAskEnd ends a's questions still open in the pages: each hears
// answered, by the page that answered or the laptop.
func (m *Model) webAIDBAskEnd(a *webAIDBAsk) {
	h := m.web
	by := a.by
	if by == "" {
		by = "laptop"
	}
	for _, q := range a.asks {
		if h.asks.here[q.id] != q {
			continue // answered in its page already
		}
		delete(h.asks.here, q.id)
		if data, err := webData(webproto.Answered{ID: q.id, By: by}); err == nil {
			q.asker.send(&webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: data})
		}
	}
	if a.host != nil {
		_ = h.dbNet.takeHostKey(a.card.Conn, a.host.Fingerprint)
	}
}

// webAIDBAnswer is a page's answer to the question with cards.answer:
// yes on a host key trusts exactly that key (as db.trust_host does);
// anything else (Cancel, or yes with no value typed) is a cancel, as the
// laptop's Esc is.
func (m *Model) webAIDBAnswer(a *webAIDBAsk, device string, trust bool) tea.Cmd {
	if m.web == nil || m.web.asks.aidb != a {
		return nil // answered already
	}
	if trust {
		if err := dbclient.TrustHostKey(a.host.KnownHosts, a.host.Host, a.host.Key); err != nil {
			recordError("[web] db.ai_ask: " + strings.TrimSpace(err.Error()))
			trust = false
		}
	}
	m.webAIDBResolve(a, device, trust, "")
	if trust {
		return m.pushToastDetail(toast.Info, "Host key saved", sanitizeDBText(a.host.Fingerprint))
	}
	return nil
}

// webAIDBGiven is db.trust_host or db.secret from a page while a db_*
// tool waits on that question: the tool gets it (a trusted key, or the
// typed value) and the laptop's card or prompt closes. need is the
// db.secret kind or "host_key"; fp the trusted key's fingerprint.
func (m *Model) webAIDBGiven(device, conn, need, fp, value string) bool {
	a := m.web.asks.aidb
	if a == nil || a.card.Conn != conn || a.card.Need.Need != need {
		return false
	}
	if need == "host_key" && (a.host == nil || a.host.Fingerprint != fp) {
		return false
	}
	m.webAIDBResolve(a, device, true, value)
	return true
}

// webAIDBResolve answers the TUI's question a mirrors (ok: trusted, or
// value typed) and closes its card or prompt on the laptop.
func (m *Model) webAIDBResolve(a *webAIDBAsk, device string, ok bool, value string) {
	a.by = device
	s := m.databases
	if s == nil {
		return
	}
	switch q := a.key.(type) {
	case *dbHostAsk:
		if s.hostAsk == q {
			s.hostAsk = nil
			if m.confirmOpen && m.confirmKind == confirmKindDBTrustHost {
				m.confirmOpen = false
			}
			q.done <- ok
		}
	case *dbSecretAsk:
		if s.secretAsk == q {
			s.secretAsk = nil
			if m.promptOpen && m.promptKind == promptKindDBSSHSecret {
				m.promptOpen = false
			}
			q.done <- value
		}
	case *dbAIPasswordAsk:
		if s.pendingSecretAI == q {
			s.pendingSecretAI = nil
			if m.promptOpen && m.promptKind == promptKindDBPassword {
				m.promptOpen = false
			}
			q.done <- value
		}
	}
}
