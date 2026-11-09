package app

// Git parity: which AI tool does each git and worktree action of the GUI
// (the palette ids git-* and worktree-*, and the web ops git.*, pr.*,
// ci.*, worktrees.* and board.*). TestAIGitEveryGUIActionHasAnAITool
// fails when an action is on neither list. The value of aiGitParity is
// the tool (first word, a chat tool) and how it does it; "a + b" names
// two tools, one after the other.

// aiGitParity maps a GUI action to the tool (and op) that does it.
var aiGitParity = map[string]string{}

// aiGitNotForAI are the GUI actions the AI does not get, each with why.
var aiGitNotForAI = map[string]string{}
