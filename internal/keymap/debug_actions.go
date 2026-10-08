package keymap

// Group D — Run & Debug: stable keymap.json names for the debugger
// actions. Kept in its own file so the shared actionNames table stays
// untouched.
func init() {
	for a, n := range map[Action]string{
		ActionDebugToggleBreakpoint: "DebugToggleBreakpoint",
		ActionDebugStepOver:         "DebugStepOver",
		ActionDebugStepInto:         "DebugStepInto",
		ActionDebugStepOut:          "DebugStepOut",
		ActionDebugStartContinue:    "DebugStartContinue",
		ActionDebugStop:             "DebugStop",
	} {
		if _, ok := actionNames[a]; !ok {
			actionNames[a] = n
		}
	}
}
