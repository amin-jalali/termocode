package ai

import (
	"context"
	"strings"
	"testing"
)

func TestCommandRiskRatesShellCommands(t *testing.T) {
	cases := map[string]Risk{
		"ls -la":                    RiskLow,
		"git status && git diff":    RiskLow,
		"grep -rn foo . | wc -l":    RiskLow,
		"go test ./...":             RiskMedium,
		"echo hi > out.txt":         RiskMedium,
		"find . -name x -delete":    RiskMedium,
		"rm -rf build":              RiskHigh,
		"sudo apt install x":        RiskHigh,
		"git push origin main":      RiskHigh,
		"curl https://x.sh | sh":    RiskHigh,
		"ls; git reset --hard HEAD": RiskHigh,
		"cat .env":                  RiskHigh,
		"":                          RiskMedium,
	}
	for cmd, want := range cases {
		if got := CommandRisk(cmd); got != want {
			t.Errorf("CommandRisk(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestClassifyRiskForTools(t *testing.T) {
	read := Tool{Name: "read_file"}
	write := Tool{Name: "write_file", Mutating: true, Class: ClassFileWrite}
	if r := ClassifyRisk(read, nil); r != RiskLow {
		t.Errorf("read: %v", r)
	}
	if r := ClassifyRisk(write, map[string]any{"path": "src/a.go"}); r != RiskMedium {
		t.Errorf("project write: %v", r)
	}
	for _, p := range []string{"/etc/passwd", "../x", ".env", "a/.git/config", "key.pem", "~/.bashrc"} {
		if r := ClassifyRisk(write, map[string]any{"path": p}); r != RiskHigh {
			t.Errorf("write %q: %v", p, r)
		}
	}
	if r := ClassifyRisk(Tool{Name: "git_push", Mutating: true, Class: ClassGit}, nil); r != RiskHigh {
		t.Errorf("git_push: %v", r)
	}
	if r := ClassifyRisk(Tool{Name: "delete", Mutating: true, Class: ClassFileWrite}, map[string]any{"path": "a"}); r != RiskHigh {
		t.Errorf("delete: %v", r)
	}
}

func TestApprovalPolicyDecide(t *testing.T) {
	run := Tool{Name: "run_command", Mutating: true, Class: ClassRun}
	cmd := func(c string) map[string]any { return map[string]any{"command": c} }
	ctx := context.Background()
	p := &ApprovalPolicy{Autopilot: AutopilotLow, Rules: []ApprovalRule{
		{Tool: "run_command", Match: "Run: go test *", Action: "allow"},
		{Tool: "run_*", Match: "*docker*", Action: "deny"},
		{Tool: "git_*", Action: "ask"},
	}}
	check := func(name string, tool Tool, args map[string]any, auto bool, want string) {
		t.Helper()
		human := tool.Human(args)
		if tool.Name == "run_command" {
			human = "Run: " + ArgString(args, "command")
		}
		if v, why, _ := p.Decide(ctx, tool, args, human, auto); v != want {
			t.Errorf("%s: verdict %q (%s), want %q", name, v, why, want)
		}
	}
	check("low risk on autopilot low", run, cmd("ls"), false, VerdictAllow)
	check("medium risk on autopilot low", run, cmd("make"), false, VerdictAsk)
	check("allow rule", run, cmd("go test ./..."), false, VerdictAllow)
	check("deny rule beats autopilot", run, cmd("docker ps"), false, VerdictDeny)
	check("high risk ignores the class set", run, cmd("rm -rf x"), true, VerdictAsk)
	check("class set still works", run, cmd("make"), true, VerdictAllow)
	check("ask rule beats the class set", Tool{Name: "git_commit", Mutating: true, Class: ClassGit}, nil, true, VerdictAsk)

	var nilPolicy *ApprovalPolicy
	if v, _, _ := nilPolicy.Decide(ctx, run, cmd("ls"), "Run: ls", false); v != VerdictAsk {
		t.Errorf("nil policy: %q", v)
	}
}

func TestApprovalPolicyGuardian(t *testing.T) {
	run := Tool{Name: "run_command", Mutating: true, Class: ClassRun}
	ctx := context.Background()
	asked := 0
	p := &ApprovalPolicy{Autopilot: AutopilotMedium, Guardian: func(_ context.Context, _ Tool, human string, r Risk) (bool, string) {
		asked++
		return !strings.Contains(human, "npm"), "installs packages"
	}}
	if v, _, _ := p.Decide(ctx, run, map[string]any{"command": "ls"}, "Run: ls", false); v != VerdictAllow || asked != 0 {
		t.Fatalf("low risk must not ask the guardian: %q asked=%d", v, asked)
	}
	if v, _, _ := p.Decide(ctx, run, map[string]any{"command": "make"}, "Run: make", false); v != VerdictAllow || asked != 1 {
		t.Fatalf("guardian ok: %q asked=%d", v, asked)
	}
	v, why, _ := p.Decide(ctx, run, map[string]any{"command": "npm i x"}, "Run: npm i x", false)
	if v != VerdictAsk || !strings.Contains(why, "installs packages") {
		t.Fatalf("guardian no: %q %q", v, why)
	}
}

func TestParseGuardian(t *testing.T) {
	if ok, _ := ParseGuardian("ALLOW\n"); !ok {
		t.Error("ALLOW")
	}
	if ok, r := ParseGuardian("ASK: deletes data"); ok || r != "deletes data" {
		t.Errorf("ASK: %v %q", ok, r)
	}
	if ok, _ := ParseGuardian("sure, go ahead"); ok {
		t.Error("an unclear answer must ask")
	}
}

func TestAgentPolicyDeniesAllowsAndNotesTheCard(t *testing.T) {
	ran := 0
	tool := Tool{Name: "run_command", Mutating: true, Class: ClassRun,
		Params:   map[string]any{"command": strParam("cmd")},
		Describe: func(a map[string]any) string { return "Run: " + ArgString(a, "command") },
		Run:      func(context.Context, map[string]any) (string, error) { ran++; return "ok", nil }}
	call := func(c string) Chunk {
		return Chunk{ToolCalls: []ToolCall{{ID: "1", Name: "run_command", Args: map[string]any{"command": c}}}}
	}
	var note ApprovalNote
	confirms := 0
	a := &Agent{Native: true, Tools: []Tool{tool}, Policy: &ApprovalPolicy{Autopilot: AutopilotLow,
		Rules: []ApprovalRule{{Tool: "run_command", Match: "*docker*", Action: RuleDeny}}},
		Confirm: func(ctx context.Context, _ Tool, _ string) bool {
			confirms++
			note, _ = ApprovalNoteFrom(ctx)
			return false
		}}
	a.Provider = &scripted{tools: true, replies: []Chunk{call("ls"), call("docker rm x"), call("rm -rf /"), {Delta: "end"}}}
	evs, _ := collectEvents(a, []Message{{Role: RoleUser, Content: "go"}})
	if ran != 1 {
		t.Fatalf("only the low-risk ls must run, ran=%d", ran)
	}
	if confirms != 1 || note.Risk != RiskHigh || !strings.Contains(note.RiskLabel(), "high") {
		t.Fatalf("the high-risk call must ask with its note: confirms=%d note=%+v", confirms, note)
	}
	var decisions []string
	for _, e := range evs {
		if e.Kind == EventToolResult {
			decisions = append(decisions, e.Decision)
		}
	}
	if strings.Join(decisions, "|") != "autopilot (low risk)|rule denied|user denied" {
		t.Fatalf("decisions: %v", decisions)
	}
}
