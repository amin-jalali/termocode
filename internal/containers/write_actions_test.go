package containers

import (
	"context"
	"reflect"
	"testing"

	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// CTX-19: kill, rename, restart policy, resource limits and network
// connect/disconnect run the engine with exactly these args (fake docker).
func TestContainerWriteActionsArgv(t *testing.T) {
	f := servicestest.New(t)
	e := fakeEngine(f)
	ctx := context.Background()
	steps := []func() error{
		func() error { return e.Kill(ctx, "abc", "") },
		func() error { return e.Kill(ctx, "abc", "SIGHUP") },
		func() error { return e.Rename(ctx, "abc", "web-2") },
		func() error { return e.SetRestartPolicy(ctx, "abc", "on-failure:3") },
		func() error { return e.SetRestartPolicy(ctx, "abc", "unless-stopped") },
		func() error { return e.SetResources(ctx, "abc", Resources{CPUs: "1.5", Memory: "512m"}) },
		func() error { return e.SetResources(ctx, "abc", Resources{Memory: "1g", MemorySwap: "2g"}) },
		func() error { return e.NetworkConnect(ctx, "backend", "abc") },
		func() error { return e.NetworkDisconnect(ctx, "backend", "abc") },
	}
	for i, s := range steps {
		if err := s(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	want := []string{
		"kill abc",
		"kill --signal SIGHUP abc",
		"rename abc web-2",
		"update --restart on-failure:3 abc",
		"update --restart unless-stopped abc",
		"update --cpus 1.5 --memory 512m abc",
		"update --memory 1g --memory-swap 2g abc",
		"network connect backend abc",
		"network disconnect backend abc",
	}
	if got := f.Calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls\n%q\nwant\n%q", got, want)
	}
}

// Bad input never reaches the engine (no flag injection, no odd values).
func TestContainerWriteActionsRefuseBadInput(t *testing.T) {
	f := servicestest.New(t)
	e := fakeEngine(f)
	ctx := context.Background()
	bad := map[string]func() error{
		"signal flag":   func() error { return e.Kill(ctx, "abc", "--all") },
		"signal junk":   func() error { return e.Kill(ctx, "abc", "HUP;rm") },
		"id flag":       func() error { return e.Kill(ctx, "-f", "") },
		"empty name":    func() error { return e.Rename(ctx, "abc", "") },
		"name flag":     func() error { return e.Rename(ctx, "abc", "--x") },
		"name chars":    func() error { return e.Rename(ctx, "abc", "a b") },
		"policy":        func() error { return e.SetRestartPolicy(ctx, "abc", "sometimes") },
		"policy count":  func() error { return e.SetRestartPolicy(ctx, "abc", "on-failure:x") },
		"no resources":  func() error { return e.SetResources(ctx, "abc", Resources{}) },
		"cpus":          func() error { return e.SetResources(ctx, "abc", Resources{CPUs: "lots"}) },
		"memory":        func() error { return e.SetResources(ctx, "abc", Resources{Memory: "12q"}) },
		"network flag":  func() error { return e.NetworkConnect(ctx, "--alias=x", "abc") },
		"network empty": func() error { return e.NetworkDisconnect(ctx, "", "abc") },
	}
	for name, call := range bad {
		if err := call(); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if calls := f.Calls(); len(calls) != 0 {
		t.Fatalf("engine called with bad input: %q", calls)
	}
}
