package mcp

import (
	"context"
	"strings"
	"testing"
	"time"
)

// MCPX-12: the server's capabilities are kept from initialize; resources
// are listed over pages, templates listed and a resource read.
func TestClientResources(t *testing.T) {
	changed := make(chan string, 4)
	p := startFake(t, nil, Options{ResourcesChanged: func(uri string) { changed <- uri }})
	caps := p.Client.Capabilities()
	if !caps.Resources || !caps.Prompts || !caps.Completions {
		t.Fatalf("caps = %+v", caps)
	}
	ctx := context.Background()
	rs, err := p.Client.ListResources(ctx)
	if err != nil || len(rs) != 3 || rs[0].URI != "file:///notes/a.txt" || rs[0].Name != "a" || rs[2].Name != "chart" {
		t.Fatalf("resources %+v, %v", rs, err)
	}
	ts, err := p.Client.ListResourceTemplates(ctx)
	if err != nil || len(ts) != 1 || ts[0].URITemplate != "notes://{id}" {
		t.Fatalf("templates %+v, %v", ts, err)
	}
	cs, err := p.Client.ReadResource(ctx, "file:///notes/a.txt")
	if err != nil || len(cs) != 1 || cs[0].Text != "note a body" || cs[0].MimeType != "text/plain" {
		t.Fatalf("read %+v, %v", cs, err)
	}
	if cs, err = p.Client.ReadResource(ctx, "notes://7"); err != nil || cs[0].Text != "note 7" {
		t.Fatalf("template read %+v, %v", cs, err)
	}
	if _, err = p.Client.ReadResource(ctx, "file:///nope"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing resource: %v", err)
	}
	// PR-14: a change notice reaches the host ("" for the list).
	if _, err := p.Client.CallTool(ctx, "notify_resources", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case u := <-changed:
		if u != "" {
			t.Errorf("list change uri = %q", u)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no resources change notice")
	}
}

// The Manager lists and reads a server's resources (starting it, with
// the trust question), and Tools tells which servers have resources,
// also from the disk cache.
func TestManagerResources(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	m := e.manager(t, 0, nil)
	ctx := context.Background()
	rs, ts, err := m.Resources(ctx, nil, "fake")
	if err != nil || len(rs) != 3 || len(ts) != 1 {
		t.Fatalf("resources %d templates %d: %v", len(rs), len(ts), err)
	}
	cs, err := m.ReadResource(ctx, nil, "fake", "file:///notes/b.md")
	if err != nil || len(cs) != 1 || cs[0].Text != "# B" {
		t.Fatalf("read %+v %v", cs, err)
	}
	list := m.Tools(ctx, nil)
	if len(list) != 1 || !list[0].Caps.Resources || !list[0].Caps.Prompts {
		t.Fatalf("caps in Tools: %+v", list)
	}
	m2 := e.manager(t, 0, nil) // from the cache: nothing starts
	if l := m2.Tools(ctx, nil); len(l) != 1 || !l[0].Caps.Resources || m2.Running() != 0 {
		t.Fatalf("cached caps: %+v running %d", l, m2.Running())
	}
	if _, _, err := m.Resources(ctx, nil, "nope"); err == nil {
		t.Fatal("unknown server: no error")
	}
}
