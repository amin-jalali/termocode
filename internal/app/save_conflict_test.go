package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/editor"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/toast"
)

// SCN-10: two windows on one folder. Window A saves notes.txt while
// window B has unsaved edits in it. B's Ctrl+S must not overwrite A's
// text silently, and must not claim "Saved" when nothing was written: it
// asks first (Cancel first and focused), and Overwrite writes B's text.
func TestSaveAsksWhenTheFileChangedOnDisk(t *testing.T) {
	c, err := nvim.New()
	if err != nil {
		t.Skipf("nvim not available: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	_ = c.Command("set cmdheight=0")
	if err := c.ExecLua(fileReloadLua); err != nil {
		t.Fatal(err)
	}
	m := Model{nvim: c, editor: editor.New(c), w: 120, h: 40, toast: toast.New()}
	p := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(p, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Command("edit " + p); err != nil {
		t.Fatal(err)
	}
	_ = c.ExecLua(`vim.api.nvim_buf_set_lines(0, -1, -1, false, {'mine'})`)
	if err := os.WriteFile(p, []byte("one\ntheirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(p, later, later)

	m.saveActiveFile()
	if !m.confirmOpen || m.confirmKind != confirmKindSaveConflict {
		t.Fatalf("no card: open=%v kind=%v", m.confirmOpen, m.confirmKind)
	}
	checkCardRule(t, "save conflict", m, true)
	if b, _ := os.ReadFile(p); string(b) != "one\ntheirs\n" {
		t.Fatalf("written before the answer: %q", b)
	}

	// Cancel keeps both: the edits stay, the file stays.
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if b, _ := os.ReadFile(p); string(b) != "one\ntheirs\n" {
		t.Fatalf("Cancel wrote the file: %q", b)
	}
	if mod, _ := c.EvalLuaString(`return tostring(vim.bo.modified)`); mod != "true" {
		t.Fatalf("Cancel lost the edits (modified=%s)", mod)
	}

	// Asked again; Overwrite writes the editor's text.
	m.saveActiveFile()
	if !m.confirmOpen {
		t.Fatal("no card on the second save")
	}
	m.confirmOpen = false
	m.handleConfirmSelect("overwrite")
	if b, _ := os.ReadFile(p); string(b) != "one\nmine\n" {
		t.Fatalf("after Overwrite: %q", b)
	}

	// Saved now: the next save writes without asking.
	_ = c.ExecLua(`vim.api.nvim_buf_set_lines(0, -1, -1, false, {'more'})`)
	m.saveActiveFile()
	if m.confirmOpen {
		t.Fatal("asked again after the conflict was resolved")
	}
	if b, _ := os.ReadFile(p); string(b) != "one\nmine\nmore\n" {
		t.Fatalf("plain save: %q", b)
	}
}

func changedDiskModel(t *testing.T) (*Model, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("XDG_STATE_HOME", home)
	c, err := nvim.New()
	if err != nil {
		t.Skipf("nvim not available: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	_ = c.Command("set cmdheight=0")
	if err := c.ExecLua(fileReloadLua); err != nil {
		t.Fatal(err)
	}
	m := &Model{nvim: c, editor: editor.New(c), w: 120, h: 40, toast: toast.New()}
	p := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(p, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Command("edit " + p); err != nil {
		t.Fatal(err)
	}
	_ = c.ExecLua(`vim.api.nvim_buf_set_lines(0, -1, -1, false, {'mine'})`)
	if err := os.WriteFile(p, []byte("one\ntheirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(p, later, later)

	return m, p
}

func TestBulkSaveAsksWhenFileChangedOnDisk(t *testing.T) {
	for _, action := range []string{"all", "close", "quit"} {
		for _, answer := range []string{"cancel", "overwrite"} {
			t.Run(action+"/"+answer, func(t *testing.T) {
				m, p := changedDiskModel(t)
				var buf int
				if err := m.nvim.EvalLuaArgs(`return vim.api.nvim_get_current_buf()`, &buf); err != nil {
					t.Fatal(err)
				}
				switch action {
				case "all":
					m.saveAll()
				case "close":
					m.confirmKind = confirmKindCloseBuffer
					m.confirmTargetID = buf
					m.handleConfirmSelect("save")
				case "quit":
					m.onQuitConfirm("save")
				}
				if !m.confirmOpen || m.confirmKind != confirmKindSaveConflict {
					t.Fatalf("missing disk conflict card: kind=%v", m.confirmKind)
				}
				checkCardRule(t, "bulk save conflict", *m, true)
				if b, _ := os.ReadFile(p); string(b) != "one\ntheirs\n" {
					t.Fatalf("wrote before answer: %q", b)
				}
				m.confirmOpen = false
				m.handleConfirmSelect(answer)
				want := "one\ntheirs\n"
				if answer == "overwrite" {
					want = "one\nmine\n"
				}
				if b, _ := os.ReadFile(p); string(b) != want {
					t.Fatalf("after %s: %q", answer, b)
				}
				if answer == "cancel" && len(m.unsavedFiles()) != 1 {
					t.Fatal("cancel lost unsaved edits")
				}
				if action == "close" && answer == "overwrite" {
					listed, err := m.nvim.EvalLuaStringArgs(`return tostring(vim.fn.buflisted(...))`, buf)
					if err != nil || listed != "0" {
						t.Fatalf("buffer not closed: %q %v", listed, err)
					}
				}
			})
		}
	}
}

func TestSaveAllResumesAcrossDiskConflicts(t *testing.T) {
	m, first := changedDiskModel(t)
	second := filepath.Join(filepath.Dir(first), "second.txt")
	if err := os.WriteFile(second, []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.nvim.Command("edit " + second); err != nil {
		t.Fatal(err)
	}
	if err := m.nvim.ExecLua(`vim.api.nvim_buf_set_lines(0, -1, -1, false, {'mine'})`); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("external\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(second, later, later); err != nil {
		t.Fatal(err)
	}
	m.saveAll()
	for i := 0; i < 2; i++ {
		if !m.confirmOpen || m.confirmKind != confirmKindSaveConflict {
			t.Fatalf("missing conflict %d", i)
		}
		m.confirmOpen = false
		m.handleConfirmSelect("overwrite")
	}
	if len(m.unsavedFiles()) != 0 {
		t.Fatal("Save All did not finish")
	}
	for p, want := range map[string]string{first: "one\nmine\n", second: "two\nmine\n"} {
		if b, _ := os.ReadFile(p); string(b) != want {
			t.Fatalf("%s: %q", p, b)
		}
	}
}
