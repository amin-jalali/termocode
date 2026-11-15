package assets

import (
	"io/fs"
	"strings"
	"testing"
)

// budgetRadial is the floating radial menu (radial.js, RM-2), gzipped:
// the package in web/packages/radial-menu and termocode's actions. Only
// a touch phone or tablet loads it, after the page.
const (
	budgetRadial    = 16 << 10
	budgetRadialCSS = 3 << 10
)

// Spec (RM-2): the radial is radial.js and radial.css alone; the page's
// first files only load it (lib/radial.ts), it imports nothing (the page
// gives it its ops), the desktop's workbench and index.html never name it.
func TestRadialChunkStandsAlone(t *testing.T) {
	for _, f := range []string{"radial.js", "radial.css"} {
		if _, err := fs.Stat(FS(), f); err != nil {
			t.Fatalf("no %s: %v", f, err)
		}
	}
	js, page := readBuilt(t, "radial.js"), readPage(t)
	for _, bad := range []string{"import ", "import(", "app-chunk"} {
		if strings.Contains(js, bad) {
			t.Errorf("radial.js imports something (%q): the page gives it what it needs", bad)
		}
	}
	if !strings.Contains(js, "/assets/radial.css") || !strings.Contains(js, "rm-fab") {
		t.Error("radial.js does not link radial.css or draw the button")
	}
	if !strings.Contains(page, `import("/assets/radial.js")`) {
		t.Error("the page does not load /assets/radial.js")
	}
	if strings.Contains(page, "rm-fab") || strings.Contains(page, "git.stash_q\",") {
		t.Error("the page's first files carry the radial's own code")
	}
	for _, f := range []string{"index.html", "workbench.js"} {
		if strings.Contains(readBuilt(t, f), "radial.js") {
			t.Errorf("%s names radial.js: only a touch phone or tablet loads it", f)
		}
	}
	if n := gzSize(t, "radial.js"); n > budgetRadial {
		t.Errorf("radial.js is %d B gzipped, budget %d B", n, budgetRadial)
	}
	if n := gzSize(t, "radial.css"); n > budgetRadialCSS {
		t.Errorf("radial.css is %d B gzipped, budget %d B", n, budgetRadialCSS)
	}
}

// radialHarness is a fake page for radial/actions.ts: it records every op
// the radial calls.
const radialHarness = `
import { categories, rings, run, DEFAULT_FAVORITES, RS } from "../radial/actions";
const sig = (v) => ({ value: v, peek() { return this.value; }, subscribe() { return () => {}; } });
const calls = [];
globalThis.document = { createElement: () => ({ className: "" }), body: { appendChild() {} } };
const host = {
  h: (c, p) => ({ c, p }),
  render: (v) => { if (v) calls.push(["ask", v.p.label, v.p.text]), v.p.send(); },
  AskLaptop: () => null,
  go: (r) => calls.push(["go", r.tab + "/" + r.rest + (r.query.sheet ? "?sheet=" + r.query.sheet : "")]),
  route: sig({ tab: "chat", rest: "", query: {} }),
  layout: sig("phone"),
  link: { meta: sig({ role: "controller" }), request: (op, a) => (calls.push(["req", op]), Promise.resolve({})) },
  live: sig(true),
  git: () => ({ repo: true, branch: { name: "main" }, files: [1] }),
  t: (k, v) => k + (v && v.branch ? ":" + v.branch : ""),
  toast: (s, m) => calls.push(["toast", m]),
  addRef: (r) => (calls.push(["ref", r.kind + ":" + r.path]), "added"),
  newChat: () => calls.push(["newChat"]),
  newTerm: () => calls.push(["newTerm"]),
  edit: (p) => calls.push(["edit", p]),
  showOnLaptop: (p) => calls.push(["laptop", p]),
};
const fail = (m) => { throw new Error(m); };
const item = (cs, id) => cs.flatMap((c) => c.items).find((i) => i.id === id) ?? fail("no item " + id);
const take = () => { const c = calls.splice(0); return JSON.stringify(c); };
`

// The rings: SAVED's defaults exist, at most 8 actions a ring, none
// empty, every action runs, ids unique; the phone-only views leave a
// tablet's rings.
func TestRadialRings(t *testing.T) {
	out := runJS(t, radialHarness+`
const phone = categories(host, "phone");
if (phone.map((c) => c.id).join() !== "edit,ai,git,run,view,tools") fail("rings " + phone.map((c) => c.id));
const ids = new Set();
for (const c of phone) {
  if (c.items.length === 0 || c.items.length > 8) fail(c.id + " has " + c.items.length);
  for (const i of c.items) {
    if (typeof i.run !== "function" || !i.label || !i.icon || !i.detail) fail("dead or bare item " + i.id);
    if (ids.has(i.id)) fail("twice: " + i.id);
    ids.add(i.id);
  }
}
for (const f of DEFAULT_FAVORITES) item(phone, f);
const tablet = categories(host, "tablet").flatMap((c) => c.items).map((i) => i.id);
if (tablet.includes("view-home") || tablet.includes("ai-review")) fail("a tablet has the phone's Home or review");
console.log("rings ok", ids.size);
`, nil)
	if !strings.Contains(out, "rings ok") {
		t.Fatal(out)
	}
}

// Every action calls the page's own op: views open their routes, push
// and stash ask first with the git view's words and op, a branch opens
// the remote sheet, new chat and new shell call the page's functions,
// the open file goes to the chat as a reference; a viewer and an offline
// page change nothing.
func TestRadialActionsCallThePagesOps(t *testing.T) {
	out := runJS(t, radialHarness+`
const cs = categories(host, "phone");
const want = (id, w) => { item(cs, id).run(); const got = take(); if (got !== JSON.stringify(w)) fail(id + ": " + got + " want " + JSON.stringify(w)); };
want("view-term", [["go", "term/"]]);
want("tools-db", [["go", "more/db"]]);
want("run-tests", [["go", "more/testing"]]);
want("git-push", [["ask", "git.remote", "git.push_q:main"], ["req", "git.push"]]);
want("git-pull", [["ask", "git.remote", "git.pull_q:main"], ["req", "git.pull"]]);
want("git-stash", [["ask", "git.remote", "git.stash_q"], ["req", "git.stash"]]);
want("git-branch", [["go", "git/?sheet=remote"]]);
want("ai-new", [["newChat"]]);
want("run-newterm", [["newTerm"]]);
want("edit-ask", [["toast", RS.noFile]]);
host.route.value = { tab: "files", rest: "cmd/main.go", query: {} };
want("edit-ask", [["ref", "file:cmd/main.go"], ["go", "chat/"]]);
want("edit-edit", [["edit", "cmd/main.go"]]);
want("edit-laptop", [["laptop", "cmd/main.go"]]);
host.git = () => ({ repo: false, branch: { name: "" }, files: [] });
want("git-push", [["go", "git/?sheet=remote"]]);
host.link.meta.value = { role: "viewer" };
want("git-pull", [["toast", RS.viewOnly]]);
want("view-git", [["go", "git/"]]);
host.link.meta.value = { role: "controller" };
host.live.value = false;
want("ai-new", [["toast", RS.offline]]);
console.log("ops ok");
`, nil)
	if !strings.Contains(out, "ops ok") {
		t.Fatal(out)
	}
}
