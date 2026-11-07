package app

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Keep-alive and auto-reconnect (DBX-21). A connection with
// options.keepAliveSeconds pings its main session that often while it is
// connected. With options.autoReconnect a failed ping (or a failed
// reconnect) drops the session and tries again after 1 s, 2 s, 4 s … at
// most 30 s, up to dbReconnectAttempts times; the tree row says so. The
// user's own Disconnect stops all of it.

const dbReconnectAttempts = 8

// dbKeepAlive is the keep-alive of one connection. gen drops ticks and
// answers of an older start.
type dbKeepAlive struct {
	gen          uint64
	fails        int
	reconnecting bool
}

type (
	dbKeepAliveMsg struct {
		conn string
		gen  uint64
	}
	dbPingDoneMsg struct {
		conn string
		gen  uint64
		err  error
	}
	dbReconnectMsg struct {
		conn string
		gen  uint64
	}
)

// dbKeepAliveEvery is the ping interval of c (0: none). An SQLite file
// has nothing to keep alive.
func dbKeepAliveEvery(c dbclient.Conn) time.Duration {
	o := c.Options
	if o == nil || c.Driver == "sqlite" {
		return 0
	}
	if o.KeepAliveSeconds > 0 {
		return time.Duration(max(o.KeepAliveSeconds, 5)) * time.Second
	}
	if o.AutoReconnect {
		return 30 * time.Second
	}
	return 0
}

func dbAutoReconnect(c dbclient.Conn) bool { return c.Options != nil && c.Options.AutoReconnect }

func (s *dbState) keepAliveOf(id string) *dbKeepAlive {
	if s.keepAlive == nil {
		s.keepAlive = map[string]*dbKeepAlive{}
	}
	ka := s.keepAlive[id]
	if ka == nil {
		ka = &dbKeepAlive{}
		s.keepAlive[id] = ka
	}
	return ka
}

// dbKeepAliveStart starts (or restarts) the pings after a connect.
func (m *Model) dbKeepAliveStart(id string) tea.Cmd {
	s := m.databases
	if s == nil {
		return nil
	}
	c, ok := s.connection(id)
	every := dbKeepAliveEvery(c)
	if !ok || every == 0 {
		return nil
	}
	ka := s.keepAliveOf(id)
	ka.gen++
	ka.fails, ka.reconnecting = 0, false
	gen := ka.gen
	return tea.Tick(every, func(time.Time) tea.Msg { return dbKeepAliveMsg{conn: id, gen: gen} })
}

func (m *Model) onDBKeepAlive(msg dbKeepAliveMsg) tea.Cmd {
	s := m.databases
	if s == nil {
		return nil
	}
	ka := s.keepAlive[msg.conn]
	sess := s.sessions[msg.conn]
	c, ok := s.connection(msg.conn)
	if ka == nil || ka.gen != msg.gen || sess == nil || !ok {
		return nil
	}
	every := dbKeepAliveEvery(c)
	if every == 0 {
		return nil
	}
	if cs := s.console; cs != nil && (cs.running != nil && dbTxConn(cs.running.conn) == msg.conn || cs.job != nil && cs.job.conn == msg.conn) {
		// Busy: the query itself keeps the session alive.
		return tea.Tick(every, func(time.Time) tea.Msg { return msg })
	}
	gen := msg.gen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return dbPingDoneMsg{conn: msg.conn, gen: gen, err: sess.Ping(ctx)}
	}
}

func (m *Model) onDBPingDone(msg dbPingDoneMsg) tea.Cmd {
	s := m.databases
	if s == nil {
		return nil
	}
	ka := s.keepAlive[msg.conn]
	c, ok := s.connection(msg.conn)
	if ka == nil || ka.gen != msg.gen || !ok || s.sessions[msg.conn] == nil {
		return nil
	}
	if msg.err == nil {
		ka.fails = 0
		every := dbKeepAliveEvery(c)
		if every == 0 {
			return nil
		}
		next := dbKeepAliveMsg{conn: msg.conn, gen: msg.gen}
		return tea.Tick(every, func(time.Time) tea.Msg { return next })
	}
	name := sanitizeDBText(dbConnName(c))
	m.databaseDisconnectNow(msg.conn)
	if !dbAutoReconnect(c) {
		n := s.nodes[msg.conn]
		n.problem = "connection lost: " + dbErrorText(msg.err)
		s.nodes[msg.conn] = n
		s.touch()
		return m.pushToastDetail(toast.Warn, "Connection lost: "+name, dbErrorText(msg.err)+" · Enter connects again.")
	}
	ka = s.keepAliveOf(msg.conn)
	ka.reconnecting = true
	return tea.Batch(m.pushToastDetail(toast.Warn, "Connection lost: "+name, "Reconnecting…"), m.dbReconnectLater(msg.conn))
}

// dbReconnectLater plans the next reconnect attempt with backoff.
func (m *Model) dbReconnectLater(id string) tea.Cmd {
	s := m.databases
	ka := s.keepAliveOf(id)
	wait := dbclient.ReconnectDelay(ka.fails)
	ka.fails++
	ka.gen++
	gen := ka.gen
	n := s.nodes[id]
	n.problem = fmt.Sprintf("connection lost · reconnecting in %d s (try %d of %d)", int(wait.Seconds()), ka.fails, dbReconnectAttempts)
	s.nodes[id] = n
	s.touch()
	return tea.Tick(wait, func(time.Time) tea.Msg { return dbReconnectMsg{conn: id, gen: gen} })
}

func (m *Model) onDBReconnect(msg dbReconnectMsg) tea.Cmd {
	s := m.databases
	if s == nil {
		return nil
	}
	ka := s.keepAlive[msg.conn]
	if ka == nil || ka.gen != msg.gen || !ka.reconnecting || s.sessions[msg.conn] != nil {
		return nil
	}
	n := s.nodes[msg.conn]
	n.problem = ""
	s.nodes[msg.conn] = n
	return m.databaseConnect(msg.conn)
}

// dbReconnectFailed runs when a connect fails: during auto-reconnect it
// plans the next try, or gives up after the last one. ok=false: no
// reconnect was running.
func (m *Model) dbReconnectFailed(id string) (tea.Cmd, bool) {
	s := m.databases
	if s == nil {
		return nil, false
	}
	ka := s.keepAlive[id]
	if ka == nil || !ka.reconnecting {
		return nil, false
	}
	if ka.fails >= dbReconnectAttempts {
		delete(s.keepAlive, id)
		c, _ := s.connection(id)
		return m.pushToastDetail(toast.Errr, "Stopped reconnecting: "+sanitizeDBText(dbConnName(c)), fmt.Sprintf("%d tries failed · Enter connects again.", dbReconnectAttempts)), true
	}
	return m.dbReconnectLater(id), true
}

// dbConnTint is the theme colour of a connection colour (DBX-21).
func dbConnTint(color string) (theme.Color256, bool) {
	switch color {
	case "red":
		return theme.DiagError, true
	case "orange":
		return theme.AccentAmber, true
	case "yellow":
		return theme.DiagWarning, true
	case "green":
		return theme.AccentGreen, true
	case "blue":
		return theme.AccentBlue, true
	case "purple":
		return theme.AccentLavender, true
	case "gray":
		return theme.TextMuted, true
	}
	return 0, false
}
