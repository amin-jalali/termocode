package toast

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestErrorReasonBeforeLongPath(t *testing.T) {
	raw := "open /private/var/folders/" + strings.Repeat("long/", 30) + "notes.txt: no such file or directory"
	m := New()
	m, _ = m.PushDetail(Errr, "Open failed", raw)
	text := ansi.Strip(strings.Join(m.Cards(72)[0].Lines, "\n"))
	if !strings.Contains(text, "no such file or directory") || !strings.Contains(text, "notes.txt") || !strings.Contains(text, "…") {
		t.Fatalf("reason or filename lost: %s", text)
	}
	if strings.Index(text, "no such file") > strings.Index(text, "notes.txt") {
		t.Fatal("reason follows the path")
	}
	if m.Live()[0].Detail != raw {
		t.Fatal("full error must remain available for logs and mirrors")
	}
}

func TestErrorTitleReasonAndMiddlePath(t *testing.T) {
	for _, detail := range []string{"", "More information"} {
		m := New()
		m, _ = m.PushDetail(Errr, "stat /very/long/path/to/notes.txt: permission denied", detail)
		for _, cards := range [][]Card{m.Cards(52), m.CompactCards(52, 1)} {
			text := ansi.Strip(strings.Join(cards[0].Lines, "\n"))
			if strings.Index(text, "permission denied") < 0 || strings.Index(text, "permission denied") > strings.Index(text, "notes.txt") {
				t.Fatalf("reason not first: %s", text)
			}
		}
	}
	if got := middlePath("/very/long/path/to/notes.txt", 11); !strings.HasSuffix(got, "notes.txt") {
		t.Fatalf("lost filename that fits: %s", got)
	}
}
