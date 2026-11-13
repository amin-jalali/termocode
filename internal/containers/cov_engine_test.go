package containers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

func argvLog(t *testing.T, f *servicestest.Fake) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(f.Data, "argv.log"))
	return string(b)
}

// Each engine call runs the engine with the argv the pure builder made
// (the fake docker logs it), and the engine's failure comes back in its
// own words.
func TestEngineCallsRunTheirArgv(t *testing.T) {
	f := servicestest.New(t)
	e := fakeEngine(f)
	ctx := context.Background()
	host := t.TempDir()
	calls := []struct {
		name string
		run  func() error
		want string
	}{
		{"prune build cache", func() error { return e.PruneBuildCache(ctx, true) }, "prune"},
		{"create volume", func() error { return e.CreateVolumeWith(ctx, VolumeCreateOptions{Name: "v1", Driver: "local"}) }, "volume create"},
		{"create network", func() error { return e.CreateNetworkWith(ctx, NetworkCreateOptions{Name: "n1", Driver: "bridge"}) }, "network create"},
		{"backup", func() error { return e.BackupVolume(ctx, "v1", host, "b.tgz") }, "tar -czf /backup/b.tgz"},
		{"restore", func() error { return e.RestoreVolume(ctx, "v1", host, "b.tgz") }, "tar -xzf /backup/b.tgz"},
		{"clone", func() error { return e.CloneVolume(ctx, "v1", "v2") }, "cp -a /from/. /to/"},
	}
	for _, c := range calls {
		if err := c.run(); err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !strings.Contains(argvLog(t, f), c.want) {
			t.Errorf("%s: the engine did not run %q; log:\n%s", c.name, c.want, argvLog(t, f))
		}
	}
	// Bad input never reaches the engine.
	if err := e.CloneVolume(ctx, "v1", "v1"); err == nil {
		t.Error("cloning onto itself must be refused")
	}
	if err := e.BackupVolume(ctx, "-bad", host, "b.tgz"); err == nil {
		t.Error("a bad volume name must be refused")
	}
	if err := e.RestoreVolume(ctx, "-bad", host, "b.tgz"); err == nil {
		t.Error("a bad volume name must be refused")
	}
	if err := e.CreateVolumeWith(ctx, VolumeCreateOptions{Name: "-bad"}); err == nil {
		t.Error("a bad volume name must be refused")
	}
	if err := e.CreateNetworkWith(ctx, NetworkCreateOptions{Name: "-bad"}); err == nil {
		t.Error("a bad network name must be refused")
	}
	if _, err := e.BrowseVolume(ctx, "-bad", "/"); err == nil {
		t.Error("a bad volume name must be refused")
	}
	f.Set(t, "all.err", "boom")
	f.Set(t, "all.code", "3")
	for name, call := range map[string]func() error{
		"prune":    func() error { return e.PruneBuildCache(ctx, false) },
		"volume":   func() error { return e.CreateVolumeWith(ctx, VolumeCreateOptions{Name: "v9"}) },
		"network":  func() error { return e.CreateNetworkWith(ctx, NetworkCreateOptions{Name: "n9"}) },
		"backup":   func() error { return e.BackupVolume(ctx, "v1", host, "b.tgz") },
		"restore":  func() error { return e.RestoreVolume(ctx, "v1", host, "b.tgz") },
		"clone":    func() error { return e.CloneVolume(ctx, "v1", "v2") },
		"browse":   func() error { _, err := e.BrowseVolume(ctx, "v1", "/"); return err },
		"history":  func() error { _, err := e.BuildHistory(ctx); return err },
		"builders": func() error { _, err := e.Builders(ctx); return err },
		"cache":    func() error { _, err := e.BuildCache(ctx); return err },
		"top":      func() error { _, err := e.Top(ctx, "abc"); return err },
		"raw":      func() error { _, err := e.InspectRaw(ctx, "abc"); return err },
		"sized":    func() error { _, err := e.ListContainersSized(ctx, true); return err },
		"info":     func() error { _, err := e.Info(ctx); return err },
		"disk":     func() error { _, err := e.DiskSummary(ctx); return err },
	} {
		if err := call(); err == nil {
			t.Errorf("%s with a failing engine: want an error", name)
		}
	}
}

func TestBrowseVolumeReadsTheListing(t *testing.T) {
	f := servicestest.New(t)
	e := fakeEngine(f)
	// The fake answers `run` with nothing: an empty volume.
	got, err := e.BrowseVolume(context.Background(), "v1", "/sub/../x")
	if err != nil || len(got) != 0 {
		t.Errorf("BrowseVolume = %v, %v", got, err)
	}
	if !strings.Contains(argvLog(t, f), "/v/x") {
		t.Errorf("the path is cleaned inside the volume; log:\n%s", argvLog(t, f))
	}
}

func TestBuildListsNeedBuildx(t *testing.T) {
	f := servicestest.New(t)
	e := fakeEngine(f)
	e.Kind = "podman"
	if _, err := e.BuildHistory(context.Background()); err == nil || !strings.Contains(err.Error(), "buildx") {
		t.Errorf("BuildHistory on podman = %v", err)
	}
	if _, err := e.Builders(context.Background()); err == nil || !strings.Contains(err.Error(), "buildx") {
		t.Errorf("Builders on podman = %v", err)
	}
	if _, err := e.InspectRaw(context.Background(), "--bad"); err == nil {
		t.Error("a bad container id must be refused")
	}
	if _, _, err := e.RecreatePlan("--bad", RunOptions{Image: "x"}); err == nil {
		t.Error("a bad container id must be refused")
	}
}
