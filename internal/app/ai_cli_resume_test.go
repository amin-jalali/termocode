//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
)

// A chat whose profile is one Codex step: each turn starts the CLI
// again, so the next turn must go on in the CLI's own session (or carry
// the earlier turns). The CLI here is a small shell script on PATH (never
// the real codex): each call writes its argv and stdin to
// $FAKE_OUT/argv.<n> and stdin.<n>; it answers in codex's --json events
// with the thread id thread-1. With FAKE_RESUME_FAIL set, a resume fails
// the way codex does when it cannot find the session.
const fakeCodexChat = `#!/bin/sh
n=$(cat "$FAKE_OUT/n" 2>/dev/null || echo 0)
n=$((n+1))
echo $n > "$FAKE_OUT/n"
printf '%s\n' "$@" > "$FAKE_OUT/argv.$n"
cat > "$FAKE_OUT/stdin.$n"
case " $* " in
*" resume "*)
  if [ -n "$FAKE_RESUME_FAIL" ]; then
    echo '{"type":"error","message":"no rollout found for thread id thread-1"}'
    exit 1
  fi;;
esac
text="Here they are."
[ "$n" = 1 ] && text="There are 20 commits."
echo '{"type":"thread.started","thread_id":"thread-1"}'
echo '{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"'"$text"'"}}'
echo '{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":0,"output_tokens":5}}'
`

type fakeCodexBin struct{ out string }

func newFakeCodexBin(t *testing.T) *fakeCodexBin {
	t.Helper()
	bin, out := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(fakeCodexChat), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_OUT", out)
	old, oldLook := aiCLIRun, agentLookPath
	t.Cleanup(func() { aiCLIRun, agentLookPath = old, oldLook })
	aiCLIRun = clistep.Run // the real step code, the fake program
	agentLookPath = func(b string) (string, error) { return filepath.Join(bin, b), nil }
	return &fakeCodexBin{out: out}
}

func (f *fakeCodexBin) calls(t *testing.T) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.out, "n"))
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

func (f *fakeCodexBin) read(t *testing.T, name string, n int) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.out, name+"."+strconv.Itoa(n)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func gptProfile(model string) ai.Profile {
	return ai.Profile{Name: "gpt", Title: "GPT", Source: ai.ProfileSourceUser,
		Steps: []ai.ProfileStep{{ID: "answer", Title: "Answer", CLI: ai.CLICodex, Model: model,
			Tools: agentdag.ToolRule{Preset: agentdag.PresetNone}}}}
}

func gptChatModel(t *testing.T) *Model {
	t.Helper()
	m, _ := cliRunModel(t)
	m.ai.profFake = []ai.Profile{gptProfile("")}
	m.ai.reloadProfiles()
	m.ai.profile = "gpt"
	m.output = newOutputStore()
	return m
}

func twoTurns(t *testing.T, m *Model) {
	t.Helper()
	driveAIChat(t, m, m.sendAIChat("how many commits?"), nil)
	if rm := aiRunMsg(t, m); rm.Run.State != ai.RunStateDone || rm.Text != "There are 20 commits." {
		t.Fatalf("turn 1 run %+v text %q", rm.Run, rm.Text)
	}
	driveAIChat(t, m, m.sendAIChat("show them"), nil)
	if rm := aiRunMsg(t, m); rm.Run.State != ai.RunStateDone || rm.Text != "Here they are." {
		t.Fatalf("turn 2 run %+v text %q", rm.Run, rm.Text)
	}
}

func hasArgs(argv string, seq ...string) bool {
	return strings.Contains("\n"+argv, "\n"+strings.Join(seq, "\n")+"\n")
}

// The owner's chat: "how many commits?" → "20 commits", then "show
// them": turn 2 goes on in turn 1's Codex session, so Codex knows what
// "them" is.
func TestAICLIStepResumesChatSession(t *testing.T) {
	f := newFakeCodexBin(t)
	m := gptChatModel(t)
	twoTurns(t, m)
	if n := f.calls(t); n != 2 {
		t.Fatalf("codex ran %d times, want 2", n)
	}
	if a := f.read(t, "argv", 1); strings.Contains(a, "resume") {
		t.Errorf("turn 1 resumed:\n%s", a)
	}
	if a := f.read(t, "argv", 2); !hasArgs(a, "resume", "thread-1", "-") {
		t.Errorf("turn 2 does not resume thread-1:\n%s", a)
	}
	if in := f.read(t, "stdin", 2); !strings.Contains(in, "show them") {
		t.Errorf("turn 2 stdin misses the message:\n%s", in)
	}
}

// The resume fails (the CLI lost its session): the turn runs once more
// in a new session, with the recent turns written before the message.
func TestAICLIStepResumeFailsCarriesTranscript(t *testing.T) {
	f := newFakeCodexBin(t)
	t.Setenv("FAKE_RESUME_FAIL", "1")
	m := gptChatModel(t)
	twoTurns(t, m)
	if n := f.calls(t); n != 3 {
		t.Fatalf("codex ran %d times, want 3 (turn 1, the failed resume, the new session)", n)
	}
	a, in := f.read(t, "argv", 3), f.read(t, "stdin", 3)
	if strings.Contains(a, "resume") {
		t.Errorf("the retry resumed again:\n%s", a)
	}
	assertTranscript(t, in)
}

// No resume when it is turned off, or when the step's model changed
// since the session started: the recent turns go in the prompt instead.
func TestAICLIStepTranscriptWithoutSession(t *testing.T) {
	for _, name := range []string{"off", "model changed"} {
		t.Run(name, func(t *testing.T) {
			f := newFakeCodexBin(t)
			m := gptChatModel(t)
			if name == "off" {
				m.ai.settings.CLIResume = "off"
			}
			driveAIChat(t, m, m.sendAIChat("how many commits?"), nil)
			if name == "model changed" {
				m.ai.profFake = []ai.Profile{gptProfile("gpt-5-codex")}
				m.ai.reloadProfiles()
			}
			driveAIChat(t, m, m.sendAIChat("show them"), nil)
			if n := f.calls(t); n != 2 {
				t.Fatalf("codex ran %d times, want 2", n)
			}
			if a := f.read(t, "argv", 2); strings.Contains(a, "resume") {
				t.Errorf("turn 2 resumed:\n%s", a)
			}
			assertTranscript(t, f.read(t, "stdin", 2))
		})
	}
}

func assertTranscript(t *testing.T, in string) {
	t.Helper()
	q, a, now := strings.Index(in, "how many commits?"), strings.Index(in, "There are 20 commits."), strings.LastIndex(in, "show them")
	if q < 0 || a < 0 || now < 0 || q > a || a > now {
		t.Errorf("the prompt does not carry turn 1 before the new message:\n%s", in)
	}
}
