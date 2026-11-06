package webproto

// The user's own commands from the web (WEBP-7; the laptop's Open Config:
// User Commands, commands.json). Each is a "User: <title>" row of the
// laptop palette and of Run Task…; the page lists them, edits them in a
// form and saves the whole list at once. A command line is the user's
// own text: listing and saving are the controller's, and a save is
// audited. Running one is tasks.run with its "User: <title>" label.

// Op names.
const (
	OpUserCmdsList = "usercmds.list"
	OpUserCmdsSave = "usercmds.save"
)

// User command limits.
const (
	MaxUserCmds   = 100  // commands in commands.json
	MaxUserCmdLen = 4096 // bytes of one command line
)

// UserCmd is one user command, as commands.json has it.
type UserCmd struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Cmd   string `json:"cmd"`
	CWD   string `json:"cwd,omitempty"` // "" = the workspace folder
}

// UserCmdSaveArgs is the whole new list.
type UserCmdSaveArgs struct {
	Items []UserCmd `json:"items"`
}

func (a *UserCmdSaveArgs) validate(int) error {
	if len(a.Items) > MaxUserCmds {
		return invalid("too many commands")
	}
	seen := map[string]bool{}
	for _, c := range a.Items {
		if !validUserCmdID(c.ID) {
			return invalid("bad command id")
		}
		if seen[c.ID] {
			return invalid("two commands have the id %s", c.ID)
		}
		seen[c.ID] = true
		if !userCmdText(c.Title, 80, true) {
			return invalid("bad command title")
		}
		if !userCmdText(c.Cmd, MaxUserCmdLen, true) {
			return invalid("bad command line")
		}
		if c.CWD != "" && !userCmdText(c.CWD, 512, true) {
			return invalid("bad command folder")
		}
	}
	return nil
}

// validUserCmdID: letters, digits, - and _ (up to 40).
func validUserCmdID(s string) bool {
	if s == "" || len(s) > 40 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// userCmdText: one line of up to max bytes with no control characters
// (tab is fine), not only spaces when need.
func userCmdText(s string, max int, need bool) bool {
	if len(s) > max {
		return false
	}
	blank := true
	for _, r := range s {
		if r < 0x20 && r != '\t' || r == 0x7f {
			return false
		}
		if r != ' ' && r != '\t' {
			blank = false
		}
	}
	return !need || !blank
}

// UserCmdList answers usercmds.list and usercmds.save, in the file's
// order. Error says why commands.json does not parse (then Items is
// empty and a save is refused, so the file is never lost).
type UserCmdList struct {
	Items []UserCmd `json:"items"`
	Error string    `json:"error,omitempty"`
}

var opUserCmdRows = []Policy{
	{Op: OpUserCmdsList, New: newOf[EmptyArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpUserCmdsSave, New: newOf[UserCmdSaveArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 512 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opUserCmdRows...); return true }()

func init() {
	registerResult(OpUserCmdsList, UserCmdList{})
	registerResult(OpUserCmdsSave, UserCmdList{})
}
