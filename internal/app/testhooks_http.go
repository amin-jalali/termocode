package app

// AIAuditLines returns the AI audit log (the Output "AI" channel). For
// tests (internal/testkit): the HTTP client's "no token anywhere" check.
func (m Model) AIAuditLines() []string { return m.output.Lines(aiAuditChannel) }
