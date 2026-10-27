package ai

import (
	"fmt"
	"time"
)

// LimitError is a subscription CLI step that hit the CLI's usage limit
// (CS-4). The run pauses at that step; ResetAt (zero: the CLI did not
// say) is when the limit resets.
type LimitError struct {
	CLI     string // CLIClaude / CLICodex
	Msg     string // the CLI's own words, one line
	ResetAt time.Time
}

func (e *LimitError) Error() string {
	s := CLITitle(e.CLI) + " usage limit reached"
	if !e.ResetAt.IsZero() {
		s += fmt.Sprintf(" (resets at %s)", e.ResetAt.Local().Format("15:04"))
	}
	return s
}
