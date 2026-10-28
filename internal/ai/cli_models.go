package ai

// The subscription CLIs have no command that lists their models, so
// their lists are built in. Claude Code's aliases (opus, sonnet, haiku)
// always point at the current models; the concrete ids follow them. The
// picker also offers a free model id, so a newer model never blocks.
var cliModels = map[string][]ModelInfo{
	CLIClaude: {
		{ID: "opus", Title: "Opus (latest)"},
		{ID: "sonnet", Title: "Sonnet (latest)"},
		{ID: "haiku", Title: "Haiku (latest)"},
		{ID: ModelClaudeOpus},
		{ID: ModelClaudeSonnet},
		{ID: ModelClaudeHaiku},
	},
	CLICodex: {
		{ID: "gpt-5-codex"},
		{ID: "gpt-5"},
		{ID: "gpt-5-mini"},
		{ID: "o3"},
	},
}

// CLIModels returns the built-in model list of a subscription CLI
// (CLIClaude, CLICodex) in picker order; nil for any other name.
func CLIModels(cli string) []ModelInfo {
	ms, ok := cliModels[cli]
	if !ok {
		return nil
	}
	return append([]ModelInfo(nil), ms...)
}
