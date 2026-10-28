package clilogin

// FailKind says why a flow failed; the card picks its buttons by it.
type FailKind int

const (
	FailOther       FailKind = iota // could not start; Try again / Use an API key
	FailNoNode                      // Node.js (npm) is missing
	FailOffline                     // no network
	FailTimeout                     // the sign-in took too long
	FailNotFinished                 // the CLI ended without a sign-in
	FailInstall                     // the install failed (Detail: the redacted output tail)
)

// Error is a failed flow: Msg is one plain line for the user; Detail is
// for errors.log only (redacted; never the sign-in link).
type Error struct {
	Kind   FailKind
	Msg    string
	Detail string
}

func (e *Error) Error() string { return e.Msg }

func errNoNode(title string) *Error {
	return &Error{Kind: FailNoNode, Msg: title + " needs Node.js. Install Node.js (nodejs.org or your package manager), then press Try again. Or use an API key instead."}
}

func errOffline() *Error {
	return &Error{Kind: FailOffline, Msg: "No connection. Try again when you are online."}
}

func errTimeout() *Error {
	return &Error{Kind: FailTimeout, Msg: "Sign-in took too long. Try again."}
}

func errNotFinished() *Error {
	return &Error{Kind: FailNotFinished, Msg: "Sign-in did not finish. Try again, or use an API key."}
}
