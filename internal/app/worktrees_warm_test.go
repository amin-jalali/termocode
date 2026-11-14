//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/worktreeform"
)

// CMP-96: a warm setup. The first worktree runs the setup and keeps a
// copy of what it made (warm.paths); the next one with the same lock
// files gets that copy back and skips the setup. The repo's env reaches
// the setup; the trust card names the env and the warm paths.
func TestWTSetupWarm(t *testing.T) {
	var runs []string
	var envs []map[string]string
	old := wtSetupExec
	wtSetupExec = func(dir, _ string, lines []string, env map[string]string) (string, error) {
		runs = append(runs, dir)
		envs = append(envs, env)
		if err := os.MkdirAll(filepath.Join(dir, "node_modules", "x"), 0o755); err != nil {
			return lines[0], err
		}
		return "", os.WriteFile(filepath.Join(dir, "node_modules", "x", "i.js"), []byte("1"), 0o644)
	}
	t.Cleanup(func() { wtSetupExec = old })

	m := wtFormModel(t)
	cfg := worktreeform.Config{Setup: []string{"npm ci"}, Env: map[string]string{"PORT": "3001"},
		Warm: &worktreeform.WarmConfig{Paths: []string{"node_modules"}, Key: []string{"package-lock.json"}}}
	newTree := func() string {
		d := t.TempDir()
		if err := os.WriteFile(filepath.Join(d, "package-lock.json"), []byte(`{"v":1}`), 0o644); err != nil {
			t.Fatal(err)
		}
		return d
	}

	// The trust card names the env and the warm paths.
	f := m.wt.form
	f.cfg = cfg
	m.wtSetupAsk(f, newTree())
	// The card's whole text: a long temp path (macOS) pushes the end of the
	// drawn card into its "…and N more lines" row.
	box := m.confirm.Message() + "\n" + ansi.Strip(m.confirm.Box())
	for _, want := range []string{"npm ci", "PORT", "node_modules"} {
		if !strings.Contains(box, want) {
			t.Errorf("trust card lacks %q:\n%s", want, box)
		}
	}
	m.confirmOpen = false
	m.wt.setupAsk = nil

	first := newTree()
	wtDrive(t, m, m.wtSetupStart(&wtSetupPending{folder: first, hash: "h", common: m.wt.common, setup: cfg.Setup, cfg: cfg}))
	if len(runs) != 1 || envs[0]["PORT"] != "3001" {
		t.Fatalf("first setup: runs %v envs %v", runs, envs)
	}
	key, err := worktreeform.WarmKey(first, "h", cfg)
	if err != nil || !worktreeform.HasWarm(worktreeform.WarmDir(m.wt.common, key)) {
		t.Fatalf("no warm copy kept: %v", err)
	}

	second := newTree()
	wtDrive(t, m, m.wtSetupStart(&wtSetupPending{folder: second, hash: "h", common: m.wt.common, setup: cfg.Setup, cfg: cfg}))
	if len(runs) != 1 {
		t.Fatalf("the setup ran again: %v", runs)
	}
	if b, err := os.ReadFile(filepath.Join(second, "node_modules", "x", "i.js")); err != nil || string(b) != "1" {
		t.Errorf("warm copy not restored: %q %v", b, err)
	}
	if r := m.wt.setups[second]; r == nil || r.state != wtSetupOK {
		t.Errorf("setup state %+v", r)
	}
	if !strings.Contains(wtLastToast(m), "warm copy") {
		t.Errorf("toast %q", wtLastToast(m))
	}

	// A changed lock file runs the setup again.
	third := newTree()
	if err := os.WriteFile(filepath.Join(third, "package-lock.json"), []byte(`{"v":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	wtDrive(t, m, m.wtSetupStart(&wtSetupPending{folder: third, hash: "h", common: m.wt.common, setup: cfg.Setup, cfg: cfg}))
	if len(runs) != 2 {
		t.Errorf("a new lock file did not run the setup: %v", runs)
	}
}

// The setup tab gets the repo's env too.
func TestWTSetupTabEnv(t *testing.T) {
	var jobs []terminalJob
	old := wtSetupSpawn
	wtSetupSpawn = func(m *Model, j terminalJob) (int, error) {
		jobs = append(jobs, j)
		return 77, nil
	}
	defer func() { wtSetupSpawn = old }()
	m := wtFormModel(t)
	cfg := worktreeform.Config{Setup: []string{"npm ci"}, Env: map[string]string{"PORT": "3001", "TOKEN": "${secret:X}"}}
	m.wtSetupStart(&wtSetupPending{folder: t.TempDir(), common: m.wt.common, setup: cfg.Setup, cfg: cfg})
	if len(jobs) != 1 || jobs[0].Env["PORT"] != "3001" {
		t.Fatalf("jobs %+v", jobs)
	}
	if _, ok := jobs[0].Env["TOKEN"]; ok {
		t.Error("a ${secret:} value reached the tab")
	}
}
