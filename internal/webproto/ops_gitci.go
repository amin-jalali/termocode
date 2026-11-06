package webproto

// Git: Fix CI with AI from the desktop (WEBP-9, Git): ci.fix_prompt reads
// the failed run's log through the laptop's gh and answers the laptop's
// own Fix with AI message (the log redacted). The page puts it in the
// chat input; nothing goes to the AI until the user sends it.

// OpCIFixPrompt is the op name.
const OpCIFixPrompt = "ci.fix_prompt"

// CIFixPrompt is the result of ci.fix_prompt.
type CIFixPrompt struct {
	Prompt string `json:"prompt"`
}

var opGitCIRows = []Policy{
	{Op: OpCIFixPrompt, New: newOf[EmptyArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
}

// The rows join the table before any init() runs (as ops_more.go's).
var _ = func() bool { opTable = append(opTable, opGitCIRows...); return true }()

func init() {
	registerResult(OpCIFixPrompt, CIFixPrompt{})
}
