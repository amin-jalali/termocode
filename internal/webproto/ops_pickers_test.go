package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (WEBP-9, Editor and files): the desktop's pickers read the
// laptop's own lists: its bookmarks (nvim's global marks: toggled on a
// row, cleared after a question here), its recent files, the language
// servers' workspace symbols for a query, and the snippets of a file's
// type. Reads are open to viewers; a change needs a controller.
func TestPickerOps(t *testing.T) {
	rows := map[string]struct {
		role  Role
		audit Audit
		args  string
	}{
		OpBookmarksToggle:     {RoleController, AuditNo, `{"buf":2,"line":4}`},
		OpBookmarksList:       {RoleViewer, AuditNo, `{}`},
		OpBookmarksClear:      {RoleController, AuditYes, `{}`},
		OpFilesRecent:         {RoleViewer, AuditNo, `{}`},
		OpLspWorkspaceSymbols: {RoleViewer, AuditNo, `{"buf":2,"query":"Hand"}`},
		OpSnippetsList:        {RoleViewer, AuditNo, `{"buf":2}`},
	}
	for op, w := range rows {
		p, ok := Lookup(op)
		if !ok || p.Role != w.role || p.Audit != w.audit || p.LaptopCard || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v", op, p)
		}
		if _, err := DecodeArgs(op, json.RawMessage(w.args)); err != nil {
			t.Errorf("%s %s: %v", op, w.args, err)
		}
		if _, ok := ResultOf(op); !ok && op != OpBookmarksClear {
			t.Errorf("%s has no result type", op)
		}
	}
	for op, bad := range map[string][]string{
		OpBookmarksToggle:     {`{"buf":0,"line":1}`, `{"buf":2,"line":-1}`},
		OpLspWorkspaceSymbols: {`{"buf":2,"query":"` + strings.Repeat("x", MaxSymbolQuery+1) + `"}`, `{"buf":2,"query":"a\u0000"}`, `{"buf":0,"query":"a"}`},
		OpSnippetsList:        {`{"buf":0}`},
	} {
		for _, a := range bad {
			if _, err := DecodeArgs(op, json.RawMessage(a)); err == nil {
				t.Errorf("%s %s: accepted", op, a)
			}
		}
	}
}
