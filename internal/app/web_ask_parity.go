package app

import "github.com/amin-jalali/termocode/internal/webproto"

// The page asks where the TUI asks (owner 2026-11-08, "match the TUI
// exactly"), and nowhere else. webTUIAsksNone are the laptop-card ops
// the TUI does without a question (a builder marks a variant with
// webAsk.noAsk): a page that approves its own actions runs them at once
// in the one path of web_ask.go; with web_approve_on_laptop on they stay
// laptop cards.

// webTUIAsksNone are the laptop-card ops the TUI does without a question.
var webTUIAsksNone = map[string]bool{
	webproto.OpAIUseMCPProfile:        true, // the MCP profile picker
	webproto.OpAIUseModel:             true, // the model picker
	webproto.OpAIUseProfile:           true, // the profile picker
	webproto.OpAISetAutoApprove:       true, // the auto-approve picker toggles
	webproto.OpContainersExec:         true, // e / a open the shell tab
	webproto.OpContainersHostTerminal: true, // t on a host row
	webproto.OpContainersOpenDB:       true, // open in Databases
	webproto.OpGitCheckout:            true, // the branch picker
	webproto.OpGitCheckoutTag:         true, // the tag picker
	webproto.OpGitPull:                true, // Git actions → Pull
	webproto.OpGitPush:                true, // Git actions → Push
	webproto.OpGitStash:               true, // the stash prompt
	webproto.OpGitStashApply:          true,
	webproto.OpGitStashPop:            true,
	webproto.OpPRCheckout:             true,
	webproto.OpWorkspaceAddFolder:     true, // the folder prompt
	webproto.OpWorkspaceOpen:          true, // open a recent workspace
	webproto.OpWorkspaceRemoveFolder:  true, // the folder picker
}

// webAskDirect: the page gets no card for a (the TUI asks nothing: it runs).
func webAskDirect(a *webAsk) bool {
	return a.noAsk || webTUIAsksNone[a.op]
}
