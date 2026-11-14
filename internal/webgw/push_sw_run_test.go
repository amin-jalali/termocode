//go:build unix

package webgw

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// swHarness runs sw.js in node with a fake worker scope and prints what
// it did as JSON.
const swHarness = `
const fs = require("fs");
const handlers = {};
const out = { shown: null, opened: null, posted: [], focused: false };
const clientsList = JSON.parse(process.argv[3]);
const step = JSON.parse(process.argv[4]);
global.self = {
  location: { origin: "https://box.ts.net", search: step.search || "" },
  navigator: { language: step.lang || "en-US" },
  addEventListener: (t, f) => { handlers[t] = f; },
  skipWaiting() {}, clients: {
    claim() {},
    matchAll: async () => clientsList.map((u) => ({ url: u, focus() { out.focused = true; return Promise.resolve(); }, postMessage: (m) => out.posted.push(m) })),
    openWindow: async (u) => { out.opened = u; },
  },
  registration: { showNotification: async (title, o) => { out.shown = { title, body: o.body, data: o.data }; } },
};
eval(fs.readFileSync(process.argv[2], "utf8"));
(async () => {
  let p;
  if (step.push !== undefined) {
    handlers.push({ data: step.push === null ? null : { json: () => step.push }, waitUntil: (x) => (p = x) });
  } else {
    handlers.notificationclick({ notification: { close() {}, data: step.data }, waitUntil: (x) => (p = x) });
  }
  await p;
  console.log(JSON.stringify(out));
})();
`

type swOut struct {
	Shown *struct {
		Title string
		Body  string
		Data  struct{ Open string }
	}
	Opened  string
	Posted  []map[string]string
	Focused bool
}

func runSW(t *testing.T, clients []string, step map[string]any) swOut {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node")
	}
	dir := t.TempDir()
	h := filepath.Join(dir, "h.js")
	sw := filepath.Join(dir, "sw.js")
	if err := os.WriteFile(h, []byte(swHarness), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sw, swJS, 0o600); err != nil {
		t.Fatal(err)
	}
	if clients == nil {
		clients = []string{}
	}
	cj, _ := json.Marshal(clients)
	sj, _ := json.Marshal(step)
	b, err := exec.Command(node, h, sw, string(cj), string(sj)).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, b)
	}
	var o swOut
	if err := json.Unmarshal(b, &o); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	return o
}

// Spec (CMP-47): a push names the session in the title and the kind in
// the body (fixed words); a done or failed push says so; the data says
// where a tap leads.
func TestServiceWorkerShowsSessionAndKind(t *testing.T) {
	o := runSW(t, nil, map[string]any{"push": map[string]any{"n": 1, "k": "plan", "s": "shop"}})
	if o.Shown == nil || o.Shown.Title != "termocode: shop" || o.Shown.Body != "A plan waits for your approval." || o.Shown.Data.Open != "cards" {
		t.Errorf("plan: %+v", o.Shown)
	}
	o = runSW(t, nil, map[string]any{"push": map[string]any{"n": 0, "k": "done", "s": "shop"}})
	if o.Shown == nil || o.Shown.Body != "The AI is done." || o.Shown.Data.Open != "" {
		t.Errorf("done: %+v", o.Shown)
	}
	o = runSW(t, nil, map[string]any{"push": map[string]any{"n": 0, "k": "failed"}})
	if o.Shown == nil || o.Shown.Title != "termocode" || o.Shown.Body != "A run did not finish." {
		t.Errorf("failed: %+v", o.Shown)
	}
	// The session name is plain text: a hostile one is cut to the safe set.
	o = runSW(t, nil, map[string]any{"push": map[string]any{"n": 1, "s": "<img src=x onerror=1>"}})
	if o.Shown == nil || o.Shown.Title != "termocode" {
		t.Errorf("hostile name: %+v", o.Shown)
	}
	o = runSW(t, nil, map[string]any{"push": nil})
	if o.Shown == nil || o.Shown.Body != "Something needs you." {
		t.Errorf("no data: %+v", o.Shown)
	}
}

// Spec (CMP-47): a tap on a card push opens the panel on the cards: an
// open page is focused and told, a closed one is opened with ?open=cards.
func TestServiceWorkerTapOpensTheCard(t *testing.T) {
	o := runSW(t, []string{"https://box.ts.net/"}, map[string]any{"data": map[string]any{"open": "cards"}})
	if !o.Focused || len(o.Posted) != 1 || o.Posted[0]["open"] != "cards" || o.Opened != "" {
		t.Errorf("open page: %+v", o)
	}
	o = runSW(t, nil, map[string]any{"data": map[string]any{"open": "cards"}})
	if o.Opened != "/?open=cards" {
		t.Errorf("closed page: %+v", o)
	}
	o = runSW(t, nil, map[string]any{"data": map[string]any{}})
	if o.Opened != "/" {
		t.Errorf("done push: %+v", o)
	}
	o = runSW(t, []string{"https://evil.example/"}, map[string]any{"data": map[string]any{"open": "cards"}})
	if o.Focused || o.Opened != "/?open=cards" {
		t.Errorf("foreign window: %+v", o)
	}
}

// Spec (CMP-47, NEW-9): an AI question push says so in fixed words and a
// tap leads to the cards; the worker speaks Persian when the page
// registered it with ?l=fa (or, with no l, the browser is Persian), and
// English when the page asked for it.
func TestServiceWorkerAIQuestionAndPersian(t *testing.T) {
	o := runSW(t, nil, map[string]any{"push": map[string]any{"n": 1, "k": "ask"}})
	if o.Shown == nil || o.Shown.Body != "The AI has a question for you." || o.Shown.Data.Open != "cards" {
		t.Errorf("ask: %+v", o.Shown)
	}
	o = runSW(t, nil, map[string]any{"push": map[string]any{"n": 1, "k": "ask"}, "search": "?l=fa"})
	if o.Shown == nil || o.Shown.Body != "هوش مصنوعی از شما سؤالی دارد." {
		t.Errorf("ask fa: %+v", o.Shown)
	}
	o = runSW(t, nil, map[string]any{"push": map[string]any{"n": 3}, "search": "?l=fa"})
	if o.Shown == nil || o.Shown.Body != "۳ چیز منتظر شماست." {
		t.Errorf("count fa: %+v", o.Shown)
	}
	o = runSW(t, nil, map[string]any{"push": map[string]any{"n": 0, "k": "done"}, "lang": "fa-IR"})
	if o.Shown == nil || o.Shown.Body != "کار هوش مصنوعی تمام شد." {
		t.Errorf("done fa by browser: %+v", o.Shown)
	}
	o = runSW(t, nil, map[string]any{"push": map[string]any{"n": 1, "k": "plan"}, "search": "?l=en", "lang": "fa-IR"})
	if o.Shown == nil || o.Shown.Body != "A plan waits for your approval." {
		t.Errorf("page asked for English: %+v", o.Shown)
	}
}
