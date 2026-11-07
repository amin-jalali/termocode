// Package trigger turns outside events into workflow runs (CMP-38).
//
// A workflow file's "on" list names its triggers. Two kinds exist:
//
//   - github: the app polls gh (the user's own login; no token is read
//     here) for new issues, newly labeled issues, new pull requests,
//     mentions in issue and pull request text, and failed CI runs. The
//     first poll only records what is there, so turning a trigger on
//     never starts runs for old items.
//   - webhook: a small HTTP handler on 127.0.0.1 takes POST
//     /hooks/<name>. Every request must be signed with the secret
//     "workflow-hook.<name>" from internal/secrets (GitHub, Slack and
//     Linear signatures, or a plain HMAC header for Jira and any JSON).
//     Unsigned or badly signed requests are refused; the body is capped.
//
// An Event is only data: its text came from outside, so the app shows
// it on a Cancel-first card (unless the trigger says "ask": false) and
// passes it to the run as trigger_* vars, never as a command.
package trigger
