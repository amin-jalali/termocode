package app

// Tooltip reports the hover label on screen: its text (padding
// included) and the screen cell of its first column. For tests.
func (m Model) Tooltip() (text string, x, y int, ok bool) {
	if !m.tip.shown {
		return "", 0, 0, false
	}
	return m.tip.text, m.tip.x, m.tip.y, true
}

// TooltipHelp reports the "More info" line of the hover label on
// screen: its text (padding included), first cell and guide topic. For
// tests.
func (m Model) TooltipHelp() (text string, x, y int, topic string, ok bool) {
	if !m.tip.shown || m.tip.help == "" {
		return "", 0, 0, "", false
	}
	return m.tip.helpText, m.tip.x, m.tip.y + 1, m.tip.help, true
}

// TooltipFor is the hover label of hit target id ("" = none). For tests.
func (m Model) TooltipFor(id string) string { return m.tooltipText(id) }
