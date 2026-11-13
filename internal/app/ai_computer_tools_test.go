package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/computeruse"
)

// fakeDesktop records the actions instead of doing them.
type fakeDesktop struct{ did []string }

func (f *fakeDesktop) Name() string { return "fake desktop" }
func (f *fakeDesktop) Screenshot(_ context.Context, p string) error {
	f.did = append(f.did, "shot "+p)
	return os.WriteFile(p, []byte("\x89PNG"), 0o600)
}
func (f *fakeDesktop) Click(_ context.Context, x, y int, b string) error {
	f.did = append(f.did, "click "+b)
	return nil
}
func (f *fakeDesktop) Type(_ context.Context, s string) error {
	f.did = append(f.did, "type "+s)
	return nil
}
func (f *fakeDesktop) Key(_ context.Context, k string) error {
	f.did = append(f.did, "key "+k)
	return nil
}

func setComputerUse(t *testing.T, on bool) {
	t.Helper()
	p, err := ai.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	body := `{}`
	if on {
		body = `{"ai_computer_use": true}`
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAIComputerToolsOffByDefault(t *testing.T) {
	m, _, root, _ := aiSkillsModel(t)
	call := func(context.Context, aiIDEFunc) (string, error) { return "", nil }
	for _, n := range []string{"computer_screenshot", "computer_click", "computer_type", "computer_key"} {
		if _, ok := ai.FindTool(m.aiChatTools(root, call), n); ok {
			t.Fatalf("%s is there with ai_computer_use off", n)
		}
	}
}

func TestAIComputerToolsAlwaysAsk(t *testing.T) {
	m, _, root, _ := aiSkillsModel(t)
	setComputerUse(t, true)
	desk := &fakeDesktop{}
	old := aiComputerDriver
	t.Cleanup(func() { aiComputerDriver = old })
	aiComputerDriver = func() (computeruse.Driver, error) { return desk, nil }
	call := func(context.Context, aiIDEFunc) (string, error) { return "", nil }
	tools := m.aiChatTools(root, call)
	for _, n := range []string{"computer_screenshot", "computer_click", "computer_type", "computer_key"} {
		tl, ok := ai.FindTool(tools, n)
		if !ok {
			t.Fatalf("%s missing with ai_computer_use on", n)
		}
		// Mutating with no class: a card every time, no "Always allow".
		if !tl.Mutating || tl.Class != "" || tl.ReadOnly {
			t.Errorf("%s: mutating=%v class=%q readonly=%v", n, tl.Mutating, tl.Class, tl.ReadOnly)
		}
	}
	ctx := context.Background()
	shot, _ := ai.FindTool(tools, "computer_screenshot")
	out, err := shot.Run(ctx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, ".termocode/screens/") || !strings.Contains(out, "look_at") {
		t.Fatalf("screenshot result: %q", out)
	}
	if ents, _ := os.ReadDir(filepath.Join(root, ".termocode", "screens")); len(ents) != 1 {
		t.Fatalf("screens: %v", ents)
	}
	click, _ := ai.FindTool(tools, "computer_click")
	if err := click.Validate(map[string]any{"x": -5.0, "y": 1.0}); err == nil {
		t.Fatal("a point off the screen passed")
	}
	if d := click.Describe(map[string]any{"x": 10.0, "y": 20.0}); !strings.Contains(d, "10, 20") {
		t.Fatalf("card text: %q", d)
	}
	if _, err := click.Run(ctx, map[string]any{"x": 10.0, "y": 20.0}); err != nil {
		t.Fatal(err)
	}
	typ, _ := ai.FindTool(tools, "computer_type")
	if err := typ.Validate(map[string]any{"text": "a\x1b"}); err == nil {
		t.Fatal("control text passed")
	}
	if _, err := typ.Run(ctx, map[string]any{"text": "hi"}); err != nil {
		t.Fatal(err)
	}
	key, _ := ai.FindTool(tools, "computer_key")
	if err := key.Validate(map[string]any{"keys": "--help"}); err == nil {
		t.Fatal("option-looking keys passed")
	}
	if _, err := key.Run(ctx, map[string]any{"keys": "ctrl+s"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(desk.did[1:], "|") != "click left|type hi|key ctrl+s" {
		t.Fatalf("did: %q", desk.did)
	}
}

func TestAIComputerUseNeverSetByTheAI(t *testing.T) {
	p, ok := aiSettingPolicies["ai_computer_use"]
	if !ok || p.Gate != aiGateNever {
		t.Fatalf("the AI must never turn on its own computer use: %+v %v", p, ok)
	}
}
