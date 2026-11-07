package containers

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// psExec answers `ps` with fixed JSON lines (or an error) and records the
// calls; it never runs a real engine.
type psExec struct {
	mu    sync.Mutex
	out   string
	code  int
	err   error
	delay time.Duration
	calls [][]string
}

func (p *psExec) Capture(ctx context.Context, _ string, argv ...string) (string, string, int, error) {
	p.mu.Lock()
	p.calls = append(p.calls, argv)
	p.mu.Unlock()
	if p.delay > 0 {
		select {
		case <-time.After(p.delay):
		case <-ctx.Done():
			return "", "", -1, ctx.Err()
		}
	}
	if p.err != nil {
		return "", "", -1, p.err
	}
	if p.code != 0 {
		return "", "Cannot connect to the Docker daemon at tcp://far:2376. Is the docker daemon running?", p.code, nil
	}
	return p.out, "", 0, nil
}

func (p *psExec) CaptureInput(ctx context.Context, dir string, _ []byte, argv ...string) (string, string, int, error) {
	return p.Capture(ctx, dir, argv...)
}

func (p *psExec) Stream(context.Context, string, ...string) (io.ReadCloser, func(), error) {
	return nil, nil, errors.New("no streams in this fake")
}

const (
	psLocal = `{"ID":"aa11","Names":"web","Image":"nginx:1.27","State":"running","Status":"Up 2 hours"}
{"ID":"aa22","Names":"db","Image":"postgres:17","State":"exited","Status":"Exited (0) 1 day ago"}
`
	psRemote = `{"ID":"bb11","Names":"api","Image":"ghcr.io/acme/api:1.4","State":"running","Status":"Up 5 minutes"}
`
)

// CTX-41: several engines/hosts in one list, each row tagged with its
// source; one source that fails is shown as a problem and never hides the
// rows of the others.
func TestListAllMergesSourcesAndKeepsProblems(t *testing.T) {
	local := &psExec{out: psLocal}
	remote := &psExec{out: psRemote}
	down := &psExec{code: 1}
	sources := []Source{
		{Name: "local", Engine: Engine{Binary: "docker", Kind: "docker", X: local}},
		{Name: "far", Engine: Engine{Binary: "docker", Kind: "docker", X: down}},
		{Name: "staging", Engine: Engine{Binary: "podman", Kind: "podman", X: remote}},
	}
	res := ListAll(context.Background(), sources)

	var got []string
	for _, c := range res.Containers {
		got = append(got, c.Source+"/"+c.Names[0])
	}
	// Source order first, then name.
	if strings.Join(got, ",") != "local/db,local/web,staging/api" {
		t.Fatalf("rows = %v", got)
	}
	if len(res.Problems) != 1 || res.Problems[0].Source != "far" || res.Problems[0].Err == nil {
		t.Fatalf("problems = %+v", res.Problems)
	}
	if !strings.Contains(res.Problems[0].Err.Error(), "not running") {
		t.Fatalf("problem text = %q", res.Problems[0].Err)
	}
	for _, x := range []*psExec{local, remote, down} {
		if len(x.calls) != 1 || x.calls[0][1] != "ps" {
			t.Fatalf("calls = %+v", x.calls)
		}
	}
}

// Sources are asked at the same time: a slow host does not add its
// time to the others.
func TestListAllRunsSourcesTogether(t *testing.T) {
	var sources []Source
	for _, n := range []string{"a", "b", "c", "d"} {
		sources = append(sources, Source{Name: n, Engine: Engine{Binary: "docker", Kind: "docker", X: &psExec{out: psRemote, delay: 200 * time.Millisecond}}})
	}
	start := time.Now()
	res := ListAll(context.Background(), sources)
	if d := time.Since(start); d > 700*time.Millisecond {
		t.Fatalf("ListAll took %v; sources ran one after another", d)
	}
	if len(res.Containers) != 4 || len(res.Problems) != 0 {
		t.Fatalf("res = %+v", res)
	}
}

func TestListAllCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := ListAll(ctx, []Source{{Name: "slow", Engine: Engine{Binary: "docker", Kind: "docker", X: &psExec{out: psRemote, delay: time.Second}}}})
	if len(res.Containers) != 0 || len(res.Problems) != 1 {
		t.Fatalf("res = %+v", res)
	}
}

func TestListAllNoSources(t *testing.T) {
	res := ListAll(context.Background(), nil)
	if len(res.Containers) != 0 || len(res.Problems) != 0 {
		t.Fatalf("res = %+v", res)
	}
}

// One source per docker context / podman connection, on the same binary;
// contexts with an error are left out, the current one comes first.
func TestSourcesFromContexts(t *testing.T) {
	base := Engine{Binary: "/usr/bin/docker", Kind: "docker", Timeout: 3 * time.Second}
	ctxs := []Context{
		{Name: "staging", Host: "ssh://deploy@staging"},
		{Name: "broken", Error: "context not found"},
		{Name: "default", Current: true},
		{Name: "prod", Host: "tcp://prod:2376", TLS: true},
	}
	got := SourcesFromContexts(base, ctxs)
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
		if s.Engine.Binary != base.Binary || s.Engine.Kind != "docker" || s.Engine.Timeout != base.Timeout {
			t.Fatalf("engine not copied: %+v", s.Engine)
		}
		if s.Engine.Context != s.Name {
			t.Fatalf("source %q has context %q", s.Name, s.Engine.Context)
		}
	}
	if strings.Join(names, ",") != "default,prod,staging" {
		t.Fatalf("sources = %v", names)
	}
}

func TestSourceLabel(t *testing.T) {
	if l := (Source{Name: "x"}).Label(); l != "x" {
		t.Fatalf("label = %q", l)
	}
	if l := (Source{Engine: Engine{Kind: "podman", Context: "far"}}).Label(); l != "podman/far" {
		t.Fatalf("label = %q", l)
	}
	if l := (Source{Engine: Engine{Kind: "docker"}}).Label(); l != "docker" {
		t.Fatalf("label = %q", l)
	}
}
