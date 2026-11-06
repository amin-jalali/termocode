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
		if _, user := webPaletteUserOnly[it.ID]; !ok || p.Gate == aiGateNever && !user {
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

// Spec (web parity, AI chat and agents): the user's own AI commands that
// the AI may never run (they change the user's AI setup, spend the
// user's tokens or start work that pushes) are offered to the web, the
// web user being the user: each is on webPaletteUserOnly with its reason,
// and each that changes a setting, spends money or touches files waits
// for a laptop card (Cancel first). The harmless MCP ones run at once.
// Other never commands (configure the provider, delete chats) are not
// offered.
func TestWebPaletteOffersTheUsersAICommands(t *testing.T) {
	r := newWebFilesRig(t)
	var res webproto.PaletteList
	webResData(t, r.call(webproto.OpPaletteList, &webproto.EmptyArgs{}), &res)
	byID := map[string]webproto.PaletteItem{}
	for _, it := range res.Items {
		byID[it.ID] = it
	}
	ask := []string{"ai-until-done", "ai-plan-approval", "ai-reasoning-next", "ai-keep-going", "ai-edit-review", "ai-worktrees-toggle",
		"ai-memory-toggle", "ai-undo-turn", "ai-apply-code", "ai-handoff", "ai-ship", "ai-plan-worktree"}
	for _, id := range ask {
		if it, ok := byID[id]; !ok || !it.Ask {
			t.Errorf("%s: offered=%v, it must wait for a laptop card: %+v", id, ok, it)
		}
	}
	for _, id := range []string{"ai-mcp-background", "ai-mcp-reset-cache", "ai-mcp-updates", "ai-mcp-reconnect-all"} {
		if it, ok := byID[id]; !ok || it.Ask {
			t.Errorf("%s: offered=%v, it runs at once: %+v", id, ok, it)
		}
	}
	for _, id := range []string{"ai-configure", "ai-history-delete", "ai-credentials", "ai-mcp-connect"} {
		if _, ok := byID[id]; ok {
			t.Errorf("%s offered to the web", id)
		}
	}
	for id, why := range webPaletteUserOnly {
		if p, _ := aiActionPolicyFor(id); p.Gate != aiGateNever {
			t.Errorf("%s is user-only but the AI policy is %+v (put it on webPaletteAllow alone)", id, p)
		}
		if _, ok := webPaletteAllow[id]; !ok || len(why) < 12 {
			t.Errorf("%s: user-only needs a webPaletteAllow entry and a reason, got %q", id, why)
		}
	}
	ran := webPaletteRan(t)
	if f := r.call(webproto.OpPaletteRun, &webproto.PaletteArgs{ID: "ai-ship"}); webErrCode(f) != "forbidden" {
		t.Errorf("ai-ship ran without a card: %v", f)
	}
	if len(*ran) != 0 {
		t.Fatalf("ran %v", *ran)
	}
}
