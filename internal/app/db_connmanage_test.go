package app

import "testing"

func TestDBManagePaletteItems(t *testing.T) {
	want := map[string]string{
		"db-manage-connection": "Databases: Manage Connection…",
		"db-paste-connection":  "Databases: Paste Connection from Clipboard",
		"db-new-from-template": "Databases: New Connection from Template…",
	}
	for _, it := range databasesPaletteItems() {
		if w, ok := want[it.ID]; ok {
			if it.Title != w {
				t.Errorf("%s title %q", it.ID, it.Title)
			}
			if _, ok := aiActionPolicies[it.ID]; !ok {
				t.Errorf("%s has no AI policy", it.ID)
			}
			delete(want, it.ID)
		}
	}
	if len(want) > 0 {
		t.Fatalf("missing %v", want)
	}
}

// The manage menu: every action, Delete last; a project connection can
// only be copied (its file belongs to the project).
func TestDBManageItems(t *testing.T) {
	ids := func(user bool) []string {
		var out []string
		for _, it := range dbManageItems(user) {
			out = append(out, it.ID)
		}
		return out
	}
	user := ids(true)
	for _, w := range []string{"edit", "comment", "rename", "duplicate", "copy-json", "copy-ssl", "ssh-profile", "save-ssh-profile", "save-template", "version", "delete"} {
		found := false
		for _, id := range user {
			found = found || id == w
		}
		if !found {
			t.Errorf("user menu misses %s: %v", w, user)
		}
	}
	if user[len(user)-1] != "delete" {
		t.Errorf("delete is not last: %v", user)
	}
	for _, id := range ids(false) {
		if id == "delete" || id == "edit" || id == "rename" {
			t.Errorf("project menu offers %s", id)
		}
	}
}
