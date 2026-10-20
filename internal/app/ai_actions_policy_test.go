package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/picker"
)

// Every palette command must say what the AI may do with it. A new
// command without a policy fails here, so it is never exposed (or hidden)
// by accident.
func TestAIActionPolicyCoversEveryPaletteID(t *testing.T) {
	ids := map[string]bool{}
	for _, e := range PaletteCatalog() {
		ids[e.ID] = true
		if _, ok := aiActionPolicies[e.ID]; !ok {
			t.Errorf("palette id %q has no AI policy: add it to aiActionPolicies in ai_actions_policy.go "+
				"as auto (safe / UI only), confirm (+ class) or never (+ reason)", e.ID)
		}
	}
	for id := range aiActionPolicies {
		if !ids[id] {
			t.Errorf("aiActionPolicies has %q, which is not a palette id: remove it or fix the id", id)
		}
	}
	// Dynamic ids: extension commands and user commands.
	for _, id := range []string{extPaletteIDPfx + "some.command", "user-build"} {
		p, ok := aiActionPolicyFor(id)
		if !ok || p.Gate != aiGateConfirm || p.Class != ai.ClassRun {
			t.Errorf("%s: policy %+v ok=%v, want confirm/run", id, p, ok)
		}
	}
	if _, ok := aiActionPolicyFor("no-such-command"); ok {
		t.Error("an unknown id has a policy")
	}
}

// Each policy is well formed: never has a reason, confirm a known class,
// auto no class.
func TestAIActionPolicyWellFormed(t *testing.T) {
	classes := map[string]bool{"": true}
	for _, c := range ai.ToolClasses {
		classes[c] = true
	}
	for id, p := range aiActionPolicies {
		switch p.Gate {
		case aiGateAuto:
			if p.Class != "" {
				t.Errorf("%s: auto with class %q", id, p.Class)
			}
		case aiGateConfirm:
			if !classes[p.Class] {
				t.Errorf("%s: unknown class %q", id, p.Class)
			}
		case aiGateNever:
			if strings.TrimSpace(p.Reason) == "" {
				t.Errorf("%s: never without a reason", id)
			}
		default:
			t.Errorf("%s: unknown gate %q", id, p.Gate)
		}
	}
}

// Protect the AI action policies for sensitive operations.
func TestAIActionPolicyRules(t *testing.T) {
	for _, id := range []string{"quit", "reload-window", "detach-session", "reset-session",
		"settings-import", "settings-export", "ai-credentials", "ai-sign-in-claude", "ai-sign-out-claude",
		"ai-auto-approve", "ai-configure", "open-shell", "reveal-fs", "ext-manage-open-folder", "open-url", "git-clone"} {
		if p := aiActionPolicies[id]; p.Gate != aiGateNever {
			t.Errorf("%s: gate %q, want never", id, p.Gate)
		}
	}
	// Destructive: always ask, no "Always allow".
	for _, id := range []string{"git-discard", "git-discard-hunk", "git-push", "git-sync", "reload-buffer"} {
		if p := aiActionPolicies[id]; p.Gate != aiGateConfirm || p.Class != "" {
			t.Errorf("%s: %+v, want confirm with no class", id, p)
		}
	}
	for _, id := range []string{"containers-start", "containers-stop", "containers-restart", "containers-pause",
		"containers-compose-up", "containers-compose-stop", "containers-compose-restart",
		"containers-image-pull", "containers-image-push", "containers-image-tag", "containers-image-run",
		"containers-image-remove", "containers-image-prune", "containers-network-create", "containers-network-remove",
		"containers-network-prune", "containers-volume-create", "containers-volume-remove", "containers-volume-prune"} {
		if p := aiActionPolicies[id]; p.Gate != aiGateConfirm || p.Class != "" {
			t.Errorf("%s: %+v, want confirm with no auto-approve class", id, p)
		}
	}
	// The user's pending edits and transaction are never the AI's (DB-7).
	for _, id := range []string{"db-submit-changes", "db-revert-changes", "db-commit", "db-rollback", "db-transaction-mode"} {
		if p := aiActionPolicies[id]; p.Gate != aiGateNever {
			t.Errorf("%s: gate %q, want never", id, p.Gate)
		}
	}
	for _, id := range []string{"containers-remove", "containers-compose-down"} {
		if p := aiActionPolicies[id]; p.Gate != aiGateNever {
			t.Errorf("%s: gate %q, want never", id, p.Gate)
		}
	}
	for _, class := range ai.ToolClasses {
		if class == "containers" {
			t.Fatal("container operations must not have an auto-approve class")
		}
	}
	for id, want := range map[string]string{"save": ai.ClassFileWrite, "git-stage": ai.ClassGit,
		"tasks-build": ai.ClassRun, "testing-run-all": ai.ClassRun} {
		if p := aiActionPolicies[id]; p.Gate != aiGateConfirm || p.Class != want {
			t.Errorf("%s: %+v, want confirm/%s", id, p, want)
		}
	}
	for _, id := range []string{"fold-all", "toggle-explorer", "next-buffer", "git-refresh", "hover"} {
		if p := aiActionPolicies[id]; p.Gate != aiGateAuto {
			t.Errorf("%s: gate %q, want auto", id, p.Gate)
		}
	}
	for _, id := range []string{"containers-image-history", "containers-image-inspect", "containers-volume-disk-usage"} {
		if p := aiActionPolicies[id]; p.Gate != aiGateAuto {
			t.Errorf("%s: gate %q, want auto", id, p.Gate)
		}
	}
}

func TestAIContainerActionsRequireNamedTargets(t *testing.T) {
	tools := aiIDETools(t.TempDir(), func(context.Context, aiIDEFunc) (string, error) {
		return "", nil
	})
	var execute ai.Tool
	for _, tool := range tools {
		if tool.Name == "execute_action" {
			execute = tool
			break
		}
	}
	if execute.Name == "" {
		t.Fatal("execute_action tool missing")
	}
	if err := execute.Validate(map[string]any{"id": "containers-start"}); err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("missing target validation = %v", err)
	}
	args := map[string]any{"id": "containers-start", "target": "api-1"}
	if err := execute.Validate(args); err != nil {
		t.Fatalf("named target rejected: %v", err)
	}
	if got := execute.Describe(args); !strings.Contains(got, "api-1") {
		t.Errorf("confirmation description does not name the target: %q", got)
	}
}

// Every setting must have a policy too: exposed to settings_set or never
// (TestAISettingPolicyExposedOrNever checks the gates).
func TestAISettingPolicyCoversEverySetting(t *testing.T) {
	keys := map[string]bool{}
	for _, s := range SettingsCatalog() {
		keys[s.Key] = true
		p, ok := aiSettingPolicies[s.Key]
		if !ok {
			t.Errorf("setting %q has no AI policy: add it to aiSettingPolicies in ai_actions_policy.go", s.Key)
			continue
		}
		if p.Gate == aiGateNever && strings.TrimSpace(p.Reason) == "" {
			t.Errorf("setting %q: never without a reason", s.Key)
		}
	}
	for k := range aiSettingPolicies {
		if !keys[k] {
			t.Errorf("aiSettingPolicies has %q, which is not a setting", k)
		}
	}
	for _, k := range []string{"ai_auto_approve", "ai_provider", "ai_base_url", "ai_max_steps"} {
		if aiSettingPolicies[k].Gate != aiGateNever {
			t.Errorf("setting %s must be never", k)
		}
	}
}

func TestAIListActions(t *testing.T) {
	items := []picker.Item{
		{ID: "fold-all", Title: "View: Fold All"},
		{ID: "unfold-all", Title: "View: Unfold All"},
		{ID: "git-stage", Title: "Git: Stage Current File"},
		{ID: "quit", Title: "File: Quit"},
		{ID: "user-build", Title: "User: Build it"},
	}
	out := aiListActions(items, "fold", 40)
	if !strings.Contains(out, "fold-all: View: Fold All (View; auto)") || !strings.Contains(out, "unfold-all") {
		t.Errorf("fold:\n%s", out)
	}
	if strings.Contains(out, "git-stage") {
		t.Errorf("fold matched git-stage:\n%s", out)
	}
	if out := aiListActions(items, "stage git", 40); !strings.Contains(out, "git-stage: Git: Stage Current File (Git; confirm)") {
		t.Errorf("stage git:\n%s", out)
	}
	if out := aiListActions(items, "quit", 40); strings.Contains(out, "quit:") {
		t.Errorf("a never id is listed:\n%s", out)
	}
	if out := aiListActions(items, "build", 40); !strings.Contains(out, "user-build") {
		t.Errorf("user command not listed:\n%s", out)
	}
	if out := aiListActions(items, "gtstg", 40); !strings.Contains(out, "git-stage") { // fuzzy
		t.Errorf("fuzzy:\n%s", out)
	}
	if out := aiListActions(items, "", 2); strings.Count(out, "\n- ") != 1 || !strings.Contains(out, "more") {
		t.Errorf("cap 2:\n%s", out)
	}
	if out := aiListActions(items, "zzzz", 40); !strings.Contains(out, "no matching") {
		t.Errorf("no match:\n%s", out)
	}
}
