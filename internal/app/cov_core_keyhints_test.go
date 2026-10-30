package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The key hints source reads nvim's leader: a space leader is
// "<space>", none is "\"; Send types into nvim; no nvim reads nothing.
func TestCovCoreKeyHintSource(t *testing.T) {
	var none nvimHintSource
	if none.Leader() != "\\" || none.Timeout() != 0 || len(none.Maps()) != 0 {
		t.Error("no nvim: want the defaults")
	}
	none.Send("x")
	m := covNvimModel(t, 100, 30)
	covEditorFile(t, &m, "k.txt", "one\n")
	src := nvimHintSource{c: m.nvim}
	if src.Leader() != "\\" {
		t.Errorf("default leader = %q", src.Leader())
	}
	_ = m.nvim.ExecLua(`vim.g.mapleader = ' '`)
	if src.Leader() != "<space>" {
		t.Errorf("space leader = %q", src.Leader())
	}
	_ = m.nvim.ExecLua(`vim.g.mapleader = ','`)
	if src.Leader() != "," {
		t.Errorf("comma leader = %q", src.Leader())
	}
	src.Send("A!<Esc>")
	covWaitLua(t, m, `return vim.fn.getline(1)`, "one!")
	if got := hintKeys([]string{"<", "a", " "}); got != "<lt>a " {
		t.Errorf("hintKeys = %q", got)
	}
}

// Toggling key hints with a config that cannot be saved says so.
func TestCovCoreKeyHintsToggleSaveFails(t *testing.T) {
	root := withConfigDir(t)
	m := covNew(t)
	_ = os.RemoveAll(filepath.Join(root, "termocode"))
	_ = os.WriteFile(filepath.Join(root, "termocode"), nil, 0o644)
	_ = m.toggleKeyHints()
	if !strings.Contains(toastText(m), "Settings error") {
		t.Errorf("toast:\n%s", toastText(m))
	}
	m.hints = nil
	if m.onKeyHintTick(keyHintTickMsg{}) != nil {
		t.Error("a tick without hints did something")
	}
}
