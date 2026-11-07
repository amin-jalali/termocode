package app

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// DBX-24: "fix this error", "explain this query" and "generate SQL from
// schema". Each one only opens the AI chat with a ready question; the AI
// works through the safe db_* tools it already has (db_schema and
// db_explain read, db_query asks before a write). Nothing here runs SQL.
// The SQL and the error text pass dbRedact before they reach the AI.

const promptKindDBAIGenerate promptKindEnum = 1290

func dbAIPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "db-ai-fix-error", Title: "Databases: AI Fix Query Error"},
		{ID: "db-ai-explain-query", Title: "Databases: AI Explain Query"},
		{ID: "db-ai-generate-sql", Title: "Databases: AI Generate SQL from Schema…"},
	}
}

func dbAIPaletteTitle(id string) string {
	for _, e := range dbAIPaletteItems() {
		if e.ID == id {
			return e.Title
		}
	}
	return ""
}

// dbAIWho is the "connection X (driver)" part of each question.
func dbAIWho(driver, conn string) string {
	return fmt.Sprintf("connection %q (%s)", dbOneLine(conn), driver)
}

// dbAIFixAsk is Fix Error's attachment (the statement and its error) and
// question.
func dbAIFixAsk(driver, conn, sql string, err error) (aiAttachment, string) {
	msg := ""
	if err != nil {
		msg = dbRedact(err.Error())
	}
	text := dbRedact(strings.TrimSpace(sql)) + "\n\n-- error:\n" + msg
	q := "This query failed on " + dbAIWho(driver, conn) + ". Explain the error in a few lines, then give the fixed query in one ```sql block. " +
		"Use db_schema to check the table and column names. Do not run a write: the user runs the fix."
	return aiAttachment{Label: "failed query", Text: text}, q
}

// dbAIExplainAsk is Explain Query's attachment and question.
func dbAIExplainAsk(driver, conn, sql string) (aiAttachment, string) {
	q := "Explain what this query does on " + dbAIWho(driver, conn) + ", step by step, and point out slow or risky parts. " +
		"You may use db_schema and db_explain (it shows the plan without running the query)."
	return aiAttachment{Label: "query", Text: dbRedact(strings.TrimSpace(sql))}, q
}

// dbAIGenerateAsk is Generate SQL's question; "" when want is empty.
func dbAIGenerateAsk(driver, conn, want string) string {
	want = strings.TrimSpace(want)
	if want == "" {
		return ""
	}
	return "Write SQL for " + dbAIWho(driver, conn) + ": " + want + "\n" +
		"Read the tables and columns with db_schema first; use only names that exist. Answer with the query in one ```sql block and a short note. Do not run it."
}

// dbStatementAtLine is the statement of text that holds the 1-based line.
func dbStatementAtLine(text, driver string, line int) string {
	spans := dbStatementSpans(text, driver)
	if len(spans) == 0 {
		return ""
	}
	offset := 0
	for i, l := range strings.Split(text, "\n") {
		if i+1 >= line {
			break
		}
		offset += len(l) + 1
	}
	for _, sp := range spans {
		if offset <= sp.end {
			return sp.sql
		}
	}
	return spans[len(spans)-1].sql
}

// dispatchDBAIPalette runs the db-ai-* entries.
func (m *Model) dispatchDBAIPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "db-ai-fix-error":
		return m.databaseAIFix(), true
	case "db-ai-explain-query":
		return m.databaseAIExplain(), true
	case "db-ai-generate-sql":
		return m.databaseAIGenerateStart(), true
	}
	return nil, false
}

// databaseAIDriver is the driver of the connection id ("" when unknown).
func (m *Model) databaseAIDriver(id string) string {
	if m.databases == nil {
		return ""
	}
	if c, ok := m.databases.connection(id); ok {
		return c.Driver
	}
	return ""
}

func (m *Model) databaseAISend(att *aiAttachment, q string) tea.Cmd {
	if att != nil {
		s := m.aiS()
		s.attachments = append(s.attachments, *att)
	}
	m.openAIPanel(true)
	return m.sendAIChat(q)
}

// databaseAIFix is Fix Error: the shown result must be a failed run.
func (m *Model) databaseAIFix() tea.Cmd {
	if m.databases == nil {
		return m.pushToastDetail(toast.Info, "No query error", "Run a statement first.")
	}
	r := m.dbConsole().shown
	if r == nil || r.result.Err == nil {
		return m.pushToastDetail(toast.Info, "No query error", "The last result did not fail.")
	}
	if c, ok := m.aiReady(); !ok {
		return c
	}
	att, q := dbAIFixAsk(m.databaseAIDriver(r.conn), r.conn, r.sql, r.result.Err)
	return m.databaseAISend(&att, q)
}

// databaseAIExplain is Explain Query: the selection or the statement at
// the cursor of a SQL console, else the statement of the shown result.
func (m *Model) databaseAIExplain() tea.Cmd {
	conn, sql := m.databaseAIStatement()
	if strings.TrimSpace(sql) == "" {
		return m.pushToastDetail(toast.Info, "No SQL to explain", "Put the cursor in a statement of a SQL console.")
	}
	if c, ok := m.aiReady(); !ok {
		return c
	}
	att, q := dbAIExplainAsk(m.databaseAIDriver(conn), conn, sql)
	return m.databaseAISend(&att, q)
}

func (m *Model) databaseAIStatement() (conn, sql string) {
	if m.databases == nil {
		return "", ""
	}
	s := m.dbConsole()
	if m.nvim != nil {
		if file, lines, ok := m.httpBufText(0); ok && strings.EqualFold(lastExt(file), ".sql") {
			conn = s.consoleConn(file, lines)
			var pos struct {
				Line      int    `json:"line"`
				Selection string `json:"selection"`
			}
			var raw string
			_ = m.nvim.ExecLuaResult(dbSelectionLua, &raw)
			_ = json.Unmarshal([]byte(raw), &pos)
			if strings.TrimSpace(pos.Selection) != "" {
				return conn, pos.Selection
			}
			return conn, dbStatementAtLine(strings.Join(lines, "\n"), m.databaseAIDriver(conn), max(pos.Line, 1))
		}
	}
	if r := s.shown; r != nil && r.describe == nil {
		if r.plan != nil {
			return r.conn, r.plan.target
		}
		return r.conn, r.sql
	}
	return "", ""
}

// databaseAIGenerateStart asks what the query should do.
func (m *Model) databaseAIGenerateStart() tea.Cmd {
	m.ensureDatabasesLoaded()
	if m.databaseContextConn() == "" {
		return m.pushToastDetail(toast.Info, "No SQL connection", "Select a connection in the Databases view, or open its console.")
	}
	if c, ok := m.aiReady(); !ok {
		return c
	}
	m.dbConsole().aiConn = m.databaseContextConn()
	return m.openHTTPPrompt(promptKindDBAIGenerate, "✦ Generate SQL with AI", "What should the query do?", "")
}

// databaseAIGeneratePrompt is the promptKindDBAIGenerate submit.
func (m *Model) databaseAIGeneratePrompt(value string) tea.Cmd {
	s := m.dbConsole()
	conn := s.aiConn
	s.aiConn = ""
	q := dbAIGenerateAsk(m.databaseAIDriver(conn), conn, value)
	if q == "" || conn == "" {
		return nil
	}
	return m.databaseAISend(nil, q)
}
