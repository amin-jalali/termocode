package confirm

// Risky makes the card for a question that grants trust or access, or does
// something that is hard to take back. Every such card follows one rule:
//
//   - the safe answer (Deny, Cancel, Not now, Not in this project) is the
//     first button, plain (never red), and it has the focus, so Enter alone
//     on a fresh card never grants or changes anything;
//   - the actions follow it; none of them is the default;
//   - Esc closes the card, and the caller treats Esc as the safe answer.
//
// The user moves to an action with Right or Tab, or clicks it.
func Risky(title, message string, safe Button, actions ...Button) Model {
	safe.Style = StyleDefault
	m := New(title, message, append([]Button{safe}, actions...))
	m.safeID = safe.ID
	return m
}

// SafeID is the ID of the safe answer of a card made with Risky, and ""
// for any other card.
func (m Model) SafeID() string { return m.safeID }
