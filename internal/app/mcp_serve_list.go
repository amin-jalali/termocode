package app

import "github.com/amin-jalali/termocode/internal/mcp"

// The tool list follows the database connections (M1 MS-7): the SQL tools
// are listed when a SQL connection exists, the Redis tools when a Redis
// one does. When that set changes (the Databases view reloads, or a
// connection file is saved) every initialized client gets
// notifications/tools/list_changed (mcp.SocketServer.NotifyToolsChanged).
// The static list of `termocode mcp` without a UI and the docs keep every
// tool.

// mcpServeDBSet is which kinds of connections the workspace has.
type mcpServeDBSet struct{ sql, redis bool }

func mcpServeDBSetOf(root string) mcpServeDBSet {
	_, _, conns := dbLoadConfigs(root)
	var d mcpServeDBSet
	for _, c := range conns {
		switch c.Driver {
		case "redis":
			d.redis = true
		case "postgres", "mysql", "sqlite":
			d.sql = true
		}
	}
	return d
}

// lists reports whether a tool is listed with these connections.
func (d mcpServeDBSet) lists(name string) bool {
	switch name {
	case "db_connections":
		return d.sql || d.redis
	case "db_schema", "db_ddl", "db_query":
		return d.sql
	case "redis_keys", "redis_get", "redis_command":
		return d.redis
	}
	return true
}

// List is tools/list: the exposed tools, the database ones only where a
// connection of their kind exists.
func (s *mcpServeSource) List() []mcp.Tool {
	d := mcpServeDBSetOf(s.root)
	out := make([]mcp.Tool, 0, len(s.specs))
	for _, t := range s.specs {
		if d.lists(t.Name) {
			out = append(out, t)
		}
	}
	return out
}

// ToolsCanChange: the list follows the connections (mcp.ToolListChanger).
func (s *mcpServeSource) ToolsCanChange() bool { return true }

// toolsChanged reports whether the connection kinds changed since the
// last look, and remembers the new set.
func (s *mcpServeSource) toolsChanged() bool {
	d := mcpServeDBSetOf(s.root)
	s.dbMu.Lock()
	defer s.dbMu.Unlock()
	changed := d != s.dbSet
	s.dbSet = d
	return changed
}

// mcpServeToolsCheck tells the connected agents when the tool list
// changed. It runs on the UI goroutine after the connections may have
// changed; it never blocks (a full send queue drops the notification).
func (m *Model) mcpServeToolsCheck() {
	st := m.mcpServe
	if st == nil || st.src == nil || st.srv == nil {
		return
	}
	if st.src.toolsChanged() {
		st.srv.NotifyToolsChanged()
	}
}
