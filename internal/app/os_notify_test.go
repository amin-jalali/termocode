package app

import (
	"github.com/amin-jalali/termocode/internal/ai"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"sync"
	"testing"
)

func TestOSNotificationRoutes(t *testing.T) {
	for _, tc := range []struct{ term, program, ssh, want string }{
		{"xterm-256color", "iTerm.app", "", "\x1b]9;"},
		{"rxvt-unicode", "", "", "\x1b]777;notify;"},
		{"xterm", "", "remote", "\x1b]9;"},
	} {
		seq, native := notificationRoute(tc.term, tc.program, tc.ssh, "done\x1b\x07\n;bad")
		if native || !strings.HasPrefix(seq, tc.want) || strings.Count(seq, "\x1b") != 1 || strings.Count(seq, "\x07") != 1 {
			t.Fatalf("unsafe route %q, %v", seq, native)
		}
	}
	if seq, native := notificationRoute("xterm", "", "", "done"); seq != "" || !native {
		t.Fatal("no desktop fallback")
	}
}

func TestOSNotificationFocusAndSetting(t *testing.T) {
	withConfigDir(t)
	m := &Model{}
	if m.notifyOS("done") != nil {
		t.Fatal("notified while focused")
	}
	m.termBlurred = true
	if m.notifyOS("done") == nil {
		t.Fatal("no notification while blurred")
	}
	off := false
	if err := saveSettings(settingsConfig{Notifications: &off}); err != nil {
		t.Fatal(err)
	}
	if m.notifyOS("done") != nil {
		t.Fatal("off ignored")
	}
	row, ok := findSettingsRow("setting-notifications")
	if !ok || currentSettingValue(row, loadSettings()) != "false" {
		t.Fatal("setting not discoverable")
	}
}

func TestOSNotificationRunEnds(t *testing.T) {
	m, _, _, _ := aiSkillsModel(t, ai.Chunk{Delta: "Done"})
	old := deliverOSNotification
	var mu sync.Mutex
	var got []string
	deliverOSNotification = func(body string) { mu.Lock(); defer mu.Unlock(); got = append(got, body) }
	t.Cleanup(func() { deliverOSNotification = old })
	m.termBlurred = true
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		if batch, ok := c().(tea.BatchMsg); ok {
			for _, child := range batch {
				run(child)
			}
		}
	}
	run(m.onAIChatDone(aiChatDoneMsg{gen: m.ai.chatGen}))
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != "AI run ended" {
		t.Fatalf("notifications: %v", got)
	}
}
