//go:build browser

package web

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The Files tab (WEB-16, web UX 5.4): the laptop's open files first, then
// the workspace tree with lazy folders.

const filesView = `document.querySelector('[data-view="files"]')`

// openFiles logs in on vp and goes to the Files tab; on a desktop it is
// the middle column from the start.
func openFiles(t *testing.T, vp Viewport) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-1")
	if vp.Mobile {
		p.GoTab("files")
		c.Subs(t, "tree", "bufs")
	}
	return p, c
}

// No workspace on the laptop: the hub refuses the tree topic and the tab
// says so in its own words, with Try again, instead of loading forever.
func TestFilesNoWorkspace(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openFiles(t, vp)
			c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, Code: webproto.CodeNotFound, Msg: "No workspace is open.", Data: raw(map[string]any{"topic": "tree"})})
			p.WaitJS(filesView + `.querySelector(".refused")?.innerText.includes("No workspace is open on the laptop.")`)
			p.TapJS(filesView + `.querySelector(".refused button")`)
			c.WaitSub(t, "tree")
			p.WaitJS(filesView + `.querySelector(".loading")`)
		})
	}
}

func entry(name string, dir bool, extra ...string) map[string]any {
	e := map[string]any{"name": name, "dir": dir, "size": 10, "mtime": 1793361600000}
	for _, x := range extra {
		k, v, _ := strings.Cut(x, "=")
		if v == "" {
			e[k] = true
		} else {
			e[k] = v
		}
	}
	return e
}

func treeSnap(hidden bool) map[string]any {
	return map[string]any{"roots": []any{map[string]any{"path": "", "label": "ws-root"}}, "hidden": hidden, "dirs": map[string]any{}}
}

func dirDelta(path string, truncated bool, entries ...map[string]any) map[string]any {
	l := make([]any, len(entries))
	for i, e := range entries {
		l[i] = e
	}
	d := map[string]any{"kind": "dir", "path": path, "entries": l}
	if truncated {
		d["truncated"] = true
	}
	return d
}

// expectList reads a tree.list (or tree.unlist) of path and answers it.
func expectList(t *testing.T, c *HubConn, op, path string) {
	t.Helper()
	r := expectReq(t, c, op)
	var a webproto.PathArgs
	if err := json.Unmarshal(r.Args, &a); err != nil || a.Path != path {
		t.Fatalf("%s args %s, want path %q", op, r.Args, path)
	}
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"path": path, "entries": []any{}})})
}

// setInput sets a text field's value as typing would (an input event).
func setInput(p *Page, el, v string) {
	p.T.Helper()
	Eval[bool](p, `(() => { const el = `+el+`; el.value = `+jsq(v)+`; el.dispatchEvent(new Event("input", { bubbles: true })); return true; })()`)
}

func treeRow(path string) string {
	return filesView + `.querySelector('.tree-row[data-path=` + jsq(path) + `]')`
}

// The tree lists the root when the topic comes, opens a folder on a tap
// (tree.list), closes it again (tree.unlist), shows git letters in the
// git colours and ignored names dim, and puts the laptop's open files
// first; a file opens in the file view.
func TestFilesTreeOpensFoldersAndFiles(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openFiles(t, vp)
			c.Send(t, Snap("bufs", 1, map[string]any{"bufs": []any{map[string]any{"buf": 3, "path": "src/a.go", "dirty": true, "active": true}}}))
			c.Send(t, Snap("tree", 1, treeSnap(false)))
			expectList(t, c, "tree.list", "")
			c.Send(t, Delta("tree", 2, dirDelta("", false, entry("src", true, "git=M"), entry("vendor", true, "ignored"), entry("README.md", false))))
			p.WaitJS(treeRow("README.md"))
			groups := Eval[[]string](p, `[...`+filesView+`.querySelectorAll(".tree-group .group-title")].map((e) => e.textContent)`)
			if len(groups) != 2 || groups[0] != "Open on laptop" || groups[1] != "ws-root" {
				t.Errorf("groups %q, want the laptop's files first, then the tree", groups)
			}
			if !Eval[bool](p, filesView+`.querySelector('.tree-group .tree-row[data-path="src/a.go"] .dirty-dot') !== null`) {
				t.Error("the dirty open file has no dot")
			}
			if got := Eval[string](p, treeRow("src")+`.querySelector(".git-badge.git-mod")?.textContent`); got != "M" {
				t.Errorf("src git badge %q, want M in the modified colour", got)
			}
			if !Eval[bool](p, treeRow("vendor")+`.classList.contains("is-dim")`) {
				t.Error("an ignored folder is not dim")
			}
			if h := Eval[float64](p, treeRow("README.md")+`.getBoundingClientRect().height`); h < 44 {
				t.Errorf("tree rows are %.0f px tall, want at least 44", h)
			}

			p.TapJS(treeRow("src"))
			expectList(t, c, "tree.list", "src")
			c.Send(t, Delta("tree", 3, dirDelta("src", false, entry("b.go", false, "git=?"))))
			p.WaitJS(treeRow("src/b.go") + `?.classList.contains("depth-1")`)
			if got := Eval[string](p, treeRow("src")+`.getAttribute("aria-expanded")`); got != "true" {
				t.Errorf("open folder aria-expanded %q", got)
			}
			p.TapJS(treeRow("src"))
			expectList(t, c, "tree.unlist", "src")
			p.WaitJS(`!` + treeRow("src/b.go"))

			p.TapJS(treeRow("README.md"))
			p.WaitJS(`location.hash === "#/files/README.md"`)
		})
	}
}

// A folder with more than 2 000 names says it shows the first ones and
// offers a filter; an empty folder says so; a folder that cannot be read
// says why with Try again.
func TestFilesTreeTruncatedEmptyAndFailed(t *testing.T) {
	p, c := openFiles(t, Phone)
	c.Send(t, Snap("tree", 1, treeSnap(false)))
	expectList(t, c, "tree.list", "")
	c.Send(t, Delta("tree", 2, dirDelta("", true, entry("big", true), entry("empty", true), entry("locked", true), entry("alpha.txt", false), entry("beta.txt", false))))
	p.WaitJS(filesView + `.querySelector(".tree-note")?.innerText.includes("Showing the first 2 000 names.")`)
	p.TapJS(filesView + `.querySelector(".tree-filter")`)
	p.Key("bet")
	p.WaitJS(`!` + treeRow("alpha.txt") + ` && ` + treeRow("beta.txt"))
	setInput(p, filesView+`.querySelector(".tree-filter")`, "")
	p.WaitJS(treeRow("alpha.txt"))

	p.TapJS(treeRow("empty"))
	expectList(t, c, "tree.list", "empty")
	c.Send(t, Delta("tree", 3, dirDelta("empty", false)))
	p.WaitJS(filesView + `.innerText.includes("This folder is empty.")`)

	p.TapJS(treeRow("locked"))
	r := expectReq(t, c, "tree.list")
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeNotFound, Msg: "That file or folder cannot be read."})
	// not_found means the folder is gone: it closes.
	p.WaitJS(treeRow("locked") + `.getAttribute("aria-expanded") === "false"`)
	p.TapJS(treeRow("locked"))
	r = expectReq(t, c, "tree.list")
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeInternal, Msg: "permission denied"})
	p.WaitJS(filesView + `.querySelector(".tree-err")?.textContent === "Could not read this folder: permission denied"`)
	p.TapJS(filesView + `.querySelector(".tree-retry")`)
	expectList(t, c, "tree.list", "locked")
}

// Dotfiles follow the laptop; when it shows them they can be hidden here.
// A hidden delta from the laptop is followed at once.
func TestFilesDotfilesFollowTheLaptop(t *testing.T) {
	p, c := openFiles(t, Desktop)
	c.Send(t, Snap("tree", 1, treeSnap(true)))
	expectList(t, c, "tree.list", "")
	c.Send(t, Delta("tree", 2, dirDelta("", false, entry(".env.example", false, "hidden"), entry("main.go", false))))
	p.WaitJS(treeRow(".env.example"))
	chip := filesView + `.querySelector(".view-actions .chip")`
	if got := Eval[string](p, chip+`.getAttribute("aria-pressed")`); got != "true" {
		t.Errorf("dotfiles chip pressed %q while the laptop shows them", got)
	}
	p.TapJS(chip)
	p.WaitJS(`!` + treeRow(".env.example") + ` && ` + treeRow("main.go"))
	p.TapJS(chip)
	p.WaitJS(treeRow(".env.example"))
	c.Send(t, Delta("tree", 3, map[string]any{"kind": "hidden", "on": false}))
	c.Send(t, Delta("tree", 4, dirDelta("", false, entry("main.go", false))))
	p.WaitJS(`!` + treeRow(".env.example") + ` && ` + chip + `.getAttribute("aria-pressed") !== "true"`)
	p.TapJS(chip)
	p.WaitJS(`(` + toastsJS + `).includes("The laptop hides dotfiles. Show them there first.")`)
}

// After a new connection the hub has forgotten this page's folders: the
// page lists the root and every open folder again.
func TestFilesTreeListsAgainAfterReconnect(t *testing.T) {
	p, c := openFiles(t, Desktop)
	c.Send(t, Snap("tree", 1, treeSnap(false)))
	expectList(t, c, "tree.list", "")
	c.Send(t, Delta("tree", 2, dirDelta("", false, entry("src", true))))
	p.TapJS(treeRow("src"))
	expectList(t, c, "tree.list", "src")
	c.Send(t, Delta("tree", 3, dirDelta("src", false, entry("a.go", false))))
	p.WaitJS(treeRow("src/a.go"))

	c2, _ := reconnect(t, p, c, "c-2")
	c2.Subs(t, "tree")
	c2.Send(t, Snap("tree", 1, treeSnap(false)))
	got := map[string]bool{}
	for len(got) < 2 {
		r := expectReq(t, c2, "tree.list")
		var a webproto.PathArgs
		_ = json.Unmarshal(r.Args, &a)
		got[a.Path] = true
		c2.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"path": a.Path, "entries": []any{}})})
	}
	if !got[""] || !got["src"] {
		t.Errorf("listed again %v, want the root and src", got)
	}
}

// Names are data, never object keys (T49): folders named __proto__,
// constructor and prototype list, open and close like any other folder,
// and no listing leaks into the prototype of every object on the page.
func TestFilesTreeNamesThatLookLikeObjectKeys(t *testing.T) {
	p, c := openFiles(t, Phone)
	c.Send(t, Snap("tree", 1, treeSnap(false)))
	expectList(t, c, "tree.list", "")
	names := []string{"__proto__", "constructor", "prototype", "hasOwnProperty", "toString"}
	var root []map[string]any
	for _, n := range names {
		root = append(root, entry(n, true))
	}
	c.Send(t, Delta("tree", 2, dirDelta("", false, root...)))
	seq := uint64(3)
	for _, n := range names {
		p.WaitJS(treeRow(n))
		p.TapJS(treeRow(n))
		expectList(t, c, "tree.list", n)
		c.Send(t, Delta("tree", seq, dirDelta(n, false, entry("in-"+n+".txt", false))))
		seq++
		p.WaitJS(treeRow(n+"/in-"+n+".txt") + `?.classList.contains("depth-1")`)
	}
	if got := Eval[string](p, `(() => { const o = {}; return [o.entries, o.truncated, Object.getPrototypeOf(o) === Object.prototype, Object.keys(Object.prototype).length].join(",") })()`); got != ",,true,0" {
		t.Errorf("Object.prototype was changed: %s", got)
	}
	p.TapJS(treeRow("constructor"))
	expectList(t, c, "tree.unlist", "constructor")
	p.WaitJS(`!` + treeRow("constructor/in-constructor.txt"))
	c.Send(t, Delta("tree", seq, map[string]any{"kind": "remove", "path": "__proto__"}))
	p.WaitJS(treeRow("__proto__") + `.getAttribute("aria-expanded") === "false"`)
}
