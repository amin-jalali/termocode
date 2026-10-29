//go:build unix

package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// shareTestOptions run the broker as this test binary in a temp folder.
func shareTestOptions(t *testing.T) *ShareOptions {
	return &ShareOptions{Exe: os.Args[0], Args: []string{"-test.run=^$"},
		Dir: filepath.Join(shortTemp(t), "mcp"), Grace: 200 * time.Millisecond}
}

func sharedManager(t *testing.T, e *managerEnv, share *ShareOptions, x Expander) *Manager {
	t.Helper()
	m := NewManager(ManagerOptions{
		UserPath: e.user, ProjectPath: e.project, CacheDir: e.cache,
		Expander:    func() Expander { return x },
		Roots:       func() []string { return []string{e.dir} },
		CallTimeout: 5 * time.Second,
		Share:       share,
	})
	t.Cleanup(m.Close)
	return m
}

func shareStatus(m *Manager, name string) Status {
	for _, s := range m.Servers() {
		if s.Name == name {
			return s
		}
	}
	return Status{}
}

func callEcho(t *testing.T, m *Manager, text string) {
	t.Helper()
	res, err := m.Call(context.Background(), nil, "fake", "echo", map[string]any{"text": text})
	if err != nil || res.Text() != "echo: "+text {
		t.Fatalf("echo: %q %v", res.Text(), err)
	}
}

func TestManagerSharesOneServerAcrossSessions(t *testing.T) {
	e := newManagerEnv(t)
	marker := filepath.Join(e.dir, "running")
	e.write(t, e.user, map[string]any{"fake": map[string]string{"FAKE_MARKER": marker}})
	share := shareTestOptions(t)
	a := sharedManager(t, e, share, Expander{Env: os.LookupEnv})
	b := sharedManager(t, e, share, Expander{Env: os.LookupEnv})
	callEcho(t, a, "one")
	callEcho(t, b, "two")
	if n := countLines(e.log, "initialize"); n != 1 {
		t.Fatalf("%d servers started, want 1", n)
	}
	waitFor(t, "2 sessions", func() bool { return shareStatus(a, "fake").Sessions == 2 && shareStatus(b, "fake").Sessions == 2 })
	sa, sb := shareStatus(a, "fake"), shareStatus(b, "fake")
	if !sa.Shared || sa.Pid == 0 || sa.Pid != sb.Pid || sa.State != StateRunning {
		t.Fatalf("status %+v / %+v", sa, sb)
	}
	// One session leaves: the other keeps the same server.
	a.Close()
	waitFor(t, "1 session", func() bool { return shareStatus(b, "fake").Sessions == 1 })
	callEcho(t, b, "three")
	if n := countLines(e.log, "initialize"); n != 1 {
		t.Fatalf("the server restarted (%d starts)", n)
	}
	// The last one leaves: the server stops after the grace.
	b.Close()
	waitFor(t, "the server to stop", func() bool {
		_, err := os.Stat(marker)
		return os.IsNotExist(err)
	})
}

func TestManagerStopSharedStopsItForEverySession(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	share := shareTestOptions(t)
	a := sharedManager(t, e, share, Expander{Env: os.LookupEnv})
	b := sharedManager(t, e, share, Expander{Env: os.LookupEnv})
	callEcho(t, a, "x")
	callEcho(t, b, "y")
	a.Stop("fake")
	waitFor(t, "the other session to see the stop", func() bool { return shareStatus(b, "fake").State == StateStopped })
	callEcho(t, b, "again") // starts it again, shared
	if n := countLines(e.log, "initialize"); n != 2 {
		t.Fatalf("%d starts, want 2", n)
	}
	if !shareStatus(b, "fake").Shared {
		t.Fatal("the restarted server is not shared")
	}
}

func TestManagerOtherConfigOtherServer(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{"X": "1"}})
	share := shareTestOptions(t)
	a := sharedManager(t, e, share, Expander{Env: os.LookupEnv})
	callEcho(t, a, "x")
	e2 := newManagerEnv(t)
	e2.log = e.log
	e2.write(t, e2.user, map[string]any{"fake": map[string]string{"X": "2"}})
	b := sharedManager(t, e2, share, Expander{Env: os.LookupEnv})
	callEcho(t, b, "y")
	if n := countLines(e.log, "initialize"); n != 2 {
		t.Fatalf("%d servers, want 2 (other env)", n)
	}
	if shareStatus(a, "fake").Pid == shareStatus(b, "fake").Pid {
		t.Fatal("two configs share one process")
	}
}

func TestManagerShareFallsBackToOwnProcess(t *testing.T) {
	for name, share := range map[string]*ShareOptions{
		"folder too long": {Exe: os.Args[0], Args: []string{"-test.run=^$"}, Dir: filepath.Join(t.TempDir(), strings.Repeat("d", 90))},
		"no broker":       {Exe: filepath.Join(t.TempDir(), "no-such-termocode"), Dir: filepath.Join(shortTemp(t), "mcp")},
		"broker fails":    {Exe: "/bin/false", Dir: filepath.Join(shortTemp(t), "mcp")},
	} {
		t.Run(name, func(t *testing.T) {
			e := newManagerEnv(t)
			e.write(t, e.user, map[string]any{"fake": map[string]string{}})
			m := sharedManager(t, e, share, Expander{Env: os.LookupEnv})
			callEcho(t, m, "x")
			st := shareStatus(m, "fake")
			if st.Shared || st.Pid == 0 {
				t.Fatalf("status %+v, want its own process", st)
			}
		})
	}
}

func TestManagerShareServerErrorIsNotRetriedAlone(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]any{"command": "termocode-no-such-server"}})
	var lines []string
	m := NewManager(ManagerOptions{UserPath: e.user, ProjectPath: e.project, Share: shareTestOptions(t),
		Log: func(_, l string) { lines = append(lines, l) }})
	defer m.Close()
	err := m.Connect(context.Background(), nil, "fake")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err %v", err)
	}
	for _, l := range lines {
		if strings.Contains(l, "not shared") {
			t.Fatalf("fell back after a server error: %q", l)
		}
	}
}

func TestManagerShareKeepsSecretsOffDiskAndArgv(t *testing.T) {
	const secret = "sup3r-s3cret-value"
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{"TOKEN": "${secret:TOKEN}"}})
	share := shareTestOptions(t)
	x := Expander{Env: os.LookupEnv, SecretLookup: func(n string) (string, bool) { return secret, n == "TOKEN" }}
	m := sharedManager(t, e, share, x)
	callEcho(t, m, "x")
	if !shareStatus(m, "fake").Shared {
		t.Fatal("not shared")
	}
	_ = filepath.Walk(share.Dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && strings.Contains(p, secret) {
			t.Errorf("secret in a file name: %s", p)
		}
		if err == nil && fi.Mode().IsRegular() {
			b, _ := os.ReadFile(p)
			if strings.Contains(string(b), secret) {
				t.Errorf("secret written to %s", p)
			}
		}
		return nil
	})
	cmd := shareCommand(share, filepath.Join(share.Dir, "k.sock"))
	if strings.Contains(strings.Join(cmd.Args, " "), secret) || strings.Contains(strings.Join(cmd.Env, "\n"), secret) {
		t.Fatal("secret in the broker's argv or env")
	}
}
