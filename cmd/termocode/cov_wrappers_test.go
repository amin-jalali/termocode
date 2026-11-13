package main

import "testing"

// Every `termocode <command>` wrapper passes its arguments on; with
// --help each one prints its usage and returns without starting anything.
func TestCommandWrappersPrintHelp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	for name, run := range map[string]func([]string) int{
		"acp": runACP, "agent": runAgent, "ai": runAI, "db": runDB, "http": runHTTP, "import": runImport,
		"review": runReview, "runner": runRunner, "serve": runServe, "service": runService, "task": runTask,
		"web": runWeb, "workflow": runWorkflow, "worktree": runWorktree,
	} {
		if code := run([]string{"--help"}); code != 0 && code != 2 {
			t.Errorf("%s --help: %d", name, code)
		}
	}
}
