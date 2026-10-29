package tabbar

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// Tabs whose names hold CJK, an emoji ZWJ sequence, a flag and an
// accent keep the bar at exactly its width, whole or scrolled.
func TestWideTabNamesKeepTheBarWidth(t *testing.T) {
	bufs := []nvim.BufferInfo{
		{ID: 1, Path: "/w/日本語のとても長いファイル名です.md"},
		{ID: 2, Path: "/w/emoji-👩‍💻-🇮🇷-notes.txt"},
		{ID: 3, Path: "/w/café.txt"},
		{ID: 4, Path: "/w/فارسی-名前.txt"},
	}
	for _, w := range []int{30, 50, 80, 200} {
		for _, active := range []int{1, 2, 3, 4} {
			m := New()
			m.SetWidth(w)
			m.SetBuffers(bufs, active)
			if got := lipglossWidth(m.View()); got != w {
				t.Errorf("width %d, active %d: bar is %d cells", w, active, got)
			}
		}
	}
}
