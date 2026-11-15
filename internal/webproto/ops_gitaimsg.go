package webproto

import "time"

// "Write it for me" in the phone's commit sheet: the laptop's Git:
// Generate Commit Message with AI (GITX-8) for the web. git.ai_message
// sends the staged diff to the laptop's AI once and answers with the
// message it wrote; nothing is committed: the page puts the message in
// the box, where the user reads and changes it.

// OpGitAIMessage asks the laptop's AI for a commit message of the staged changes.
const OpGitAIMessage = "git.ai_message"

// GitAIMessage is the result of git.ai_message: the message the AI wrote
// (a subject line, and a body after a blank line when it needs one).
type GitAIMessage struct {
	Message string `json:"message"`
}

var opGitAIMsgRows = []Policy{
	{Op: OpGitAIMessage, New: newOf[EmptyArgs](), Role: RoleController, Rate: Rate{N: 1, Per: 2 * time.Second}, MaxBytes: 1 * kib, Live: 1, Audit: AuditCounted, Phase: PhaseV2},
}

// The rows join the table before any init() runs (as ops_more.go's).
var _ = func() bool { opTable = append(opTable, opGitAIMsgRows...); return true }()

func init() {
	registerResult(OpGitAIMessage, GitAIMessage{})
}
