package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// The way of working in the system prompt (how to start, explore, stop,
// prove and end a turn) is built only from the tools a step has: a line
// that names a tool the step does not have would send the model to a
// tool that is not there.

// aiDocTools are tools by name (only the names matter to the prompt).
func aiDocTools(names ...string) []ai.Tool {
	out := make([]ai.Tool, len(names))
	for i, n := range names {
		out[i] = ai.Tool{Name: n}
	}
	return out
}

// aiDocLine is the doctrine line of text that holds key ("" for none).
func aiDocLine(text, key string) string {
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, key) {
			return l
		}
	}
	return ""
}

const (
	aiDocHead     = "How you work:"
	aiDocIntent   = "say in one line what the owner wants"
	aiDocExplore  = "Explore first, in parallel"
	aiDocStop     = "Stop searching"
	aiDocEvidence = "Never say done without proof"
	aiDocFailures = "fixes that fail"
	aiDocProgress = "one short line before you explore"
	aiDocTurnEnd  = "A question to the owner ends your turn"
	aiDocOpenFile = "COMPLETE updated file"
)

func TestAIDoctrineFollowsTheTools(t *testing.T) {
	chat := func(names ...string) string { return aiWithToolGuide("", aiDocTools(names...), false) }

	// no tools: only the lines every step can follow, and the open-file
	// reply (there is no edit tool to change it with)
	bare := chat()
	for _, w := range []string{aiDocHead, aiDocIntent, aiDocProgress, aiDocTurnEnd, aiDocOpenFile} {
		if !strings.Contains(bare, w) {
			t.Errorf("no tools: misses %q:\n%s", w, bare)
		}
	}
	for _, w := range []string{aiDocExplore, aiDocStop, aiDocEvidence, aiDocFailures} {
		if strings.Contains(bare, w) {
			t.Errorf("no tools: has %q, but no tool for it:\n%s", w, bare)
		}
	}

	// read tools: explore in parallel with them, and stop in time
	reads := chat("read_file", "search", "list_dir")
	if l := aiDocLine(reads, aiDocExplore); !strings.Contains(l, "read_file") || !strings.Contains(l, "search") || strings.Contains(l, "glob") {
		t.Errorf("explore line does not name the read tools present (and only them): %q", l)
	}
	if strings.Contains(aiDocLine(reads, aiDocExplore), "agent_run_start") {
		t.Errorf("explore line names agent_run_start, which the step lacks")
	}
	if !strings.Contains(reads, aiDocStop) {
		t.Errorf("read tools but no stop rule:\n%s", reads)
	}
	if l := aiDocLine(chat("read_file", "agent_run_start"), aiDocExplore); !strings.Contains(l, "agent_run_start") || !strings.Contains(l, "read-only") {
		t.Errorf("with agent_run_start the explore line should offer read-only runs: %q", l)
	}

	// proof: names only the check tools present
	ev := aiDocLine(chat("diagnostics", "tests_run", "tests_results"), aiDocEvidence)
	if !strings.Contains(ev, "diagnostics") || !strings.Contains(ev, "tests_run") || strings.Contains(ev, "run_command") {
		t.Errorf("evidence line does not follow the tools: %q", ev)
	}
	if ev := aiDocLine(chat("run_command"), aiDocEvidence); !strings.Contains(ev, "run_command") {
		t.Errorf("evidence line misses run_command: %q", ev)
	}

	// edit tools: the failure rule; the file is changed with them, not
	// pasted whole in a code block
	edit := chat("read_file", "edit_file")
	if !strings.Contains(edit, aiDocFailures) {
		t.Errorf("edit tools but no failure rule:\n%s", edit)
	}
	if strings.Contains(edit, aiDocOpenFile) {
		t.Errorf("edit tools, still asks for the complete file in a code block:\n%s", edit)
	}

	// the chat-wide rules no longer carry the open-file reply for everyone
	if strings.Contains(aiChatSystemPrompt, aiDocOpenFile) {
		t.Errorf("aiChatSystemPrompt still tells every turn to paste the complete file")
	}
}

// A CLI step's prompt carries the tool guide from MCP but not the
// doctrine: that sits once in its instructions, where its own file and
// shell tools count.
func TestAICLIInstructionsHaveTheDoctrine(t *testing.T) {
	if p := aiWithToolGuide("", aiDocTools("read_file"), true); strings.Contains(p, aiDocHead) {
		t.Errorf("CLI prompt repeats the doctrine:\n%s", p)
	}
	ro := aiCLIInstructions("/p", nil, false)
	for _, w := range []string{aiDocHead, aiDocIntent, aiDocExplore, aiDocStop, aiDocEvidence, aiDocProgress, aiDocTurnEnd} {
		if !strings.Contains(ro, w) {
			t.Errorf("read-only CLI: misses %q:\n%s", w, ro)
		}
	}
	for _, w := range []string{aiDocFailures, aiDocOpenFile} {
		if strings.Contains(ro, w) {
			t.Errorf("read-only CLI: has %q:\n%s", w, ro)
		}
	}
	if rw := aiCLIInstructions("/p", nil, true); !strings.Contains(rw, aiDocFailures) || strings.Contains(rw, aiDocOpenFile) {
		t.Errorf("writing CLI: wants the failure rule and no open-file reply:\n%s", rw)
	}
	if strings.Count(aiCLIInstructions("/p", aiDocTools("read_file", "tests_run"), true), aiDocHead) != 1 {
		t.Errorf("CLI instructions: the doctrine is not there exactly once")
	}
}

// The doctrine stays small: it rides on every request.
func TestAIDoctrineIsSmall(t *testing.T) {
	all := aiDocTools("read_file", "search", "glob", "list_dir", "agent_run_start", "diagnostics", "tests_run", "tasks_run", "run_command", "edit_file", "write_file")
	text := aiWithToolGuide("", all, false)
	i := strings.Index(text, aiDocHead)
	if i < 0 {
		t.Fatalf("no doctrine:\n%s", text)
	}
	doc := text[i:]
	if tok := len(doc) / 4; tok > 300 { // about 4 bytes a token
		t.Errorf("doctrine is about %d tokens, want <= 300:\n%s", tok, doc)
	}
}
