package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Image generation (CMP-97): an agent tool, off by default, that asks an
// OpenAI-compatible images endpoint for a PNG and saves it in the
// workspace. Every call asks first (it costs money): no "Always allow".

func imageGenModel(t *testing.T, kind, base string) (*Model, string) {
	t.Helper()
	withConfigDir(t)
	m := aiTestModel(t)
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	t.Chdir(dir)
	m.ai.slotFake = map[string]aiSlot{ai.SlotMain: {cfg: ai.Config{Kind: kind, APIKey: "sk-test", BaseURL: base}}}
	m.ai.provider = fakeAIProvider{} // a set-up provider: AI palette entries need one
	return &m, dir
}

func noCall(_ context.Context, _ aiIDEFunc) (string, error) {
	return "", nil
}

func TestImageGenOffByDefault(t *testing.T) {
	m, dir := imageGenModel(t, ai.KindOpenAI, "http://127.0.0.1:1")
	if tools := m.aiImageTools(dir, noCall); len(tools) != 0 {
		t.Fatalf("tools %v while off", tools)
	}
	found := false
	for _, e := range aiPaletteItems() {
		if e.id == "ai-image-gen" && e.title == "AI: Toggle Image Generation" {
			found = true
		}
	}
	if !found {
		t.Fatal("palette entry missing")
	}
	if p, ok := aiActionPolicies["ai-image-gen"]; !ok || p.Gate != aiGateNever {
		t.Fatalf("policy %+v ok=%v: the AI must not turn on a paid tool itself", p, ok)
	}
	if _, ok := m.dispatchAIPalette("ai-image-gen"); !ok {
		t.Fatal("palette id not handled")
	}
	if !loadAIImageSettings().On {
		t.Fatal("toggle did not save ai_image_gen")
	}
	tools := m.aiImageTools(dir, noCall)
	if len(tools) != 1 || tools[0].Name != "generate_image" {
		t.Fatalf("tools %v after toggle", tools)
	}
	if tools[0].Mutating != true || tools[0].Class != "" {
		t.Fatalf("mutating %v class %q: every call must ask, with no Always allow", tools[0].Mutating, tools[0].Class)
	}
	m.dispatchAIPalette("ai-image-gen")
	if loadAIImageSettings().On {
		t.Fatal("second toggle did not turn it off")
	}
}

func TestImageGenNeedsImageProvider(t *testing.T) {
	m, dir := imageGenModel(t, ai.KindAnthropic, "")
	if err := saveAIImageSettings(aiImageSettings{On: true}); err != nil {
		t.Fatal(err)
	}
	if tools := m.aiImageTools(dir, noCall); len(tools) != 0 {
		t.Fatalf("tool offered for a provider without an images API: %v", tools)
	}
}

func TestImageGenToolSavesPNG(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(png)}}})
	}))
	defer srv.Close()
	m, dir := imageGenModel(t, ai.KindOpenAI, srv.URL)
	if err := saveAIImageSettings(aiImageSettings{On: true, Model: "img-x"}); err != nil {
		t.Fatal(err)
	}
	called := false
	call := func(_ context.Context, _ aiIDEFunc) (string, error) {
		called = true
		return "", nil
	}
	tool := m.aiImageTools(dir, call)[0]
	args := map[string]any{"prompt": "a logo", "path": "assets/logo.png"}
	if err := tool.Validate(args); err != nil {
		t.Fatal(err)
	}
	if d := tool.Human(args); !strings.Contains(d, "assets/logo.png") || !strings.Contains(d, "paid") {
		t.Errorf("card line %q", d)
	}
	res, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "assets/logo.png")); string(b) != string(png) {
		t.Fatalf("file %q", b)
	}
	if !strings.Contains(res, "assets/logo.png") || !called {
		t.Errorf("result %q called %v", res, called)
	}
	if got["model"] != "img-x" || got["prompt"] != "a logo" {
		t.Errorf("request %v", got)
	}
	// an existing file is never overwritten; only .png; never outside the workspace
	for _, bad := range []map[string]any{
		{"prompt": "x", "path": "assets/logo.png"},
		{"prompt": "x", "path": "a.jpg"},
		{"prompt": "x", "path": "../out.png"},
		{"prompt": "", "path": "b.png"},
	} {
		if err := tool.Validate(bad); err == nil {
			t.Errorf("validate %v: no error", bad)
		}
	}
}
