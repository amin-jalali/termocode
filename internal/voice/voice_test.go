package voice

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoadMissingFileIsNotSetUp(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if s.Configured() {
		t.Fatalf("no voice.json, but configured: %+v", s)
	}
}

func TestLoadReadsCommandWithComments(t *testing.T) {
	dir := t.TempDir()
	body := `{
  // the program that listens
  "command": ["my-dictate", "--lang", "{lang}", "--max", "{seconds}"],
  "language": "de",
  "max_seconds": 20
}`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Configured() {
		t.Fatal("not configured")
	}
	got := strings.Join(s.Argv(), " ")
	if got != "my-dictate --lang de --max 20" {
		t.Fatalf("argv %q", got)
	}
}

func TestLoadBadJSONSaysWhere(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(`{"command": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), FileName) {
		t.Fatalf("err %v", err)
	}
}

func TestSecondsDefaultAndCap(t *testing.T) {
	if got := (Settings{}).Seconds(); got != DefaultMaxSeconds {
		t.Fatalf("default %d", got)
	}
	if got := (Settings{MaxSeconds: 99999}).Seconds(); got != MaxMaxSeconds {
		t.Fatalf("cap %d", got)
	}
	if got := (Settings{MaxSeconds: -3}).Seconds(); got != DefaultMaxSeconds {
		t.Fatalf("negative %d", got)
	}
}

func TestArgvAutoLanguage(t *testing.T) {
	s := Settings{Command: []string{"x", "{lang}"}}
	if got := s.Argv(); got[1] != "auto" {
		t.Fatalf("argv %q", got)
	}
}

func TestEmptyProgramIsNotConfigured(t *testing.T) {
	if (Settings{Command: []string{"  "}}).Configured() {
		t.Fatal("a blank program counts as set up")
	}
}

func TestCleanDropsTimestampsAndMarkers(t *testing.T) {
	in := "[00:00:00.000 --> 00:00:02.000]  Fix the\n[BLANK_AUDIO]\n[00:00:02.000 --> 00:00:04.000]   login test.\r\n(silence)\x1b[0m\n"
	if got := Clean(in); got != "Fix the login test." {
		t.Fatalf("clean %q", got)
	}
}

func TestCleanKeepsWordsInBrackets(t *testing.T) {
	if got := Clean("rename [config] to settings"); got != "rename [config] to settings" {
		t.Fatalf("clean %q", got)
	}
}

func TestCleanCapsLength(t *testing.T) {
	long := strings.Repeat("word ", MaxTranscript)
	if got := Clean(long); len([]rune(got)) > MaxTranscript {
		t.Fatalf("len %d", len([]rune(got)))
	}
}

func TestDefaultBodyLoadsAsNotSetUp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(DefaultBody()), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Configured() {
		t.Fatal("the template must not run anything until the user fills it in")
	}
}

func unixOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
}

func TestStartReadsTheWords(t *testing.T) {
	unixOnly(t)
	sess, err := Start(context.Background(), Settings{Command: []string{"sh", "-c", "echo '[00:00:00.000 --> 00:00:01.000] hello there'"}})
	if err != nil {
		t.Fatal(err)
	}
	r := sess.Wait()
	if r.Err != nil || r.Canceled || r.Text != "hello there" {
		t.Fatalf("result %+v", r)
	}
}

func TestStopLetsTheProgramFinish(t *testing.T) {
	unixOnly(t)
	// The program prints its words when it is asked to stop.
	script := `trap 'echo stopped words; exit 0' INT; echo ready; while :; do sleep 0.05; done`
	sess, err := Start(context.Background(), Settings{Command: []string{"sh", "-c", script}})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return strings.Contains(sess.out.String(), "ready") })
	sess.Stop()
	r := sess.Wait()
	if r.Err != nil || r.Canceled || !strings.Contains(r.Text, "stopped words") {
		t.Fatalf("result %+v", r)
	}
}

func TestCancelDropsTheWords(t *testing.T) {
	unixOnly(t)
	sess, err := Start(context.Background(), Settings{Command: []string{"sh", "-c", "echo secret; exec sleep 30"}})
	if err != nil {
		t.Fatal(err)
	}
	sess.Cancel()
	r := sess.Wait()
	if !r.Canceled || r.Text != "" {
		t.Fatalf("result %+v", r)
	}
}

func TestFailingProgramShowsItsError(t *testing.T) {
	unixOnly(t)
	sess, err := Start(context.Background(), Settings{Command: []string{"sh", "-c", "echo no microphone >&2; exit 3"}})
	if err != nil {
		t.Fatal(err)
	}
	r := sess.Wait()
	if r.Err == nil || !strings.Contains(r.Err.Error(), "no microphone") {
		t.Fatalf("result %+v", r)
	}
}

func TestStartNotConfigured(t *testing.T) {
	if _, err := Start(context.Background(), Settings{}); err == nil {
		t.Fatal("started without a command")
	}
}

func TestMissingProgramFailsToStart(t *testing.T) {
	if _, err := Start(context.Background(), Settings{Command: []string{"termocode-no-such-dictate-program"}}); err == nil {
		t.Fatal("started a missing program")
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
