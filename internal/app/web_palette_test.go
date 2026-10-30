package app

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webPaletteRan records the ids the web palette ran (no laptop command
// really runs in these tests).
func webPaletteRan(t *testing.T) *[]string {
	t.Helper()
	var ran []string
	old := webPaletteDispatch
	webPaletteDispatch = func(_ *Model, id string) tea.Cmd { ran = append(ran, id); return nil }
	t.Cleanup(func() { webPaletteDispatch = old })
	return &ran
}

// Spec (web palette): palette.list offers the laptop palette's commands
// on the web allow-list, in the palette's order, with the laptop's title
// and keys. None is a command the AI may never run, and every one the AI
// policy calls destructive (or the list marks) waits for a laptop card.
func TestWebPaletteListIsTheSafeAllowList(t *testing.T) {
	r := newWebFilesRig(t)
	var res webproto.PaletteList
	webResData(t, r.call(webproto.OpPaletteList, &webproto.EmptyArgs{}), &res)
	if len(res.Items) < 8 {
		t.Fatalf("only %d commands: %+v", len(res.Items), res.Items)
	}
	byID := map[string]webproto.PaletteItem{}
	for _, it := range res.Items {
		byID[it.ID] = it
		p, ok := aiActionPolicyFor(it.ID)
		if !ok || p.Gate == aiGateNever {
			t.Errorf("%s offered, but the AI policy is %+v", it.ID, p)
		}
		if p.Gate == aiGateConfirm && p.Class == "" && !it.Ask {
			t.Errorf("%s is destructive and runs without a laptop card", it.ID)
		}
	}
	if it := byID["save-all"]; it.Title != "File: Save All Files" || it.Ask {
		t.Errorf("save-all: %+v", it)
	}
	if it := byID["git-push"]; !it.Ask {
		t.Errorf("git-push runs without the laptop: %+v", it)
	}
	for _, id := range []string{"quit", "reload-window", "open-shell", "quick-open", "git-discard", "web-start"} {
		if _, ok := byID[id]; ok {
			t.Errorf("%s offered to the web", id)
		}
	}
	for id := range webPaletteAllow {
		if !slices.ContainsFunc(paletteItems(), func(it picker.Item) bool { return it.ID == id }) {
			t.Errorf("%s is on the web allow-list but not in the laptop palette", id)
		}
	}
}

// Spec: palette.run runs an allowed command on the laptop at once; an
// unknown id, a command not on the list, or one that needs the laptop's
// yes is refused and nothing runs.
func TestWebPaletteRun(t *testing.T) {
	r := newWebFilesRig(t)
	ran := webPaletteRan(t)
	if f := r.call(webproto.OpPaletteRun, &webproto.PaletteArgs{ID: "save-all"}); f["t"] != "res" || webErrCode(f) != "" {
		t.Fatalf("save-all: %v", f)
	}
	for _, id := range []string{"quit", "nope", "git-push"} {
		if f := r.call(webproto.OpPaletteRun, &webproto.PaletteArgs{ID: id}); webErrCode(f) != "forbidden" {
			t.Errorf("%s: %v", id, f)
		}
	}
	if got := strings.Join(*ran, ","); got != "save-all" {
		t.Errorf("ran %q, want save-all only", got)
	}
}

// Spec: palette.ask puts a laptop card up (Cancel first, naming the
// command and the device); the command runs only after the laptop's yes.
// A command that needs no card is refused here.
func TestWebPaletteAskWaitsForTheLaptop(t *testing.T) {
	r := newWebFilesRig(t)
	ran := webPaletteRan(t)
	var ref webproto.AskRef
	webTestData(t, r.call(webproto.OpPaletteAsk, &webproto.PaletteArgs{ID: "git-push"}), &ref)
	if ref.Card == "" {
		t.Fatal("no card id")
	}
	if len(*ran) != 0 {
		t.Fatal("ran before the laptop said yes")
	}
	if !r.m.confirmOpen || r.m.confirmKind != confirmKindWebAsk || r.m.confirm.SafeID() != "cancel" {
		t.Fatalf("laptop dialog open=%v kind=%v", r.m.confirmOpen, r.m.confirmKind)
	}
	if v := r.m.confirm.View(); !strings.Contains(v, "Git: Push") || !strings.Contains(v, "Requested from") {
		t.Errorf("card:\n%s", v)
	}
	webTestLaptopSelect(t, r.m, "confirm")
	if got := strings.Join(*ran, ","); got != "git-push" {
		t.Errorf("ran %q after the yes", got)
	}
	for _, id := range []string{"save-all", "quit"} {
		if f := r.call(webproto.OpPaletteAsk, &webproto.PaletteArgs{ID: id}); webErrCode(f) != "forbidden" {
			t.Errorf("%s: %v", id, f)
		}
	}
}
