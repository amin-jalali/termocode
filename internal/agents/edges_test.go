package agents

import "testing"

func TestFromCommandRunners(t *testing.T) {
	cases := map[string]string{
		"npx claude@latest":                    "claude", // the binary name as the package
		"pnpm dlx @openai/codex@0.4.1":         "codex",
		"yarn exec opencode-ai@1.2":            "opencode",
		"npx -y":                               "",
		"npx cowsay hello":                     "",
		"DEBUG=1 FOO=bar":                      "",
		"bunx --bun @anthropic-ai/claude-code": "claude",
	}
	for cmd, want := range cases {
		a, ok := FromCommand(cmd)
		if a.ID != want || ok != (want != "") {
			t.Errorf("FromCommand(%q) = %q, %v; want %q", cmd, a.ID, ok, want)
		}
	}
}

func TestByIDAndStateNames(t *testing.T) {
	if a, ok := ByID("codex"); !ok || a.Bin != "codex" {
		t.Errorf("codex = %+v, %v", a, ok)
	}
	if _, ok := ByID("mystery"); ok {
		t.Error("an unknown id was found")
	}
	for s, want := range map[State]string{Working: "working", Waiting: "waiting", Done: "done", Unknown: "unknown"} {
		if s.String() != want {
			t.Errorf("%d = %q", s, s.String())
		}
	}
}
