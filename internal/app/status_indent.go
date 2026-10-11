package app

// statusIndent is the status bar's indentation chip for the active buffer
// ("Spaces: 2", "Tabs: 4"), as nvim reports it (tab_size, EditorConfig,
// the file type's own settings). Before the first state fetch it shows
// nvim's default for this app.
func (m Model) statusIndent() string {
	if m.indentLabel != "" {
		return m.indentLabel
	}
	return "Spaces: 4"
}
