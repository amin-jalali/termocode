package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentrun"
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
