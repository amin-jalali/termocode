//go:build unix

package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestViewBlock decodes view.run's answer.
func webTestViewBlock(t *testing.T, f map[string]any) webproto.ViewRunResult {
	t.Helper()
	if f["t"] == "err" {
		t.Fatalf("view.run refused: %v", f)
	}
	var r webproto.ViewRunResult
	webTestData(t, f, &r)
	return r
}

// webTestViewTick waits for the live views' tick and runs it as Update.
func webTestViewTick(t *testing.T, m *Model) {
	t.Helper()
	select {
	case msg := <-m.web.ch:
		tick, ok := msg.(webViewTickMsg)
		if !ok {
			t.Fatalf("hub message %T, want the live views' tick", msg)
		}
		m.handleWebMsg(tick)
	case <-time.After(5 * time.Second):
		t.Fatal("no live views tick")
	}
}

// Spec (MOB-9): view.run of a topic view answers one block built from
// the topic's snapshot, with the spec applied on the laptop.
func TestViewRunTopicView(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	webTestToolCard(&m, ai.Tool{Name: "write_file"})
	c := webTestSub(t, &m)
	f, _ := webTestCall(t, &m, c, webproto.OpViewRun, `{"spec":{"view":"cards"}}`)
	r := webTestViewBlock(t, f)
	b := r.Block
	if b.Kind != "list" || b.Source != "cards" || b.Total != 1 || len(b.Items) != 1 || !strings.HasPrefix(b.ID, "v-") || r.Live != "" {
		t.Fatalf("block %+v", b)
	}
	if !strings.Contains(b.Items[0].Title, "AI wants to") || b.Items[0].Ref == nil || b.Items[0].Ref.Kind != "card" {
		t.Errorf("item %+v", b.Items[0])
	}
	if err := b.Validate(); err != nil {
		t.Error(err)
	}
}

// Spec (MOB-9): a spec with an unknown field is refused with the allowed
// ids; an unknown view is not_found.
func TestViewRunRefusesUnknownField(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	f, _ := webTestCall(t, &m, c, webproto.OpViewRun, `{"spec":{"view":"cards","fields":["nope"]}}`)
	if webTestErr(f) != "bad_request" || !strings.Contains(f["msg"].(string), "title, kind, when, laptop") {
		t.Fatalf("got %v", f)
	}
	f, _ = webTestCall(t, &m, c, webproto.OpViewRun, `{"spec":{"view":"no.such"}}`)
	if webTestErr(f) != "not_found" {
		t.Fatalf("unknown view: %v", f)
	}
}

// Spec (MOB-9): a live view re-runs when its topic publishes (one tick,
// coalesced) and sends ev view_update with the new block in place; it
// sends nothing when the block did not change; view.stop ends it.
func TestViewLiveUpdatesInPlace(t *testing.T) {
	old := webViewCoalesce
	webViewCoalesce = time.Millisecond
	t.Cleanup(func() { webViewCoalesce = old })
	m := aiTestModel(t)
	webTestHub(t, &m)
	webTestToolCard(&m, ai.Tool{Name: "write_file"})
	c := webTestSub(t, &m)
	f, _ := webTestCall(t, &m, c, webproto.OpViewRun, `{"spec":{"view":"cards"},"live":true}`)
	r := webTestViewBlock(t, f)
	if !strings.HasPrefix(r.Live, "lv-") || r.Block.ID != r.Live || !r.Block.Live {
		t.Fatalf("live %q block %+v", r.Live, r.Block)
	}
	if !m.web.webListens("cards") {
		t.Fatal("the cards publisher does not run for the live view")
	}

	m.web.publish("cards", struct{}{}) // nothing changed
	webTestViewTick(t, &m)
	for _, fr := range webTestNow(t, c) {
		if fr["kind"] == webproto.EventViewUpdate {
			t.Fatalf("an update with no change: %v", fr)
		}
	}

	m.ai.confirm, m.confirmOpen = nil, false // the card was answered
	m.web.publish("cards", struct{}{})
	m.web.publish("cards", struct{}{}) // coalesced: one tick
	webTestViewTick(t, &m)
	var up webproto.ViewUpdate
	n := 0
	for _, fr := range webTestNow(t, c) {
		if fr["kind"] == webproto.EventViewUpdate {
			webTestData(t, fr, &up)
			n++
		}
	}
	if n != 1 || up.ID != r.Live || up.Block.ID != r.Live || up.Block.Total != 0 {
		t.Fatalf("%d updates, %+v", n, up)
	}
	select {
	case msg := <-m.web.ch:
		t.Fatalf("a second tick %T for two publishes", msg)
	case <-time.After(20 * time.Millisecond):
	}

	if f, _ := webTestCall(t, &m, c, webproto.OpViewStop, `{"id":"`+r.Live+`"}`); f["t"] == "err" {
		t.Fatal(f)
	}
	if len(m.web.views.live) != 0 || m.web.webListens("cards") {
		t.Fatal("view.stop left the live view or its topic running")
	}
}

// Spec (MOB-9): at most 8 live views per connection; they end with the
// connection.
func TestViewLiveCapAndEndWithConnection(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	for i := range webproto.MaxLiveViews {
		if f, _ := webTestCall(t, &m, c, webproto.OpViewRun, `{"spec":{"view":"chats"},"live":true}`); f["t"] == "err" {
			t.Fatalf("live view %d: %v", i, f)
		}
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpViewRun, `{"spec":{"view":"chats"},"live":true}`); webTestErr(f) != "limit" {
		t.Fatalf("a ninth live view: %v", f)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpViewRun, `{"spec":{"view":"chats"}}`); f["t"] == "err" {
		t.Fatalf("a one-time run past the cap: %v", f)
	}
	m.onWebGone(webGoneMsg{hub: m.web, conn: c})
	if len(m.web.views.live) != 0 {
		t.Fatalf("%d live views left after the connection", len(m.web.views.live))
	}
}

// Spec (MOB-9): an op-backed view runs its source op through that op's
// own handler (same role, same audit) and applies the spec to its result.
func TestViewRunOpView(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	_, run := webTestRepo(t, &m)
	run("tag", "v1")
	run("tag", "v2")
	webTestGitLoad(&m)
	c := webTestSub(t, &m)
	f := webTestCallBg(t, &m, c, webproto.OpViewRun, `{"spec":{"view":"git.tags","sort":[{"field":"name","desc":true}]}}`)
	b := webTestViewBlock(t, f).Block
	if b.Kind != "list" || b.Total != 2 || len(b.Items) != 2 || b.Items[0].Title != "v2" || b.Items[1].Title != "v1" {
		t.Fatalf("block %+v", b)
	}
}

// Spec (MOB-9): view.catalog lists the features and the views this role
// may run.
func TestViewCatalog(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	c.role = webproto.RoleViewer
	f, _ := webTestCall(t, &m, c, webproto.OpViewCatalog, `{}`)
	var cat webproto.ViewCatalog
	webTestData(t, f, &cat)
	ids := map[string]bool{}
	for _, d := range cat.Views {
		ids[d.ID] = true
		if d.Role != webproto.RoleViewer {
			t.Errorf("viewer catalog lists %s (%s)", d.ID, d.Role)
		}
	}
	for _, id := range []string{"git.status", "pr.list", "runs", "inbox", "projects", "mcp.status"} {
		if !ids[id] {
			t.Errorf("catalog misses %s", id)
		}
	}
	if len(cat.Features) == 0 || cat.Features[0] != "git" {
		t.Errorf("features %v", cat.Features)
	}
}
