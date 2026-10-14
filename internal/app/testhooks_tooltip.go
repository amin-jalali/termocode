package app

// Tooltip reports the hover label on screen: its text (padding
// included) and the screen cell of its first column. For tests.
func (m Model) Tooltip() (text string, x, y int, ok bool) {
	if !m.tip.shown {
		return "", 0, 0, false
	}
	return m.tip.text, m.tip.x, m.tip.y, true
}

// TooltipFor is the hover label of hit target id ("" = none). For tests.
func (m Model) TooltipFor(id string) string { return m.tooltipText(id) }
