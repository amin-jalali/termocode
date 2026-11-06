package app

import (
	"sort"
	"testing"

	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webKeysRig is a web rig with the default keymap and its own config.
func webKeysRig(t *testing.T) *webFilesRig {
	t.Helper()
	r := newWebFilesRig(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	r.m.keys = keymap.Default()
	return r
}

// webKeysOther is a default key bound to an action other than not.
func webKeysOther(t *testing.T, not keymap.Action) (string, keymap.Action) {
	t.Helper()
	var keys []string
	for k := range keymap.Default().Bindings() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if a := keymap.Default().Bindings()[k]; a != not && a != keymap.ActionNone {
			return k, a
		}
	}
	t.Fatal("the default keymap binds nothing")
	return "", keymap.ActionNone
}

// Spec (WEBP-7): keys.list names every laptop action (sorted by name, as
// Preferences: Customize Keybindings lists them) with the keys bound to
// it now.
func TestWebKeysList(t *testing.T) {
	r := webKeysRig(t)
	var list webproto.KeyBindings
	webResData(t, r.call(webproto.OpKeysList, &webproto.EmptyArgs{}), &list)
	all := keymap.AllActions()
	if len(list.Items) != len(all) || list.Items[0].Action != keymap.ActionName(all[0]) {
		t.Fatalf("%d rows, want %d: %+v", len(list.Items), len(all), list.Items[:1])
	}
	key, owner := webKeysOther(t, keymap.ActionNone)
	found := false
	for _, it := range list.Items {
		if it.Action == keymap.ActionName(owner) {
			for _, k := range it.Keys {
				found = found || k == key
			}
		}
	}
	if !found {
		t.Errorf("%s is not listed under %s", key, keymap.ActionName(owner))
	}
}

// Spec (WEBP-7): keys.bind saves a key for an action in keymap.json and
// the laptop uses it at once. A key another action has is not taken: the
// answer names that action, and only a second bind with replace takes
// it. A key the laptop cannot read and an unknown action are refused.
func TestWebKeysBind(t *testing.T) {
	r := webKeysRig(t)
	var done webproto.KeyBound
	webResData(t, r.call(webproto.OpKeysBind, &webproto.KeyBindArgs{Action: "PickTheme", Key: "ctrl+alt+t"}), &done)
	if done.Conflict != "" || !done.Saved || keymap.LoadOverrides("")["ctrl+alt+t"] != keymap.ActionPickTheme || r.m.keys.Bindings()["ctrl+alt+t"] != keymap.ActionPickTheme {
		t.Fatalf("bind: %+v, saved %v, live %v", done, keymap.LoadOverrides("")["ctrl+alt+t"], r.m.keys.Bindings()["ctrl+alt+t"])
	}
	key, owner := webKeysOther(t, keymap.ActionPickTheme)
	done = webproto.KeyBound{}
	webResData(t, r.call(webproto.OpKeysBind, &webproto.KeyBindArgs{Action: "PickTheme", Key: key}), &done)
	if done.Conflict != keymap.ActionName(owner) || done.Saved || r.m.keys.Bindings()[key] != owner {
		t.Fatalf("a key in use was taken without replace: %+v, live %v", done, r.m.keys.Bindings()[key])
	}
	done = webproto.KeyBound{}
	webResData(t, r.call(webproto.OpKeysBind, &webproto.KeyBindArgs{Action: "PickTheme", Key: key, Replace: true}), &done)
	if !done.Saved || r.m.keys.Bindings()[key] != keymap.ActionPickTheme {
		t.Fatalf("replace: %+v, live %v", done, r.m.keys.Bindings()[key])
	}
	if f := r.call(webproto.OpKeysBind, &webproto.KeyBindArgs{Action: "PickTheme", Key: "hyper+q"}); webErrCode(f) != "bad_request" {
		t.Errorf("a key the laptop cannot read: %v", f)
	}
	if f := r.call(webproto.OpKeysBind, &webproto.KeyBindArgs{Action: "NoSuchAction", Key: "f6"}); webErrCode(f) != "not_found" {
		t.Errorf("an unknown action: %v", f)
	}
}
