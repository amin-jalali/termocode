package app

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// dbFormatPosLua returns the cursor line and the selected lines (lo, hi;
// 0 = no selection) of the current buffer, and leaves Visual mode.
const dbFormatPosLua = `
local a=vim.api
local mode=vim.fn.mode()
local cur=a.nvim_win_get_cursor(0)[1]
local lo,hi=0,0
if mode=='v' or mode=='V' or mode=='\22' then
 lo=vim.fn.getpos('v')[2];hi=cur
 if lo>hi then lo,hi=hi,lo end
 a.nvim_feedkeys(a.nvim_replace_termcodes('<Esc>',true,false,true),'n',false)
end
return vim.json.encode({line=cur,lo=lo,hi=hi})
`

// dbFormatApplyLua replaces lines [s, e) of the current buffer in ONE
// nvim_buf_set_lines call (one undo step) after checking they still
// hold what was formatted.
const dbFormatApplyLua = `
local s,e,repl,orig=...
local cur=vim.api.nvim_buf_get_lines(0,s,e,false)
if #cur~=#orig then return 'stale' end
for i=1,#cur do if cur[i]~=orig[i] then return 'stale' end end
vim.api.nvim_buf_set_lines(0,s,e,false,repl)
return 'ok'
`

// dbFormatRange picks the lines to format: the whole file (all), the
// selected lines (selLo..selHi, 1-based) or the whole lines of the
// statement at the cursor (1-based). It returns the 0-based range
// [start, end) and the formatted lines; an error leaves the buffer as
// it is.
func dbFormatRange(lines []string, cursor, selLo, selHi int, driver string, all bool) (int, int, []string, error) {
	start, end := 0, len(lines)
	switch {
	case all:
	case selLo > 0:
		start, end = max(selLo-1, 0), min(selHi, len(lines))
	default:
		text := strings.Join(lines, "\n")
		spans := dbStatementSpans(text, driver)
		if len(spans) == 0 {
			return 0, 0, nil, errors.New("no SQL at the cursor")
		}
		offset := 0
		for i, l := range lines {
			if i+1 >= cursor {
				break
			}
			offset += len(l) + 1
		}
		chosen := spans[len(spans)-1]
		for _, sp := range spans {
			if offset <= sp.end {
				chosen = sp
				break
			}
		}
		start = strings.Count(text[:chosen.start], "\n")
		end = strings.Count(text[:chosen.end], "\n") + 1
	}
	if start >= end {
		return 0, 0, nil, errors.New("no SQL to format")
	}
	src := strings.Join(lines[start:end], "\n")
	if strings.TrimSpace(src) == "" {
		return 0, 0, nil, errors.New("no SQL to format")
	}
	out, err := dbclient.FormatQuery(src, driver)
	if err != nil {
		return 0, 0, nil, err
	}
	return start, end, strings.Split(out, "\n"), nil
}

// databaseFormat formats the statement at the cursor, the selected lines
// or (all) the whole .sql file with the console connection's dialect.
func (m *Model) databaseFormat(all bool) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	file, lines, ok := m.httpBufText(0)
	if !ok || !strings.EqualFold(lastExt(file), ".sql") {
		return m.pushToastDetail(toast.Info, "Open a SQL file", "Format works in a .sql console or file.")
	}
	var pos struct {
		Line int `json:"line"`
		Lo   int `json:"lo"`
		Hi   int `json:"hi"`
	}
	var raw string
	_ = m.nvim.ExecLuaResult(dbFormatPosLua, &raw)
	_ = json.Unmarshal([]byte(raw), &pos)
	driver := ""
	m.ensureDatabasesLoaded()
	if c, ok := m.databases.connection(m.dbConsole().consoleConn(file, lines)); ok {
		driver = c.Driver
	}
	start, end, repl, err := dbFormatRange(lines, max(pos.Line, 1), pos.Lo, pos.Hi, driver, all)
	if err != nil {
		return m.pushToastDetail(toast.Info, "Not formatted", err.Error())
	}
	orig := lines[start:end]
	if slices.Equal(orig, repl) {
		return m.pushToast(toast.Info, "Already formatted")
	}
	var res string
	if err := m.nvim.ExecLuaResult(dbFormatApplyLua, &res, start, end, repl, orig); err != nil || res != "ok" {
		return m.pushToastDetail(toast.Warn, "Not formatted", "The buffer changed; try again.")
	}
	return m.pushToast(toast.Info, "SQL formatted")
}
