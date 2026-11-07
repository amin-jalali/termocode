package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/blocks"
)

// mdText is all the text of a markdown AST, in order.
func mdText(n *blocks.MdNode) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(n.Text)
	for i := range n.C {
		b.WriteString(" " + mdText(&n.C[i]))
	}
	return b.String()
}

// Spec (MOB-15): the ai topic tells the phone whether the laptop's web
// tools are on, so the Research chip is off (and says why) while both
// are off. It names the search provider, never its key.
func TestResearchAISnapWebTools(t *testing.T) {
	m, _, _, _ := aiSkillsModel(t)
	aiWebFake(t)
	if s := m.webAIBuild(); s.WebFetch || s.WebSearch != "" {
		t.Fatalf("web tools on by default: %+v", s)
	}
	m.onAIWebToolsPicked(aiWebRowFetch)
	if s := m.webAIBuild(); !s.WebFetch || s.WebSearch != "" {
		t.Fatalf("fetch on: %+v", s)
	}
	m.onAIWebToolsPicked(aiWebRowSearchPrefix + "brave")
	m.promptOpen = false
	m.onAIWebKeyEntered("sk-" + "testwebkey12345")
	s := m.webAIBuild()
	if s.WebSearch != "Brave Search" {
		t.Fatalf("search provider %q", s.WebSearch)
	}
	if strings.Contains(s.WebSearch+s.Provider+s.Model, "testwebkey") {
		t.Fatal("the key reached the ai topic")
	}
	m.onAIWebToolsPicked(aiWebRowSearchPrefix + "off")
	m.onAIWebToolsPicked(aiWebRowFetch)
	if s := m.webAIBuild(); s.WebFetch || s.WebSearch != "" {
		t.Fatalf("web tools still on: %+v", s)
	}
}

// Spec (MOB-15): "research X" on the phone runs the laptop's web_search
// against the provider (here a fake endpoint) and the phone gets a
// sources list: title, site badge, snippet, and the url as a ref so a
// tap opens it after the page's confirm. web_fetch gives a text block
// cut at 16 KiB with "Open on laptop".
func TestResearchE2EFakeSearch(t *testing.T) {
	m, _, root, _ := aiSkillsModel(t)
	srv := aiWebFake(t)
	m.onAIWebToolsPicked(aiWebRowFetch)
	m.onAIWebToolsPicked(aiWebRowSearchPrefix + "brave")
	m.promptOpen = false
	m.onAIWebKeyEntered("sk-" + "testwebkey12345")
	call := func(context.Context, aiIDEFunc) (string, error) { return "", nil }
	tools := m.aiChatTools(root, call)

	ws, ok := ai.FindTool(tools, "web_search")
	if !ok || ws.View == nil {
		t.Fatal("web_search has no phone view")
	}
	args := map[string]any{"query": "foo"}
	out, err := ws.Run(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	b := ws.View(args, out)
	if b == nil || b.Kind != blocks.KindList || b.Source != "" || len(b.Items) != 1 {
		t.Fatalf("sources block %+v", b)
	}
	if err := viewFilled(b).Validate(); err != nil {
		t.Fatal(err)
	}
	it := b.Items[0]
	if it.Title != "Foo docs" || it.Badge != "foo.example" || !strings.Contains(it.Sub, "all about Foo") {
		t.Errorf("source row %+v", it)
	}
	if it.Ref == nil || it.Ref.Kind != researchRefURL || it.Ref.ID != "https://foo.example" {
		t.Errorf("source ref %+v", it.Ref)
	}
	if strings.Contains(out, "testwebkey") {
		t.Error("the key is in the tool text")
	}

	wf, ok := ai.FindTool(tools, "web_fetch")
	if !ok || wf.View == nil {
		t.Fatal("web_fetch has no phone view")
	}
	args = map[string]any{"url": srv.URL + "/doc"}
	out, err = wf.Run(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	b = wf.View(args, out)
	if b == nil || b.Kind != blocks.KindText || b.Source != "" || b.Title != "API" {
		t.Fatalf("page block %+v", b)
	}
	if err := viewFilled(b).Validate(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mdText(b.MD), "Use Foo()") {
		t.Errorf("page text %q", mdText(b.MD))
	}
	if b.Ref == nil || b.Ref.Kind != researchRefURL || !strings.HasSuffix(b.Ref.ID, "/doc") {
		t.Errorf("page ref %+v", b.Ref)
	}
	var laptop bool
	for _, a := range b.Actions {
		laptop = laptop || (a.Kind == blocks.ActLaptop && a.Ref != nil && a.Ref.Kind == researchRefURL)
	}
	if !laptop {
		t.Errorf("no Open on laptop: %+v", b.Actions)
	}
}

// Spec (MOB-15): a long page is cut at 16 KiB of text on the phone and
// says so; a url longer than a ref id gets no ref (no tap), never a cut
// url that opens the wrong page.
func TestResearchViewsCut(t *testing.T) {
	long := strings.Repeat("word ", 8000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, long)
	}))
	t.Cleanup(srv.Close)
	out := "Long\n" + srv.URL + "/long (text/plain)\n---\n" + long
	b := researchFetchView(nil, out)
	if b == nil || !b.Truncated {
		t.Fatalf("not cut: %+v", b)
	}
	if n := len(mdText(b.MD)); n > researchMaxText+64 {
		t.Errorf("text %d bytes, max %d", n, researchMaxText)
	}
	if err := viewFilled(b).Validate(); err != nil {
		t.Fatal(err)
	}

	u := "https://foo.example/" + strings.Repeat("a", 200)
	b = researchSearchView(map[string]any{"query": "q"}, "1. Long one\n   "+u+"\n   text")
	if b == nil || len(b.Items) != 1 {
		t.Fatalf("block %+v", b)
	}
	if b.Items[0].Ref != nil {
		t.Errorf("a long url got a ref %+v", b.Items[0].Ref)
	}
	if err := viewFilled(b).Validate(); err != nil {
		t.Error(err)
	}
	if researchSearchView(map[string]any{"query": "q"}, "1. Bad\n   javascript:alert(1)\n").Items[0].Ref != nil {
		t.Error("a non-http url got a ref")
	}
}

// viewFilled is b as the agent sends it: a view leaves its id and source
// to the agent (toolBlock), so the test fills them before Validate.
func viewFilled(b *blocks.Block) *blocks.Block {
	c := *b
	c.ID, c.Source = "view", "tool"
	return &c
}
