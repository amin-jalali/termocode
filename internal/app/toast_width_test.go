package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/editor"
	"github.com/amin-jalali/termocode/internal/toast"
)

func TestErrorToastReadableWithChatPanel(t *testing.T) {
	m := Model{editor: editor.New(nil), w: 140, h: 40, showExp: true, explorerWidth: 26, actionsOpen: true, actionsPinned: true, actionsWidth: 60}
	const detail = "Another Git operation is still running"
	m.toast, _ = m.toast.PushDetail(toast.Errr, "Stage all failed", detail)
	places := m.toastPlaces()
	if len(places) != 1 {
		t.Fatal("missing error toast")
	}
	p := places[0]
	if p.compact {
		t.Fatal("error detail squeezed into the compact title row")
	}
	if !strings.Contains(ansi.Strip(strings.Join(p.card.Lines, "\n")), detail) {
		t.Fatal("error reason cut")
	}
	if p.x < 0 || p.x+p.w > m.w {
		t.Fatal("toast outside screen")
	}
}
