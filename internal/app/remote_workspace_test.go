package app

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containerhost"
	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
	"github.com/amin-jalali/termocode/internal/explorer"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// fakeContainerWS is a container workspace /app in api-1 on the fake
// engine (never a real one), with main.go and pkg/util.go.
func fakeContainerWS(t *testing.T, mod ...func(*fakedocker.Container)) (workspace.Workspace, *containerhost.Host, *fakedocker.Docker, *fakedocker.Container) {
	t.Helper()
	d := fakedocker.New()
	c := d.Add(t, "api-1")
	c.WriteFile(t, "/app/main.go", []byte("package main\n\nfunc main() {}\n"), 0o644)
	c.WriteFile(t, "/app/pkg/util.go", []byte("package pkg\n"), 0o644)
	for _, f := range mod {
		f(c)
	}
	h, err := containerhost.New(context.Background(), containerhost.Config{Container: "api-1", Root: "/app", X: d})
	if err != nil {
		t.Fatal(err)
	}
	return h.Workspace(), h, d, c
}

// newContainerModel is New() (without nvim) on a container workspace.
func newContainerModel(t *testing.T) (Model, *containerhost.Host, *fakedocker.Docker, *fakedocker.Container) {
	t.Helper()
	ws, h, d, c := fakeContainerWS(t)
	setProcWorkspace(t, ws)
	t.Setenv("TERMOCODE_NO_NVIM", "1")
	m := New()
	pumpExplorerLoads(t, &m)
	return m, h, d, c
}

// pumpExplorerLoads runs the explorer's container listings (they run off
// the UI loop, FIX-2) and applies them, until none is left.
func pumpExplorerLoads(t *testing.T, m *Model) {
	t.Helper()
	for i := 0; i < 100; i++ {
		cmd := m.explorer.ScheduleLoads()
		if cmd == nil {
			return
		}
		for _, msg := range runBatch(cmd) {
			if lm, ok := msg.(explorer.DirLoadedMsg); ok {
				m.onExplorerDirLoaded(lm)
			}
		}
	}
	t.Fatal("explorer listings never ended")
}

// runBatch runs cmd and every command of a batch it returns.
func runBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, runBatch(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestContainerWorkspaceExplorerListsTheContainer(t *testing.T) {
	m, _, _, _ := newContainerModel(t)
	if got := m.explorer.PrimaryRoot(); got != "/app" {
		t.Fatalf("explorer root = %q, want /app", got)
	}
	m.explorer.RevealPath("/app/pkg/util.go")
	pumpExplorerLoads(t, &m)
	if got := m.explorer.CursorPath(); got != "/app/pkg/util.go" {
		t.Fatalf("cursor = %q after reveal", got)
	}
}
