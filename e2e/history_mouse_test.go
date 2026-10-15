//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Bug report 2026-10-15 (screenshot of AI Chat History): the mouse did not
// work in the history list opened from the AI chat panel. A click on a
// chat opens it; the wheel moves the selection.
func TestAIHistoryMouseFromChatPanel(t *testing.T) {
	for _, sz := range [][2]int{{120, 36}, {190, 60}, {240, 70}} {
		t.Run(strconv.Itoa(sz[0])+"x"+strconv.Itoa(sz[1]), func(t *testing.T) { historyMouse(t, sz[0], sz[1]) })
	}
}

func historyMouse(t *testing.T, cols, rows int) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "termocode", "ai", "chats")
	_ = os.MkdirAll(dir, 0o755)
	now := time.Now().UnixMilli()
	for i, title := range []string{"First chat about files", "Second chat"} {
		body := `{"id":"c` + strconv.Itoa(i) + `","title":"` + title + `","messages":[{"role":"user","text":"` + title +
			`"},{"role":"assistant","text":"ANSWER-` + strconv.Itoa(i) + `"}],"updated_at_ms":` + strconv.FormatInt(now-int64(i)*1000, 10) + `}`
		_ = os.WriteFile(filepath.Join(dir, "c"+strconv.Itoa(i)+".json"), []byte(body), 0o644)
	}
	tm := Start(t, StartOpts{Cols: cols, Rows: rows, Home: home})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool { return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1]) })
	tm.settle()
	tm.Send("\x1ba") // Alt+A: open the AI chat (input focused)
	tm.WaitText("AI CHAT", uiTimeout)
	tm.settle()
	// The history button in the chat header.
	clicked := false
	for y, l := range tm.Lines() {
		if i := strings.Index(l, "AI CHAT"); i >= 0 {
			for _, g := range []string{"↺", "", "h"} {
				if j := strings.LastIndex(l, g); j > i {
					tm.Click(len([]rune(l[:j])), y)
					clicked = true
					break
				}
			}
			break
		}
	}
	if !clicked {
		t.Fatalf("no history button:\n%s", tm.Screen())
	}
	tm.WaitText("AI Chat History", uiTimeout)
	tm.settle()
	for y, l := range tm.Lines() {
		if i := strings.Index(l, "Second chat"); i >= 0 {
			tm.Click(len([]rune(l[:i]))+2, y)
			break
		}
	}
	tm.WaitText("ANSWER-1", uiTimeout)
	tm.Quit()
}
