package containerhost

import (
	"context"
	"errors"
	"testing"
)

func TestHostNamesItsContainerAndContext(t *testing.T) {
	h, _, _ := newHost(t, func(c *Config) { c.Context = "prod-ssh" })
	if h.Container() != "api-1" || h.Context() != "prod-ssh" || h.Root() != "/app" {
		t.Fatalf("container %q context %q root %q", h.Container(), h.Context(), h.Root())
	}
}

func TestStartBringsAStoppedContainerBack(t *testing.T) {
	h, d, c := newHost(t)
	c.Stop()
	if h.CheckOnline(context.Background()) {
		t.Fatal("a stopped container counts as online")
	}
	if err := h.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !h.Online() {
		t.Fatal("not online after Start")
	}
	calls := d.Calls()
	found := false
	for _, cl := range calls {
		if len(cl.Args) == 2 && cl.Args[0] == "start" && cl.Args[1] == "api-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no docker start call: %+v", calls)
	}
}

func TestStartOfAGoneContainerIsNotRunning(t *testing.T) {
	h, _, c := newHost(t)
	c.Remove()
	if err := h.Start(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("err = %v, want ErrNotRunning", err)
	}
}
