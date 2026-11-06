package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (WEBP-9): snippets.save adds (or updates) one of the user's
// snippets in snippets.json, snippets.user lists them by scope and
// trigger, snippets.delete removes one.
func TestWebUserSnippets(t *testing.T) {
	r := newWebFilesRig(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var saved webproto.SnippetSaved
	webResData(t, r.call(webproto.OpSnippetsSave, &webproto.SnippetSaveArgs{Scope: "go", Trigger: "iferr", Body: "if err != nil {\n\treturn $1\n}"}), &saved)
	if saved.Replaced {
		t.Error("a new snippet says it replaced one")
	}
	webResData(t, r.call(webproto.OpSnippetsSave, &webproto.SnippetSaveArgs{Scope: "global", Trigger: "todo", Body: "// TODO"}), &saved)
	webResData(t, r.call(webproto.OpSnippetsSave, &webproto.SnippetSaveArgs{Scope: "go", Trigger: "iferr", Body: "if err != nil { return err }"}), &saved)
	if !saved.Replaced {
		t.Error("an update does not say so")
	}
	var list webproto.UserSnippetList
	webResData(t, r.call(webproto.OpSnippetsUser, &webproto.EmptyArgs{}), &list)
	if len(list.Items) != 2 || list.Items[0].Scope != "*" || list.Items[0].Trigger != "todo" || list.Items[1].Scope != "go" || list.Items[1].Body != "if err != nil { return err }" {
		t.Fatalf("list %+v", list.Items)
	}
	if f := r.call(webproto.OpSnippetsSave, &webproto.SnippetSaveArgs{Scope: "go", Trigger: "if-err", Body: "x"}); webErrCode(f) != "bad_request" {
		t.Errorf("a trigger that never expands: %v", f)
	}
	if f := r.call(webproto.OpSnippetsDelete, &webproto.SnippetRefArgs{Scope: "go", Trigger: "iferr"}); f["t"] != "res" {
		t.Fatal(f)
	}
	webResData(t, r.call(webproto.OpSnippetsUser, &webproto.EmptyArgs{}), &list)
	if len(list.Items) != 1 {
		t.Fatalf("after delete %+v", list.Items)
	}
	if f := r.call(webproto.OpSnippetsDelete, &webproto.SnippetRefArgs{Scope: "go", Trigger: "iferr"}); webErrCode(f) != "not_found" {
		t.Errorf("a snippet that is gone: %v", f)
	}
}
