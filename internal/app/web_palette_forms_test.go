package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (WEBP-9, MCP setup): the MCP setup commands the AI may never run
// (browse the catalog, install a bundle, connect a service, import from
// other apps) are offered to the web user, but only through a laptop
// card: palette.run refuses them; the card (Cancel first) says the form
// opens on the laptop, where sign-ins, keys and tokens are typed; after
// the laptop's yes the laptop's own form opens there. The page never gets
// a field for a key, and nothing secret goes over the link.
func TestWebPaletteOffersMCPSetupOnlyWithALaptopCard(t *testing.T) {
	r := newWebFilesRig(t)
	ran := webPaletteRan(t)
	var res webproto.PaletteList
	webResData(t, r.call(webproto.OpPaletteList, &webproto.EmptyArgs{}), &res)
	byID := map[string]webproto.PaletteItem{}
	for _, it := range res.Items {
		byID[it.ID] = it
	}
	ids := []string{"ai-mcp-catalog", "ai-mcp-bundle", "ai-mcp-connect", "mcp-import-other"}
	for _, id := range ids {
		if it, ok := byID[id]; !ok || !it.Ask || it.Title == "" {
			t.Errorf("%s: offered=%v, it must wait for a laptop card: %+v", id, ok, it)
		}
		if f := r.call(webproto.OpPaletteRun, &webproto.PaletteArgs{ID: id}); webErrCode(f) != "forbidden" {
			t.Errorf("%s ran without the laptop: %v", id, f)
		}
		if p, _ := aiActionPolicyFor(id); p.Gate != aiGateNever {
			t.Errorf("%s: the AI may run it now (%+v); it needs no form entry", id, p)
		}
	}
	for _, id := range []string{"ai-credentials", "ai-configure", "ai-sign-in-claude"} {
		if _, ok := byID[id]; ok {
			t.Errorf("%s offered to the web", id)
		}
	}
	var ref webproto.AskRef
	webTestData(t, r.call(webproto.OpPaletteAsk, &webproto.PaletteArgs{ID: "ai-mcp-connect"}), &ref)
	if ref.Card == "" || len(*ran) != 0 {
		t.Fatalf("card %q, ran %v before the laptop's yes", ref.Card, *ran)
	}
	if r.m.confirm.SafeID() != "cancel" {
		t.Fatal("the card's safe choice is not Cancel")
	}
	a := r.m.web.asks.shown
	if a == nil {
		t.Fatal("no laptop card shows")
	}
	for _, want := range []string{"Connect a Service", "typed here, on the laptop", "never on the web"} {
		if !strings.Contains(a.title+" "+a.body, want) {
			t.Errorf("the card misses %q: %q %q", want, a.title, a.body)
		}
	}
	webTestLaptopSelect(t, r.m, "confirm")
	if got := strings.Join(*ran, ","); got != "ai-mcp-connect" {
		t.Errorf("ran %q after the yes", got)
	}
}
