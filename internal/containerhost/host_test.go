package containerhost

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
)

func newHost(t *testing.T, mod ...func(*Config)) (*Host, *fakedocker.Docker, *fakedocker.Container) {
	t.Helper()
	d := fakedocker.New()
	c := d.Add(t, "api-1")
	c.Mkdir(t, "/app")
	cfg := Config{Container: "api-1", Root: "/app", X: d}
	for _, m := range mod {
		m(&cfg)
	}
	h, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return h, d, c
}

func TestNewInspectsTheContainer(t *testing.T) {
	h, d, _ := newHost(t)
	if !h.Online() || h.Label() != "api-1" || h.ID() != "container:default/api-1:/app" {
		t.Fatalf("host = online %v label %q id %q", h.Online(), h.Label(), h.ID())
	}
	calls := d.Calls()
	if len(calls) == 0 || calls[0].Args[0] != "inspect" || calls[0].Context != "" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestNewPassesTheContextAsAFlag(t *testing.T) {
	h, d, _ := newHost(t, func(c *Config) { c.Context = "prod-ssh" })
	if h.ID() != "container:prod-ssh/api-1:/app" {
		t.Fatalf("id = %q", h.ID())
	}
	for _, c := range d.Calls() {
		if c.Context != "prod-ssh" {
			t.Fatalf("call without the context: %+v", c)
		}
	}
}

func TestNewPodmanUsesConnection(t *testing.T) {
	h, _, _ := newHost(t, func(c *Config) { c.Binary = "/usr/bin/podman"; c.Context = "vm" })
	if got := h.argv("ps"); len(got) != 4 || got[1] != "--connection" || got[2] != "vm" {
		t.Fatalf("argv = %v", got)
	}
}

func TestNewRefusesStoppedOrUnknownContainer(t *testing.T) {
	d := fakedocker.New()
	c := d.Add(t, "api-1")
	c.Stop()
	if _, err := New(context.Background(), Config{Container: "api-1", Root: "/app", X: d}); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("stopped: err = %v", err)
	}
	if _, err := New(context.Background(), Config{Container: "db", Root: "/app", X: d}); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("unknown: err = %v", err)
	}
}

func TestNewRefusesBadInput(t *testing.T) {
	d := fakedocker.New()
	d.Add(t, "api-1")
	bad := []Config{
		{Container: "", Root: "/app"},
		{Container: "-it", Root: "/app"},
		{Container: "a:b", Root: "/app"},
		{Container: "api-1", Root: "app"},
		{Container: "api-1", Root: "/app", Context: "--host=x"},
	}
	for _, cfg := range bad {
		cfg.X = d
		if _, err := New(context.Background(), cfg); err == nil {
			t.Errorf("New(%+v) accepted", cfg)
		}
	}
}

func TestOnlineFollowsTheContainer(t *testing.T) {
	h, _, c := newHost(t)
	c.Stop()
	if _, err := h.FS().ReadDir(t.Context(), "."); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("ReadDir on a stopped container = %v", err)
	}
	if h.Online() {
		t.Fatal("still online after a failed call")
	}
	if h.CheckOnline(t.Context()) {
		t.Fatal("CheckOnline true while stopped")
	}
	c.Start()
	if !h.CheckOnline(t.Context()) || !h.Online() {
		t.Fatal("CheckOnline false after start")
	}
}

func TestCheckOnlineEngineGone(t *testing.T) {
	h, d, _ := newHost(t)
	h.cfg.Timeout = 20 * time.Millisecond
	d.SetDelay("inspect", time.Minute)
	if h.CheckOnline(t.Context()) {
		t.Fatal("online while the engine hangs")
	}
}
