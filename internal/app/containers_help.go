package app

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Containers view: the HELP section ────────────────────────────────────
//
// Three action rows (Open guide, Keys, Report a problem) and one info row
// about the engine. The rows run on Enter, `l` and a click.

const (
	// containersGuidePage is the built-in guide page of this view.
	containersGuidePage = "guides/containers.md"
	// containersKeysAnchor is the "Keys" heading of that page.
	containersKeysAnchor = "keys"
	// reportProblemURL is the project's issue page.
	reportProblemURL = "https://github.com/amin-jalali/termocode/issues/new"
)

// helpSectionBody is the HELP section's rows.
func (st *servicesState) helpSectionBody(key string) []svcRow {
	item := func(h hostKind, id, name string) svcRow {
		return svcRow{kind: svcRowHostItem, section: key, host: h, hostID: id, name: name}
	}
	out := []svcRow{
		item(hostHelpGuide, "guide", "Open guide"),
		item(hostHelpKeys, "keys", "Keys"),
		item(hostHelpReport, "report", "Report a problem"),
	}
	for _, l := range st.helpEngineLines() {
		out = append(out, svcRow{kind: svcRowHint, section: key, text: l})
	}
	return out
}

// helpEngineLines is the engine info (hint rows, short enough for a
// narrow sidebar): "docker 27.3.1", "context prod", "not running".
func (st *servicesState) helpEngineLines() []string {
	switch {
	case !st.containerDetected:
		return []string{"Looking for an engine…"}
	case st.containerEngine.Binary == "":
		return []string{"No container engine"}
	}
	e := st.containerEngine
	line := e.Kind
	if v := e.Version.Version; v != "" {
		line += " " + v
	}
	out := []string{line}
	if ctx := st.activeContext(); ctx != "" {
		out = append(out, "context "+ctx)
	}
	if !e.Reachable {
		out = append(out, "not running")
	}
	return out
}

// isHelpHostKind reports whether k is one of the HELP action rows.
func isHelpHostKind(k hostKind) bool {
	return k == hostHelpGuide || k == hostHelpKeys || k == hostHelpReport
}

// helpRowLook is the glyph and colours of a HELP action row.
func helpRowLook(r svcRow) (string, theme.Color256, theme.Color256) {
	ascii := theme.CurrentIconMode == theme.IconModeASCII
	switch r.host {
	case hostHelpGuide:
		return helpIcon(), theme.TextSecondary, theme.TextPrimary
	case hostHelpKeys:
		return "?", theme.TextSecondary, theme.TextPrimary
	case hostHelpReport:
		if ascii {
			return ">", theme.TextSecondary, theme.TextPrimary
		}
		return "↗", theme.TextSecondary, theme.TextPrimary
	}
	return "", 0, theme.TextPrimary
}

// helpRowFooter is the two footer lines of a HELP action row.
func helpRowFooter(r svcRow) (string, string) {
	switch r.host {
	case hostHelpGuide:
		return "the Containers guide", "Enter opens it here"
	case hostHelpKeys:
		return "every key of this view", "Enter opens the guide at Keys"
	case hostHelpReport:
		return "report a problem or an idea", "Enter opens the issue page"
	}
	return "", ""
}

// openContainersGuide opens the built-in guide at the Containers page.
func (m *Model) openContainersGuide(anchor string) tea.Cmd {
	return m.openHelp(help.Loc{Page: containersGuidePage, Anchor: anchor})
}

// reportProblem opens the issue page in the browser; over SSH (no
// browser on this machine) it copies the address instead.
func (m *Model) reportProblem() tea.Cmd {
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		m.copyToClipboard(reportProblemURL)
		return m.servicesToast(toast.Info, "Copied "+reportProblemURL, "open it in a browser on your own computer")
	}
	if err := osOpen(reportProblemURL); err != nil {
		return m.servicesToast(toast.Warn, "Open the page in your browser", reportProblemURL)
	}
	return m.servicesToast(toast.Info, "Opened", reportProblemURL)
}

// runHelpRow runs a HELP action row (ok=false: not a HELP row).
func (m *Model) runHelpRow(r svcRow) (tea.Cmd, bool) {
	if r.kind != svcRowHostItem {
		return nil, false
	}
	switch r.host {
	case hostHelpGuide:
		return m.openContainersGuide(""), true
	case hostHelpKeys:
		return m.openContainersGuide(containersKeysAnchor), true
	case hostHelpReport:
		return m.reportProblem(), true
	}
	return nil, false
}
