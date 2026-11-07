package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
)

// SCN-30: a config.json that termocode cannot read. It starts with the
// default settings (ADR 0005: config never breaks startup), says so once
// in plain words, and never writes over the file: cfgfile refuses a
// file that is not valid JSON, so every setting change shows the error
// too, and the first-run card stays hidden (its steps could not be
// saved). The user fixes the file with Open Config: Theme.

// settingsFileProblem says what is wrong with config.json in plain
// words; "" when it is fine, empty or missing.
func settingsFileProblem() string {
	path, err := settingsConfigPath()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(b)) == 0 {
		return "" // missing, or unreadable (SCN-7 covers the folder rights)
	}
	var cfg settingsConfig
	err = json.Unmarshal(b, &cfg)
	if err == nil {
		return ""
	}
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn):
		line, col := lineCol(b, syn.Offset)
		return fmt.Sprintf("config.json is not valid JSON (line %d, column %d)", line, col)
	case errors.As(err, &typ) && typ.Field != "":
		line, _ := lineCol(b, typ.Offset)
		return fmt.Sprintf("config.json: %s has the wrong type (line %d)", typ.Field, line)
	default:
		return "config.json is not valid JSON"
	}
}

// lineCol turns a byte offset into a 1-based line and column.
func lineCol(b []byte, off int64) (line, col int) {
	if off < 0 {
		off = 0
	}
	if off > int64(len(b)) {
		off = int64(len(b))
	}
	head := b[:off]
	line = bytes.Count(head, []byte("\n")) + 1
	col = len(head) - bytes.LastIndexByte(head, '\n')
	return line, col
}

// configBrokenCmd is the start toast for a broken config.json; nil when
// the file is fine. The problem also goes to errors.log.
func configBrokenCmd() tea.Cmd {
	p := settingsFileProblem()
	if p == "" {
		return nil
	}
	recordError("settings: " + p)
	body := p + ". termocode uses the default settings, and the file is not changed until you fix it. Open it with Open Config: Theme."
	return func() tea.Msg {
		return ToastMsg{Level: toast.Errr, Title: "Settings file is broken", Body: body}
	}
}
