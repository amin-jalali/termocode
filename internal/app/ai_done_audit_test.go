package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
)

// Before the AI says done it audits the work: each thing the owner asked,
// next to the proof for it; no proof or a doubt is not done, and green
// tests count only when they test the ask. The same words are in the
// system prompt (chat and CLI steps), the keep-going push and the
// until-done verify step, so "done" means one thing everywhere.
func TestAIDoneAuditEverywhere(t *testing.T) {
	wants := []string{"list each thing the owner asked", "the proof", "not sure", "means not done", "Green tests are proof only when they test what was asked"}
	for what, text := range map[string]string{
		"chat":              aiWithToolGuide("", nil, false),
		"chat, tools":       aiWithToolGuide("", aiDocTools("read_file", "tests_run"), false),
		"cli":               aiCLIInstructions("/p", nil, false),
		"keep going":        aiKeepGoingText(2),
		"until-done verify": agentrun.VerifyPrompt,
	} {
		for _, w := range wants {
			if !strings.Contains(text, w) {
				t.Errorf("%s misses %q:\n%s", what, w, text)
			}
		}
	}
	// the verify step still ends with its verdict format
	for _, w := range []string{"EVIDENCE:", "VERDICT: PASS"} {
		if !strings.Contains(agentrun.VerifyPrompt, w) {
			t.Errorf("verify prompt lost %q", w)
		}
	}
}

// The step-limit texts: the push (no open todos) carries the audit too,
// and the note no longer tells the owner to reply "continue" as the only
// way on: keep going (on by default) goes on by itself, "continue" is
// for when it is off.
func TestAIStepLimitTexts(t *testing.T) {
	if !strings.Contains(aiStepLimitPush, agentrun.DoneAudit) {
		t.Errorf("step-limit push lacks the audit: %q", aiStepLimitPush)
	}
	note := ai.StepLimitText(100)
	for _, w := range []string{"step limit (100 tool steps)", "goes on by itself", "keep going is off", `"continue"`} {
		if !strings.Contains(note, w) {
			t.Errorf("step-limit note misses %q: %q", w, note)
		}
	}
}
