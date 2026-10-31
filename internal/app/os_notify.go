package app

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/amin-jalali/termocode/internal/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

// Notification text is deliberately generic: no prompts, paths or tool output.
// OSC reaches the client terminal even over SSH. Unknown local terminals use
// the desktop helper; an SSH server must never notify its own desktop.
func notificationRoute(term, program, ssh, body string) (string, bool) {
	body = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == ';' {
			return ' '
		}
		return r
	}, body)
	p := strings.ToLower(program)
	if strings.Contains(term, "rxvt") || strings.Contains(p, "vte") || strings.Contains(p, "gnome") {
		return "\x1b]777;notify;termocode;" + body + "\x07", false
	}
	if ssh != "" || strings.Contains(p, "iterm") || strings.Contains(p, "wezterm") || strings.Contains(p, "ghostty") {
		return "\x1b]9;termocode: " + body + "\x07", false
	}
	return "", true
}

var deliverOSNotification = func(body string) {
	ssh := os.Getenv("SSH_CONNECTION") + os.Getenv("SSH_TTY")
	program := os.Getenv("TERM_PROGRAM")
	if program == "" && os.Getenv("VTE_VERSION") != "" {
		program = "vte"
	}
	seq, native := notificationRoute(os.Getenv("TERM"), program, ssh, body)
	if !native {
		if os.Getenv("TMUX") != "" {
			seq = "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
		}
		_, _ = os.Stdout.WriteString(seq)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if runtime.GOOS == "darwin" {
		// AppleScript receives the message as an argument, never as source code.
		_ = spawn.CommandContext(ctx, "osascript", "-e", "on run argv\ndisplay notification (item 1 of argv) with title \"termocode\"\nend run", body).Run()
	} else if runtime.GOOS == "linux" {
		_ = spawn.CommandContext(ctx, "notify-send", "--app-name=termocode", "--", "termocode", body).Run()
	}
}

func (m *Model) notifyOS(body string) tea.Cmd {
	if !m.termBlurred {
		return nil
	}
	if on := loadSettings().Notifications; on != nil && !*on {
		return nil
	}
	return func() tea.Msg { deliverOSNotification(body); return nil }
}

func (m *Model) applyNotificationSetting(value string) tea.Cmd {
	on, err := parseBoolLoose(value)
	if err != nil {
		return m.aiInfo("notifications: expected true/false")
	}
	cfg := loadSettings()
	cfg.Notifications = &on
	if err = saveSettings(cfg); err != nil {
		return m.aiToastErr("Could not save notifications", err)
	}
	return m.aiInfo("notifications = " + strconv.FormatBool(on))
}
