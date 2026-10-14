package app

// SetupButtonInfo is one button of the setup card. For tests.
type SetupButtonInfo struct {
	ID, Label string
	Disabled  bool
	Why       string // the reason told when a disabled button is pressed
}

// SetupCardButtons lists the setup card's buttons in Tab order (nil when
// the card is hidden). For tests.
func (m Model) SetupCardButtons() []SetupButtonInfo {
	var out []SetupButtonInfo
	for _, b := range m.setupCardView().buttons() {
		out = append(out, SetupButtonInfo{ID: b.id, Label: b.label, Disabled: b.disabled, Why: b.why})
	}
	return out
}
