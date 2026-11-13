package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// SQL completion inside the SQL strings of a Go, Python or JS/TS file
// (DBX-41). On each change (150 ms debounce) the buffer notifies
// termocode_db_embed; Go pushes the ranges of its SQL strings and, when
// the file has a connection, the same schema a .sql buffer gets. The
// buffer's omnifunc becomes _termocode_sql_embed_omni (dbConsoleLua):
// inside a range it runs the SQL omnifunc on that string, elsewhere the
// omnifunc the buffer had (the language server's).

// dbEmbedRange is one SQL string: 0-based rows, byte columns of its text
// (quotes left out, end exclusive). Q is the quote a quoted name must
// escape with a backslash ("" for raw and triple-quoted strings).
type dbEmbedRange struct {
	SR, SC, ER, EC int
	Q              string
}

// dbEmbedRanges are the SQL string ranges of src (lang from EmbedLang).
func dbEmbedRanges(src, lang string) []dbEmbedRange {
	out := []dbEmbedRange{}
	lineStart := []int{0}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			lineStart = append(lineStart, i+1)
		}
	}
	for _, e := range dbclient.FindEmbeddedSQL(src, lang) {
		if e.Line < 1 || e.Line > len(lineStart) {
			continue
		}
		open := lineStart[e.Line-1] + e.Col // the opening quote
		at, q := open+1, ""
		switch {
		case strings.HasPrefix(src[open:], `"""`) || strings.HasPrefix(src[open:], `'''`):
			at = open + 3
		case src[open] == '"' || src[open] == '\'':
			q = src[open : open+1]
		}
		if at+len(e.Text) > len(src) {
			continue
		}
		sr, sc := dbLineCol(src, at)
		er, ec := dbLineCol(src, at+len(e.Text))
		out = append(out, dbEmbedRange{SR: sr, SC: sc, ER: er, EC: ec, Q: q})
	}
	return out
}

// dbSQLAttachable reports a file a connection can be attached to: a .sql
// file, or a code file whose strings may hold SQL.
func dbSQLAttachable(file string) bool {
	return strings.EqualFold(lastExt(file), ".sql") || dbclient.EmbedLang(file) != ""
}

// databaseEmbedBuffer pushes the SQL string ranges of a code buffer and
// the schema of its connection.
func (m *Model) databaseEmbedBuffer(buf int) {
	if m.nvim == nil {
		return
	}
	file, lines, ok := m.httpBufText(buf)
	lang := dbclient.EmbedLang(file)
	if !ok || lang == "" {
		return
	}
	_ = m.nvim.ExecLuaArgs(`local b,r=...;if _G._termocode_sql then _G._termocode_sql.embed[b]=r end`, buf, dbEmbedRanges(strings.Join(lines, "\n"), lang))
	m.ensureDatabasesLoaded()
	if c, ok := m.databases.connection(m.dbConsole().consoleConn(file, lines)); ok && dbclient.DialectFor(c.Driver) != nil {
		m.databasePushSchema(buf, c)
	}
}

func init() {
	registerNotifyHandler("termocode_db_embed", func(m *Model, args []any) tea.Cmd {
		buf := notifyArgInt(args, 0)
		m.databaseEmbedBuffer(buf)
		return m.databaseBackgroundSchema(buf)
	})
}
