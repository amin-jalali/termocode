package app

import (
	"os"
	"strings"
	"testing"
)

// Coverage of snippets_user.go: the manager flow on a real nvim model with a
// clean snippets.json in the sandboxed config dir.

func cleanSnippets(t *testing.T) string {
	t.Helper()
	p, err := userSnippetsPath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(p)
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

func TestCovSnippetManagerFlow(t *testing.T) {
	path := cleanSnippets(t)
	m := newTestModel(t)
	m.w, m.h = 120, 40
	if m.nvim == nil {
		t.Skip("no nvim")
	}
	m.setupUserSnippets()
	// empty: the three action rows
	m.openSnippetManager()
	if !m.pickerOpen || m.pickerKind != pickerKindSnippetManager {
		t.Fatal("manager not open")
	}
	// New: step 1 asks the name, step 2 the body
	m.onSnippetManagerSelected(snipMgrNew)
	if !m.promptOpen || m.promptKind != promptKindSnippetNew {
		t.Fatal("name prompt")
	}
	if m.onSnippetNewSubmit("bad name!") == nil {
		t.Error("bad trigger toasts")
	}
	m.onSnippetNewSubmit("go:errw")
	if m.promptKind != promptKindSnippetBody {
		t.Fatal("body prompt")
	}
	m.onSnippetBodySubmit(`if err != nil {\n\treturn err\n}`)
	m.onSnippetNewSubmit("*:todo")
	m.onSnippetBodySubmit("// TODO $0")
	// replacing an existing one says "updated"; blank bodies are ignored
	m.onSnippetNewSubmit("go:errw")
	m.onSnippetBodySubmit("new body")
	m.onSnippetNewSubmit("go:errw") // seeds the prompt with the old body
	if m.onSnippetBodySubmit("   ") != nil {
		t.Error("blank body")
	}
	if m.onSnippetBodySubmit("x") != nil { // no pending trigger
		t.Error("no pending trigger")
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"errw": "new body"`) || !strings.Contains(string(b), "todo") {
		t.Fatalf("file: %s", b)
	}
	// manager lists them, cycles the scope filter, edits and deletes
	m.openSnippetManager()
	m.onSnippetManagerSelected(snipMgrScope)
	if m.extras.snipScope == "" {
		t.Error("filter should move to the first scope")
	}
	m.onSnippetManagerSelected(snipMgrNew) // default scope = the filter
	m.onSnippetManagerSelected(snipMgrFile)
	m.onSnippetManagerSelected("garbage")
	m.onSnippetManagerSelected(snipMgrPrefix + "no-separator")
	m.onSnippetManagerSelected(snipMgrPrefix + "go\x00errw")
	if !m.confirmOpen || m.confirmKind != confirmKindSnippetAction {
		t.Fatal("snippet card")
	}
	m.onSnippetActionConfirm("edit")
	m.extras.snipTarget = snippetRef{Scope: "go", Trigger: "errw"}
	if m.onSnippetActionConfirm("delete") == nil {
		t.Error("delete toasts")
	}
	m.extras.snipTarget = snippetRef{Scope: "go", Trigger: "errw"}
	if m.onSnippetActionConfirm("delete") != nil { // already gone
		t.Error("deleting twice")
	}
	if m.onSnippetActionConfirm("cancel") != nil {
		t.Error("cancel")
	}
	// a stale filter is dropped when its scope has no snippets left
	m.extras.snipScope = "go"
	m.openSnippetManager()
	if m.extras.snipScope != "" {
		t.Error("stale filter")
	}
	// invalid JSON on disk
	_ = os.WriteFile(path, []byte("{bad"), 0o644)
	if m.openSnippetManager() == nil {
		t.Error("bad JSON toasts")
	}
	if m.onSnippetNewSubmit("go:x"); m.promptKind != promptKindSnippetBody {
		t.Error("still asks for the body")
	}
	if m.onSnippetBodySubmit("b") == nil {
		t.Error("bad JSON on save toasts")
	}
	m.extras.snipTarget = snippetRef{Scope: "go", Trigger: "x"}
	if m.onSnippetActionConfirm("delete") == nil {
		t.Error("bad JSON on delete toasts")
	}
	// nothing breaks without an editor
	var e Model
	e.setupUserSnippets()
	e.reloadUserSnippets()
	e.jumpToSnippetInBuffer(snippetRef{Trigger: "x"})
	if e.currentFiletype() != "" {
		t.Error("no filetype without an editor")
	}
}

func TestCovSnippetHelpers(t *testing.T) {
	if nextSnippetScope(nil, "") != "" || nextSnippetScope([]string{"*", "go"}, "") != "*" ||
		nextSnippetScope([]string{"*", "go"}, "*") != "go" || nextSnippetScope([]string{"*", "go"}, "go") != "" {
		t.Error("scope cycling")
	}
	u := userSnippets{}
	if u.remove(snippetRef{Scope: "go", Trigger: "x"}) {
		t.Error("missing scope")
	}
	u.set("go", "a", "1")
	if u.remove(snippetRef{Scope: "go", Trigger: "zz"}) {
		t.Error("missing trigger")
	}
	u.set("python", "b", "line1\nline2")
	u.set("all", "c", "3")
	if len(u.list("go")) != 1 || len(u.list("")) != 3 || u.scopes()[0] != "*" {
		t.Errorf("list %+v scopes %v", u.list(""), u.scopes())
	}
	if s := string(u.marshal()); !strings.Contains(s, `"line1"`) {
		t.Errorf("multi-line bodies are arrays: %s", s)
	}
	back, err := parseUserSnippets(u.marshal())
	if err != nil || back["python"]["b"] != "line1\nline2" {
		t.Errorf("round trip %v %v", back, err)
	}
	if got, _ := parseUserSnippets([]byte("  ")); len(got) != 0 {
		t.Error("blank file")
	}
	if _, err := parseUserSnippets([]byte("{bad")); err == nil {
		t.Error("bad json")
	}
	got, _ := parseUserSnippets([]byte(`{"go":{"ok":"x","bad name":"y","num":5,"arr":["a","b"],"badarr":[1]}}`))
	if len(got["go"]) != 2 || got["go"]["arr"] != "a\nb" {
		t.Errorf("parsed %v", got)
	}
	if _, _, err := parseScopeTrigger("go:ok"); err != nil {
		t.Error(err)
	}
	if _, _, err := parseScopeTrigger("a b:ok"); err == nil {
		t.Error("scope with a space")
	}
	if s, tr, _ := parseScopeTrigger("todo"); s != "*" || tr != "todo" {
		t.Errorf("bare trigger %q %q", s, tr)
	}
	if unescapeBodyInput(`a\nb\tc\\d\$e\`) != "a\nb\tc\\d\\$e\\" {
		t.Errorf("unescape %q", unescapeBodyInput(`a\nb\tc\\d\$e\`))
	}
	if luaLongString("a]]b") == "[[\na]]b]]" {
		t.Error("long string level must avoid the content")
	}
	// load: missing file, then a directory in the way
	p := cleanSnippets(t)
	if s, err := loadUserSnippets(); err != nil || len(s) != 0 {
		t.Errorf("missing file: %v %v", s, err)
	}
	_ = os.MkdirAll(p, 0o755)
	if _, err := loadUserSnippets(); err == nil {
		t.Error("a directory is a read error")
	}
	if saveUserSnippets(userSnippets{}) == nil {
		t.Error("cannot write over a directory")
	}
	_ = os.Remove(p)
}
