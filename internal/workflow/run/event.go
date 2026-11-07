package run

import "time"

// Kind is what an Event tells.
type Kind string

// Event kinds.
const (
	StepStart    Kind = "step_start"
	StepDone     Kind = "step_done"
	StepFailed   Kind = "step_failed"
	StepSkipped  Kind = "step_skipped" // when was false, or a dry run did not run it
	ItemStart    Kind = "item_start"
	ItemDone     Kind = "item_done" // Err is set when the item failed
	WriteWait    Kind = "write_wait"
	WriteAllowed Kind = "write_allowed"
	WriteDenied  Kind = "write_denied"
	RunDone      Kind = "run_done"
	RunFailed    Kind = "run_failed"
	RunCancelled Kind = "run_cancelled"
	RunPaused    Kind = "run_paused"
	RunHeld      Kind = "run_held"      // the user paused the run (Handle.Pause)
	RunContinued Kind = "run_continued" // the user let it go on (Handle.Continue)
	LoopRound    Kind = "loop_round"    // a loop step starts a round (Attempt is the round)
	WaitPoll     Kind = "wait_poll"     // a wait step looked and waits on (Err is the detail)
)

// Event is one thing that happened in a run. Options.OnEvent gets them
// in order, on the run's goroutine.
type Event struct {
	Kind    Kind
	Run     string    // the run id
	Step    string    // the step key (its id, or line<N>)
	Item    string    // the item key ("" outside an each)
	Attempt int       // the try of the step (1 = first)
	Err     string    // the error, for failed / denied events
	At      time.Time //
}
