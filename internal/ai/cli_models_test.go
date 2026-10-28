package ai

import (
	"reflect"
	"testing"
)

func TestCLIModels(t *testing.T) {
	claude := []string{"opus", "sonnet", "haiku", ModelClaudeOpus, ModelClaudeSonnet, ModelClaudeHaiku}
	if got := modelIDs(CLIModels(CLIClaude)); !reflect.DeepEqual(got, claude) {
		t.Fatalf("claude = %v, want %v", got, claude)
	}
	codex := []string{"gpt-6.1-sol", "gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"}
	if got := modelIDs(CLIModels(CLICodex)); !reflect.DeepEqual(got, codex) {
		t.Fatalf("codex = %v, want %v", got, codex)
	}
	if CLIModels("other") != nil {
		t.Fatal("an unknown CLI has no list")
	}
	// Callers get a copy: changing it does not change the next answer.
	CLIModels(CLIClaude)[0].ID = "x"
	if CLIModels(CLIClaude)[0].ID != "opus" {
		t.Fatal("CLIModels returned shared storage")
	}
}
