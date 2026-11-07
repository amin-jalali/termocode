package app

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Redis admin (DBX-51): rename / copy a key (M / C on a key row, or the
// palette on the open key), and the server views: Show Server Info runs
// INFO, Show Sessions runs CLIENT LIST, Show Redis Slow Log runs SLOWLOG
// GET; INFO and SLOWLOG come back as readable grids. Rename never
// overwrites (RENAMENX), copy never replaces; both ask first.

const (
	promptKindDBRedisRename promptKindEnum = 1460 + iota
	promptKindDBRedisCopy
)

// dbRedisRenameCopyArgs is the command for the typed new name.
func dbRedisRenameCopyArgs(copyKey bool, key, value string) ([]string, error) {
	dst := strings.TrimSpace(value)
	if dst == "" {
		return nil, errors.New("type a key name")
	}
	if dst == key {
		return nil, errors.New("the new name is the same as the old one")
	}
	if copyKey {
		return dbclient.RedisCopy(key, dst, -1, false), nil
	}
	return dbclient.RedisRename(key, dst, false), nil
}

// databaseRedisTarget is the key of the value page, else the key row
// selected in the tree.
func (m *Model) databaseRedisTarget() (*dbRedisEdit, bool) {
	if m.databases != nil && m.databases.console != nil {
		if s := m.databases.console; s.shown != nil && s.shown.redis != nil && s.running == nil && isDBResultPath(m.editor.Path()) {
			v := s.shown.redis
			return &dbRedisEdit{conn: s.shown.conn, db: v.db, key: v.key, typ: v.info.Type, ttl: v.info.TTL, tab: s.curTab()}, true
		}
	}
	m.ensureDatabasesLoaded()
	if row, ok := m.databaseCurrentRow(); ok && row.kind == dbNodeRedisKey {
		n := m.databases.nodes[row.path]
		return &dbRedisEdit{conn: n.conn, db: n.rdb, key: n.key, typ: n.rtype, ttl: n.ttl}, true
	}
	return nil, false
}

// databaseRedisRenameCopyPrompt asks for the new key name.
func (m *Model) databaseRedisRenameCopyPrompt(edit *dbRedisEdit, copyKey bool) tea.Cmd {
	if m.databases.sessions[edit.conn] == nil {
		return m.databaseConnect(edit.conn)
	}
	m.dbConsole().redisEdit = edit
	name := sanitizeDBText(dbclient.RedisDisplay(edit.key))
	if copyKey {
		edit.what = "copy"
		return m.openHTTPPrompt(promptKindDBRedisCopy, "Copy key", "Copy "+name+" to the new key (an existing key is never replaced):", edit.key+":copy")
	}
	edit.what = "rename"
	return m.openHTTPPrompt(promptKindDBRedisRename, "Rename key", "New name of "+name+" (an existing key is never replaced):", edit.key)
}

// databaseRedisPaletteAdmin runs the Redis admin palette commands.
func (m *Model) databaseRedisPaletteAdmin(id string) tea.Cmd {
	if id == "redis-slowlog" {
		return m.databaseRedisServerView("SLOWLOG GET 128")
	}
	edit, ok := m.databaseRedisTarget()
	if !ok {
		return m.pushToastDetail(toast.Info, "Select a Redis key first", "Open a key, or select one in the Databases view.")
	}
	return m.databaseRedisRenameCopyPrompt(edit, id == "redis-copy-key")
}

// databaseRedisServerView runs a read-only server command in the console
// of the current Redis connection.
func (m *Model) databaseRedisServerView(line string) tea.Cmd {
	c, cmd, ok := m.databaseSessionConn()
	if !ok {
		return cmd
	}
	if c.Driver != "redis" {
		return m.pushToastDetail(toast.Info, "Not a Redis connection", "Select a Redis or Valkey connection first.")
	}
	return m.databaseRunInConsole(c.ID, line)
}

// databaseRedisAdminPrompt handles the rename / copy answers.
func (m *Model) databaseRedisAdminPrompt(kind promptKindEnum, value string) (tea.Cmd, bool) {
	if kind != promptKindDBRedisRename && kind != promptKindDBRedisCopy {
		return nil, false
	}
	s := m.dbConsole()
	e := s.redisEdit
	s.redisEdit = nil
	if e == nil {
		return nil, true
	}
	args, err := dbRedisRenameCopyArgs(kind == promptKindDBRedisCopy, e.key, value)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Key not changed", err.Error()), true
	}
	return m.databaseRedisAsk(&dbRedisWrite{conn: e.conn, db: e.db, key: e.key, args: args, admin: e.what, dst: args[2]}), true
}

// onDBRedisAdminDone reports a rename / copy: 0 means the new name was
// taken; on success the tree reloads and a renamed open key reopens.
func (m *Model) onDBRedisAdminDone(w *dbRedisWrite, reply any, hist tea.Cmd) tea.Cmd {
	dst := dbOneLine(dbclient.RedisDisplay(w.dst))
	if n, ok := reply.(int64); (ok && n == 0) || (reply == nil && w.admin == "new") {
		return tea.Batch(hist, m.pushToastDetail(toast.Warn, "A key named "+dst+" already exists", "Nothing changed. Pick another name."))
	}
	title := "Copied to " + dst
	if w.admin == "new" {
		title = "Made key " + dst
	}
	var reopen tea.Cmd
	if w.admin == "rename" {
		title = "Renamed to " + dst
		cs := m.dbConsole()
		if cs.shown != nil && cs.shown.redis != nil && cs.shown.redis.key == w.key && cs.shown.conn == w.conn {
			reopen = m.databaseRedisOpen(w.conn, w.db, w.dst, "", nil, cs.curTab())
		}
	}
	return tea.Batch(hist, m.pushToast(toast.Info, title), m.databaseRefresh(), reopen)
}
