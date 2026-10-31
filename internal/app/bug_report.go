package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	tea "github.com/charmbracelet/bubbletea"
)

const confirmKindBugReport confirmKindEnum = 2651
const reportMaxBytes = 256 << 10

type bugReportState struct {
	body, root string
	chat       bool
}

func reportSecretField(k string) bool {
	k = strings.ToLower(k)
	return looksSecret(k) || k == "key" || k == "auth" || k == "cookie" || k == "headers" || k == "env" || k == "args"
}

// Blank secret fields, and also remove their values if copied into logs/chat.
// Malformed settings are omitted: never fall back to exporting raw bytes.
func buildBugReport(settings []byte, logs, chat string) string {
	var data any
	var secrets []string
	var collect func(any)
	collect = func(v any) {
		switch x := v.(type) {
		case string:
			if x != "" {
				secrets = append(secrets, x)
			}
		case map[string]any:
			for _, c := range x {
				collect(c)
			}
		case []any:
			for _, c := range x {
				collect(c)
			}
		}
	}
	var scrub func(any) any
	scrub = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				if reportSecretField(k) {
					collect(c)
					x[k] = ""
				} else {
					x[k] = scrub(c)
				}
			}
		case []any:
			for i, c := range x {
				x[i] = scrub(c)
			}
		}
		return v
	}
	config := "Settings unavailable or invalid; omitted."
	if len(settings) > 0 && json.Unmarshal(settings, &data) == nil {
		clean := scrub(data)
		b, _ := json.MarshalIndent(clean, "", "  ")
		config = string(b)
	}
	body := fmt.Sprintf("# termocode bug report\n\nPlatform: %s/%s\n\n## Settings (secret fields blanked)\n\n%s\n\n## Errors\n\n%s\n", runtime.GOOS, runtime.GOARCH, config, logs)
	if chat != "" {
		body += "\n## Optional chat\n\n" + chat
	}
	for _, secret := range secrets {
		body = strings.ReplaceAll(body, secret, "[REDACTED]")
	}
	return ai.Redact(aiCleanText(body))
}

// Bounded reads keep large logs from blocking the report screen.
func reportRead(path string, tail bool) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil
	}
	if st.Size() > reportMaxBytes {
		if !tail {
			return nil
		}
		if _, err = f.Seek(-reportMaxBytes, io.SeekEnd); err != nil {
			return nil
		}
	}
	b, _ := io.ReadAll(io.LimitReader(f, reportMaxBytes))
	if tail && st.Size() > reportMaxBytes {
		if i := strings.IndexByte(string(b), '\n'); i >= 0 {
			b = b[i+1:]
		}
		b = append([]byte("[older log entries omitted]\n"), b...)
	}
	return b
}

func (m *Model) openBugReport(chat bool) tea.Cmd {
	path, _ := settingsConfigPath()
	logs := string(reportRead(filepath.Join(filepath.Dir(path), "errors.log"), true))
	if logs == "" {
		logs = strings.Join(errorEntries(), "\n")
	}
	text := ""
	if chat && m.ai != nil {
		text = aiChatMarkdown(m.ai.conv)
		if len(text) > reportMaxBytes {
			text = text[:reportMaxBytes] + "\n[chat truncated]"
		}
	}
	p := &bugReportState{body: buildBugReport(reportRead(path, false), logs, text), root: aiWorkspaceRoot(), chat: chat}
	m.extras.bugReport = p
	toggle := "Include chat"
	if chat {
		toggle = "Remove chat"
	}
	m.confirm = confirm.Risky("Bug report preview", "Review before saving. Check for private text. Nothing is sent.\nSave creates a new termocode-report-*.md in this workspace.\n\n"+p.body,
		confirm.Button{ID: "cancel", Title: "Cancel"}, confirm.Button{ID: "chat", Title: toggle}, confirm.Button{ID: "save", Title: "Save report", Style: confirm.StyleDestructive}).WithScrollText().WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmKind = confirmKindBugReport
	m.confirmOpen = true
	return nil
}

func (m *Model) answerBugReport(id string) tea.Cmd {
	p := m.extras.bugReport
	if p == nil {
		return nil
	}
	if id == "chat" {
		return m.openBugReport(!p.chat)
	}
	m.extras.bugReport = nil
	if id != "save" {
		return nil
	}
	f, err := os.CreateTemp(p.root, "termocode-report-*.md")
	if err != nil {
		return m.aiToastErr("Could not save report", err)
	}
	_, err = f.WriteString(p.body)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return m.aiToastErr("Could not save report", err)
	}
	return m.aiInfo("Report saved: " + f.Name())
}
