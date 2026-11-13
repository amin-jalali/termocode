package app

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
	"github.com/amin-jalali/termocode/internal/containers"
)

// countPipe is a shellmux Piper that only counts its starts and fails,
// so the host falls back to one-shot engine calls.
type countPipe struct {
	mu     sync.Mutex
	starts int
}

func (p *countPipe) Pipe(context.Context, ...string) (io.WriteCloser, io.ReadCloser, io.ReadCloser, func(), error) {
	p.mu.Lock()
	p.starts++
	p.mu.Unlock()
	return nil, nil, nil, nil, errors.New("no mux in this test")
}

func (p *countPipe) n() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.starts
}

// filesBrowseModel opens the Containers file browser on api-1 of an
// engine whose context points at endpoint, and lists "/".
func filesBrowseModel(t *testing.T, endpoint string) (*Model, *countPipe) {
	t.Helper()
	d := fakedocker.New()
	d.Add(t, "api-1")
	d.SetEndpoint("prod", endpoint)
	pipe := &countPipe{}
	old := containerFilesPipe
	containerFilesPipe = pipe
	t.Cleanup(func() { containerFilesPipe = old })
	mm := newTestModel(t)
	m := &mm
	m.services = svcTestState()
	m.services.containerEngine = containers.Engine{Binary: "docker", Kind: "docker", Context: "prod", Reachable: true, X: d}
	run := func(cmd tea.Cmd) tea.Cmd {
		if cmd == nil {
			t.Fatal("no command")
		}
		msg, ok := cmd().(containerUIDoneMsg)
		if !ok {
			t.Fatalf("msg = %T", msg)
		}
		return msg.apply(m)
	}
	list := run(m.openContainerFiles(containers.Container{ID: "api-1", Names: []string{"api-1"}, State: "running"}))
	run(list)
	if m.services.files.host == nil {
		t.Fatal("the browser has no host")
	}
	t.Cleanup(func() { _ = m.services.files.host.Close() })
	return m, pipe
}

// RC-9: the Containers file browser on a host over ssh:// (or tcp://)
// runs its listings through one long shell (the shellmux), as a folder
// opened in a container does; a local engine never starts one.
func TestContainerFilesUseTheMuxOnARemoteHost(t *testing.T) {
	if _, pipe := filesBrowseModel(t, "ssh://deploy@build.example"); pipe.n() == 0 {
		t.Fatal("a remote host's file browser did not try the shellmux")
	}
}

func TestContainerFilesNoMuxOnALocalEngine(t *testing.T) {
	if _, pipe := filesBrowseModel(t, "unix:///var/run/docker.sock"); pipe.n() != 0 {
		t.Fatalf("a local engine started the shellmux %d times", pipe.n())
	}
}
