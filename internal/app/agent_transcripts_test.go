package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/agentlog/agentlogtest"
)

// transcriptHome makes a fake HOME with the fixture sessions, points
// the app at it and records every file or folder agentlog opens.
func transcriptHome(t *testing.T) (home string, opened func() []string) {
	t.Helper()
	home = agentlogtest.FakeHome(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	var mu sync.Mutex
	var paths []string
	old := agentlog.OnOpen
	agentlog.OnOpen = func(p string) { mu.Lock(); paths = append(paths, p); mu.Unlock() }
	t.Cleanup(func() { agentlog.OnOpen = old })
	return home, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), paths...) }
}

// setAgentTranscripts writes agent_transcripts to config.json.
func setAgentTranscripts(t *testing.T, v string) {
	t.Helper()
	path, err := settingsConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	if err := os.WriteFile(path, []byte(`{"agent_transcripts": "`+v+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Spec (AGX-7, owner D1): reading the CLIs' session files is opt-in:
// agent_transcripts is off by default, and while it is off no file or
// folder under ~/.claude or ~/.codex is opened. With "read" the session
// file of a folder is found. The AI can never turn it on, and the
// setting is documented.
func TestAgentTranscriptsOffOpensNothing(t *testing.T) {
	home, opened := transcriptHome(t)
	if agentTranscriptsOn() {
		t.Fatal("agent_transcripts is on by default")
	}
	for _, cli := range []agentlog.CLI{agentlog.Claude, agentlog.Codex} {
		if _, err := agentTranscriptFind(cli, agentlogtest.Folder, time.Time{}); !errors.Is(err, errAgentTranscriptsOff) {
			t.Errorf("%s off: err %v", cli, err)
		}
	}
	if got := opened(); len(got) != 0 {
		t.Fatalf("opened with the setting off: %v", got)
	}
	setAgentTranscripts(t, "off")
	if agentTranscriptsOn() {
		t.Error("off is on")
	}
	setAgentTranscripts(t, "read")
	if !agentTranscriptsOn() {
		t.Fatal("read is not on")
	}
	for _, cli := range []agentlog.CLI{agentlog.Claude, agentlog.Codex} {
		ref, err := agentTranscriptFind(cli, agentlogtest.Folder, time.Time{})
		if err != nil || !strings.HasPrefix(ref.Path, home) {
			t.Errorf("%s on: %+v %v", cli, ref, err)
		}
	}
	if len(opened()) == 0 {
		t.Error("the observer saw no open (the off check above proves nothing)")
	}
	if p, ok := aiSettingPolicies["agent_transcripts"]; !ok || p.Gate != aiGateNever {
		t.Errorf("agent_transcripts policy %+v %v", p, ok)
	}
	found := false
	for _, e := range SettingsCatalog() {
		if e.Key == "agent_transcripts" && e.Default == "off" {
			found = true
		}
	}
	if !found {
		t.Error("agent_transcripts is not documented with default off")
	}
}
