package app

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Spec (3.3, T42): the AI can never start the web panel, make a link or
// stop it: execute_action refuses every web palette id before it runs,
// and nothing starts (no gateway, no start card, no link).
func TestAICannotStartWebPanel(t *testing.T) {
	m := aiTestModel(t)
	writeUserConfig(t, `{"web_panel":"on"}`)
	started := 0
	old := webGatewayStart
	webGatewayStart = func(*webHub, []string) (*webGatewayProc, error) { started++; return nil, context.Canceled }
	defer func() { webGatewayStart = old }()
	var call aiIDECaller = func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(&m)
		return out, err
	}
	var exec *ai.Tool
	for _, tl := range m.aiChatTools(t.TempDir(), call) {
		if tl.Name == "execute_action" {
			tl := tl
			exec = &tl
		}
	}
	if exec == nil {
		t.Fatal("no execute_action tool")
	}
	for _, it := range webPaletteItems() {
		args := map[string]any{"id": it.ID}
		if err := exec.Validate(args); err == nil {
			t.Errorf("%s passed validation", it.ID)
		}
		if _, err := exec.Run(context.Background(), args); err == nil {
			t.Errorf("%s ran", it.ID)
		}
	}
	if started != 0 || m.webPanel != nil || m.web != nil || m.confirmOpen {
		t.Fatalf("the AI reached the web panel: started=%d panel=%v hub=%v card=%v", started, m.webPanel, m.web != nil, m.confirmOpen)
	}
}

// Spec (3.3): web cards (the start card, laptop cards) are answered only
// by the user: their answer functions are reached from the confirm
// router alone, which only a confirm dialog's SelectMsg (a key or a
// click) and Esc reach; the web may only cancel a laptop card. No file
// of the AI or of the MCP server calls them, or starts the panel.
func TestAICannotAnswerWebCards(t *testing.T) {
	allowed := map[string][]string{
		"answerWebAsk":        {"ai_config_ui.go"}, // the confirm router (and Esc)
		"answerWebStart":      {"ai_config_ui.go"},
		"handleAIConfirm":     {"confirm.go"},
		"handleConfirmSelect": {"update.go"}, // confirm.SelectMsg only
		"onAIConfirmClosed":   {"update.go"}, // confirm.CloseMsg (Esc) only
		"webAskDone":          {"web_ask.go", "web_cards_answer.go"},
		"webAskOpen":          {"web_ask.go", "web_call.go"},
		"startWebPanel":       {"web_panel.go", "web_tui.go"},
		"webNewLink":          {"web_panel.go", "web_tui.go"},
		"openWebStartCard":    {"web_panel.go", "web_tui.go"},
		"webCtlOpen":          {"web_panel.go", "web_tui.go"},
		"dispatchWebPalette":  {"palette.go"}, // the AI's execute_action is refused before it (aiNever)
		// Pairing and devices (web_access.go): the owner's buttons only.
		"handleWebAccessConfirm":   {"ai_config_ui.go"},
		"handleWebAccessPicker":    {"ai_config_ui.go"},
		"answerWebPairAsk":         {"web_access.go"},
		"answerWebPairQR":          {"web_access.go"},
		"answerWebDeviceRemove":    {"web_access.go"},
		"openWebPair":              {"web_access.go"},
		"webRevokeDevice":          {"web_access.go"},
		"answerWebGrant":           {"web_access.go"},
		"webMakeViewer":            {"web_access.go"},
		"dispatchWebAccessPalette": {"palette.go"},
		"openWebTotpSetup":         {"web_access.go"},
		"webTotpNew":               {"webtotp_setup.go"},
	}
	fset := token.NewFileSet()
	files, _ := filepath.Glob("*.go")
	got := map[string]map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if _, watch := allowed[sel.Sel.Name]; watch {
					if got[sel.Sel.Name] == nil {
						got[sel.Sel.Name] = map[string]bool{}
					}
					got[sel.Sel.Name][f] = true
				}
			}
			return true
		})
	}
	for name, ok := range allowed {
		var bad []string
		for f := range got[name] {
			if !slices.Contains(ok, f) {
				bad = append(bad, f)
			}
		}
		sort.Strings(bad)
		if len(bad) > 0 {
			t.Errorf("%s is reached from %v; only %v may", name, bad, ok)
		}
	}
}
