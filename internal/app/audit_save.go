package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Output: Save Audit Log (CMP-93, the local part) ────────────────────
//
// The AI channel (every agent tool call, ai_audit.go) and the Web
// channel (every write from the web page, web_audit.go) live only in
// the Output panel. This command saves both to a private file in
// termocode's own folder (<config>/audit/audit-<time>.log, 0600), so a
// person can keep or review what the AI and the web page did. Workflow
// runs keep their own audit.jsonl in each run folder. Teams, sign-in
// and a shared admin console need a hosted service: not here.

const auditSavePaletteID = "output-save-audit"

func init() {
	aiActionPolicies[auditSavePaletteID] = aiNever("the audit log is the user's record of what the AI and the web page did")
}

// auditDir is the folder of the saved logs (a var for tests).
var auditDir = func() (string, error) {
	d, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "audit"), nil
}

// auditLogText is the saved file: a header, then the AI lines, then the
// Web lines, each line redacted once more.
func auditLogText(at time.Time, root string, aiLines, webLines []string) string {
	var b strings.Builder
	b.WriteString("# termocode audit log\n")
	b.WriteString("# saved " + at.UTC().Format(time.RFC3339) + "\n")
	if root != "" {
		b.WriteString("# workspace " + root + "\n")
	}
	b.WriteString("# workflow runs keep their own audit.jsonl in each run folder\n")
	section := func(name string, lines []string) {
		fmt.Fprintf(&b, "\n## %s (%d lines)\n", name, len(lines))
		if len(lines) == 0 {
			b.WriteString("(empty)\n")
			return
		}
		for _, l := range lines {
			b.WriteString(ai.Redact(l) + "\n")
		}
	}
	section("AI", aiLines)
	section("Web", webLines)
	return b.String()
}

// auditSave writes the AI and Web channels to a new file and opens it.
func (m *Model) auditSave() tea.Cmd {
	aiLines, webLines := m.output.Lines(aiAuditChannel), m.output.Lines(webAuditChannel)
	if len(aiLines)+len(webLines) == 0 {
		return m.pushToastDetail(toast.Info, "Nothing to save yet", "The AI and Web audit channels of View: Output are empty.")
	}
	dir, err := auditDir()
	if err == nil {
		err = os.MkdirAll(dir, 0o700)
	}
	if err == nil {
		err = os.Chmod(dir, 0o700) // a folder made earlier with a wider mode
	}
	if err != nil {
		return m.pushToastDetail(toast.Errr, "The audit log was not saved", err.Error())
	}
	now := clock.Now()
	path, err := auditWriteNew(dir, "audit-"+now.Format("20060102-150405"), auditLogText(now, m.ws.Root, aiLines, webLines))
	if err != nil {
		return m.pushToastDetail(toast.Errr, "The audit log was not saved", err.Error())
	}
	if m.nvim != nil {
		m.ensureEditorWindowCurrent()
		_ = m.nvim.OpenFile("edit", path)
		m.focus = FocusEditor
	}
	return m.pushToastDetail(toast.Info, "Audit log saved", path)
}

// auditWriteNew writes body to dir/<base>.log, or <base>-2.log and on
// when that file is there: it never overwrites one.
func auditWriteNew(dir, base, body string) (string, error) {
	for i := 1; i < 100; i++ {
		name := base
		if i > 1 {
			name += "-" + strconv.Itoa(i)
		}
		path := filepath.Join(dir, name+".log")
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.WriteString(body)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(path)
			return "", werr
		}
		return path, nil
	}
	return "", errors.New("too many audit logs saved in this second")
}
