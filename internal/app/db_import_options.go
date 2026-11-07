package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
	tea "github.com/charmbracelet/bubbletea"
)

// Import options (DBX-37): the import card's Options… button opens a
// picker of the reading options (lines to skip at the start, the quote
// character, the escape inside quotes) and the per-column formats (trim,
// a NULL text, a date format, a decimal comma). Changing a reading option
// reads the file again and comes back to the card. Several files (a
// pattern like data/*.csv) import into one table in one transaction. Like
// the import itself, none of this is open to the AI.

const (
	pickerKindDBImportOpts pickerKindEnum = 7370 + iota
	pickerKindDBImportFmtCol
	pickerKindDBImportFmtKind
)

const (
	promptKindDBImportGlob promptKindEnum = 7370 + iota
	promptKindDBImportSkip
	promptKindDBImportNull
	promptKindDBImportDate
)

// dbImportGlobMax is the most files one pattern may import.
const dbImportGlobMax = 200

// dbImportOptionsText is the Options line of the card ("" for plain CSV).
func dbImportOptionsText(o dbclient.CSVOptions) string {
	var parts []string
	if o.SkipLines > 0 {
		parts = append(parts, "skip the first "+dbPlural(o.SkipLines, "line"))
	}
	switch {
	case o.Quote == dbclient.QuoteNone:
		parts = append(parts, "no quoting")
	case o.Quote != 0 && o.Quote != '"':
		parts = append(parts, "quote "+string(o.Quote))
	}
	if o.Escape != 0 {
		parts = append(parts, "escape "+string(o.Escape))
	}
	return strings.Join(parts, " · ")
}

func dbImportQuoteName(q rune) string {
	switch q {
	case dbclient.QuoteNone:
		return "none (\" is text)"
	case '\'':
		return "' (single quote)"
	}
	return `" (double quote)`
}

func (m *Model) databaseImportOptionsPicker() tea.Cmd {
	imp := m.dbConsole().imp
	if imp == nil {
		return nil
	}
	o := imp.opts
	escape := `doubled quote ("")`
	if o.Escape == '\\' {
		escape = `backslash (\")`
	}
	set := 0
	for _, f := range imp.formats {
		if !f.IsZero() {
			set++
		}
	}
	formats := "none set"
	if set > 0 {
		formats = dbPlural(set, "column") + " set"
	}
	items := []picker.Item{
		{ID: "skip", Title: "Lines to skip at the start…", Hint: strconv.Itoa(o.SkipLines)},
		{ID: "quote", Title: "Quote character", Hint: dbImportQuoteName(o.Quote) + " · Enter changes"},
		{ID: "escape", Title: "Quote inside a quoted field", Hint: escape + " · Enter changes"},
		{ID: "formats", Title: "Column formats…", Hint: formats},
	}
	m.picker = picker.NewItems("Import options (Esc goes back to the card)", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBImportOpts
	m.pickerOpen = true
	return nil
}

// databaseImportReload reads the file again with the changed options.
func (m *Model) databaseImportReload() tea.Cmd {
	s := m.dbConsole()
	imp := s.imp
	if imp == nil {
		return nil
	}
	if err := imp.opts.Valid(imp.sniff.Delimiter); err != nil {
		imp.opts = imp.sniff.Options
		return tea.Batch(m.pushToastDetail(toast.Warn, "Cannot use that option", err.Error()), m.databaseImportOptionsPicker())
	}
	sess := m.databases.sessions[imp.sessKey]
	if sess == nil {
		s.imp = nil
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	var header *bool
	if imp.opts.SkipLines == imp.sniff.Options.SkipLines {
		h := imp.sniff.Header
		header = &h
	}
	return m.databaseImportRead(imp, sess, header)
}

func (m *Model) databaseImportOptionsPicked(id string) tea.Cmd {
	imp := m.dbConsole().imp
	if imp == nil {
		return nil
	}
	switch id {
	case "skip":
		return m.openHTTPPrompt(promptKindDBImportSkip, "Import options", "Lines to skip before the header (0 or more):", strconv.Itoa(imp.opts.SkipLines))
	case "quote":
		switch imp.opts.Quote {
		case 0, '"':
			imp.opts.Quote = '\''
		case '\'':
			imp.opts.Quote = dbclient.QuoteNone
			imp.opts.Escape = 0 // nothing is quoted, so nothing is escaped
		default:
			imp.opts.Quote = 0
		}
		return m.databaseImportReload()
	case "escape":
		if imp.opts.Quote == dbclient.QuoteNone {
			return tea.Batch(m.pushToastDetail(toast.Info, "Nothing to escape", "The quote character is none."), m.databaseImportOptionsPicker())
		}
		if imp.opts.Escape == 0 {
			imp.opts.Escape = '\\'
		} else {
			imp.opts.Escape = 0
		}
		return m.databaseImportReload()
	case "formats":
		return m.databaseImportFormatColPicker()
	}
	return m.databaseImportCard()
}

func (m *Model) databaseImportFormatColPicker() tea.Cmd {
	imp := m.dbConsole().imp
	if imp == nil {
		return nil
	}
	var items []picker.Item
	for i, c := range imp.cols {
		if imp.mapping[i] < 0 {
			continue
		}
		hint := "as is"
		if i < len(imp.formats) && !imp.formats[i].IsZero() {
			hint = dbOneLine(imp.formats[i].Describe())
		}
		items = append(items, picker.Item{ID: strconv.Itoa(i), Title: dbOneLine(c), Hint: hint})
	}
	if len(items) == 0 {
		return tea.Batch(m.pushToastDetail(toast.Info, "No mapped columns", "Map columns first."), m.databaseImportCard())
	}
	m.picker = picker.NewItems("Column formats (Enter changes one, Esc goes back)", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBImportFmtCol
	m.pickerOpen = true
	return nil
}

func dbOnOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func (m *Model) databaseImportFormatKindPicker() tea.Cmd {
	imp := m.dbConsole().imp
	if imp == nil || imp.fmtCol < 0 || imp.fmtCol >= len(imp.formats) {
		return m.databaseImportCard()
	}
	f := imp.formats[imp.fmtCol]
	null, date := "(none)", "(none)"
	if f.NullText != "" {
		null = strconv.Quote(f.NullText)
	}
	if f.DateLayout != "" {
		date = f.DateLayout
	}
	items := []picker.Item{
		{ID: "trim", Title: "Trim spaces", Hint: dbOnOff(f.Trim) + " · Enter changes"},
		{ID: "null", Title: "Text that means NULL…", Hint: dbOneLine(null) + " · an empty cell is always NULL"},
		{ID: "date", Title: "Date format…", Hint: dbOneLine(date) + " · sent as YYYY-MM-DD"},
		{ID: "decimal", Title: "Decimal comma (1.234,5 → 1234.5)", Hint: dbOnOff(f.DecimalComma) + " · Enter changes"},
		{ID: "clear", Title: "Clear this column's format"},
	}
	m.picker = picker.NewItems("Format of "+dbOneLine(imp.cols[imp.fmtCol]), items)
	m.picker.SetSize(m.w, m.h)
	m.pickerKind = pickerKindDBImportFmtKind
	m.pickerOpen = true
	return nil
}

func (m *Model) databaseImportFormatPicked(id string) tea.Cmd {
	imp := m.dbConsole().imp
	if imp == nil {
		return nil
	}
	if m.pickerKind == pickerKindDBImportFmtCol {
		col, err := strconv.Atoi(id)
		if err != nil || col < 0 || col >= len(imp.cols) {
			return m.databaseImportFormatColPicker()
		}
		if len(imp.formats) != len(imp.cols) {
			imp.formats = make([]dbclient.ColumnFormat, len(imp.cols))
		}
		imp.fmtCol = col
		return m.databaseImportFormatKindPicker()
	}
	if imp.fmtCol < 0 || imp.fmtCol >= len(imp.formats) {
		return m.databaseImportCard()
	}
	f := &imp.formats[imp.fmtCol]
	switch id {
	case "trim":
		f.Trim = !f.Trim
	case "decimal":
		f.DecimalComma = !f.DecimalComma
	case "clear":
		*f = dbclient.ColumnFormat{}
	case "null":
		return m.openHTTPPrompt(promptKindDBImportNull, "Format of "+dbOneLine(imp.cols[imp.fmtCol]), `Text that means NULL (for example \N or NULL; empty: none):`, f.NullText)
	case "date":
		return m.openHTTPPrompt(promptKindDBImportDate, "Format of "+dbOneLine(imp.cols[imp.fmtCol]), "Date format with YYYY YY MM DD HH mm ss (for example DD/MM/YYYY; empty: none):", f.DateLayout)
	}
	return m.databaseImportFormatKindPicker()
}

// databaseImportGlob is the several-files prompt: the pattern is matched
// in the workspace (or from a full path), sorted, and the files are read
// like one.
func (m *Model) databaseImportGlob(value string) tea.Cmd {
	s := m.dbConsole()
	imp := s.imp
	if imp == nil {
		return nil
	}
	abs, display, err := m.dbResolvePath(value)
	if err != nil {
		s.imp = nil
		return m.pushToastDetail(toast.Warn, "Cannot import", err.Error())
	}
	matches, err := filepath.Glob(abs)
	if err != nil {
		s.imp = nil
		return m.pushToastDetail(toast.Warn, "Cannot import", "The pattern is not valid: "+err.Error())
	}
	var files []string
	for _, p := range matches {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			files = append(files, p)
		}
	}
	sort.Strings(files)
	switch {
	case len(files) == 0:
		s.imp = nil
		return m.pushToastDetail(toast.Info, "No files match", sanitizeDBText(display))
	case len(files) > dbImportGlobMax:
		s.imp = nil
		return m.pushToastDetail(toast.Warn, "Too many files", fmt.Sprintf("%d files match; one import takes at most %d.", len(files), dbImportGlobMax))
	}
	sess := m.databases.sessions[imp.sessKey]
	if sess == nil {
		s.imp = nil
		return m.pushToastDetail(toast.Info, "Not connected", "Connect first: Enter on the connection.")
	}
	imp.path, imp.more, imp.display = files[0], files[1:], display
	return m.databaseImportRead(imp, sess, nil)
}

// databaseImportPrompt routes the option prompts. ok=false: not ours.
func (m *Model) databaseImportPrompt(kind promptKindEnum, value string) (tea.Cmd, bool) {
	switch kind {
	case promptKindDBImportGlob, promptKindDBImportSkip, promptKindDBImportNull, promptKindDBImportDate:
	default:
		return nil, false
	}
	imp := m.dbConsole().imp
	switch kind {
	case promptKindDBImportGlob:
		return m.databaseImportGlob(value), true
	case promptKindDBImportSkip:
		if imp == nil {
			return nil, true
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 0 || n > 1_000_000 {
			return tea.Batch(m.pushToastDetail(toast.Warn, "Not a line count", "Type 0 or a whole number of lines."), m.databaseImportOptionsPicker()), true
		}
		imp.opts.SkipLines = n
		return m.databaseImportReload(), true
	case promptKindDBImportNull:
		if imp == nil || imp.fmtCol < 0 || imp.fmtCol >= len(imp.formats) {
			return nil, true
		}
		imp.formats[imp.fmtCol].NullText = value
		return m.databaseImportFormatKindPicker(), true
	case promptKindDBImportDate:
		if imp == nil || imp.fmtCol < 0 || imp.fmtCol >= len(imp.formats) {
			return nil, true
		}
		v := strings.TrimSpace(value)
		if v != "" {
			if err := dbclient.CheckDateLayout(v); err != nil {
				return tea.Batch(m.pushToastDetail(toast.Warn, "Cannot use that date format", err.Error()), m.databaseImportFormatKindPicker()), true
			}
		}
		imp.formats[imp.fmtCol].DateLayout = v
		return m.databaseImportFormatKindPicker(), true
	}
	return nil, false
}

// databaseImportPromptClosed: Esc in an option prompt goes back to the
// picker it came from; Esc in the pattern prompt ends the import.
func (m *Model) databaseImportPromptClosed() tea.Cmd {
	if m.databases == nil || m.databases.console == nil || m.databases.console.imp == nil {
		return nil
	}
	switch m.promptKind {
	case promptKindDBImportGlob:
		m.databases.console.imp = nil
	case promptKindDBImportSkip:
		return m.databaseImportOptionsPicker()
	case promptKindDBImportNull, promptKindDBImportDate:
		return m.databaseImportFormatKindPicker()
	}
	return nil
}
