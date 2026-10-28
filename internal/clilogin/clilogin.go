// Package clilogin installs a subscription CLI (Claude Code, Codex) into
// termocode's tools folder and runs the CLI's own sign-in, so the Add AI
// card can connect a subscription without a terminal tab (AIP-5).
// termocode never sees the login: the CLI opens the browser and stores
// its own credentials; termocode only reads the link it prints and asks
// the CLI's status command whether the sign-in is done.
//
// Two layers:
//
//   - Session (Start) runs `claude auth login` or `codex login` (Codex
//     over SSH without a display: `codex login --device-auth`) under a
//     pty, strips escape sequences from its output, keeps a capped tail,
//     and sends Events: EventURL (the first complete https:// link),
//     EventCode (the one-time code of a device sign-in), EventNeedsCode
//     (the CLI asks for a pasted code: Session.Write sends it), and
//     EventExit last, after which the channel is closed. Cancel, or the
//     context ending, kills the CLI's whole process group.
//
//   - Flow (Run) is the whole sign-in path the card drives. It sends
//     States on Updates() and closes the channel when it is done. A CLI
//     that is already signed in goes straight to LoadingModels.
//     Flow.Write pastes a code, Flow.Cancel stops everything (installer
//     and CLI; the channel closes without a Failed state). While the
//     CLI's sign-in runs, the status is polled every 2 s; Ready ends the
//     flow and the CLI is left 10 s to exit, then stopped. The sign-in
//     times out after 5 minutes.
//
// The States, in order:
//
//   - PhaseInstalling{Elapsed, Line}: only when the CLI is missing; one
//     every Config.Tick (1 s) for the timer and one per installer
//     progress line.
//   - PhaseWaitingForBrowser{URL, Code, NeedsCode}: the CLI's sign-in
//     runs; URL is "" until the CLI prints it.
//   - PhaseLoadingModels, then PhaseReady{Models}: the CLI's status says
//     signed in.
//   - PhaseFailed{Err}: Err.Kind picks the card's buttons (FailNoNode:
//     Use an API key / Try again), Err.Msg is one plain line.
//
// A card uses it with tea.Cmds: Run once, then a Cmd that returns the
// next State from Updates() (re-issued after each one) until the channel
// closes.
//
// Secrets: the CLI and the installer get the environment without the
// vendors' API keys and termocode's own secrets (clistep.CleanEnv). The
// sign-in link may carry a one-time state: it is never logged and never
// put in an error; a pasted code goes only to the CLI's pty. Errors
// never contain the CLI's output; an install failure's Detail (for
// errors.log) is the installer's output tail, redacted.
//
// What a real CLI prints is not verified by the tests (they use fake
// scripts): the parsers depend only on the first https:// link, a prompt
// line that asks for a code, and a code-shaped word after the link.
package clilogin
