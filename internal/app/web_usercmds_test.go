package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// webUserCmdsRig is a web rig with its own config folder.
func webUserCmdsRig(t *testing.T) (*webFilesRig, string) {
	t.Helper()
	r := newWebFilesRig(t)
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	return r, filepath.Join(cfg, "termocode", "commands.json")
}

// Spec (WEBP-7): usercmds.list sends the user's commands (commands.json)
// in the file's order; no file is an empty list, not an error.
func TestWebUserCmdsList(t *testing.T) {
	r, path := webUserCmdsRig(t)
	var list webproto.UserCmdList
	webResData(t, r.call(webproto.OpUserCmdsList, &webproto.EmptyArgs{}), &list)
	if len(list.Items) != 0 || list.Error != "" {
		t.Fatalf("no file: %+v", list)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`[{"id":"b","title":"Build","cmd":"go build ./..."},{"id":"w","title":"Web","cmd":"npm run dev","cwd":"web"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	webResData(t, r.call(webproto.OpUserCmdsList, &webproto.EmptyArgs{}), &list)
	if len(list.Items) != 2 || list.Items[0] != (webproto.UserCmd{ID: "b", Title: "Build", Cmd: "go build ./..."}) || list.Items[1].CWD != "web" {
		t.Errorf("list %+v", list.Items)
	}
}

// Spec (WEBP-7): usercmds.save writes the whole list to commands.json
// (the laptop's palette and Run Task… show it at once: User: <title>)
// and answers the list as saved. A commands.json that does not parse is
// never written over: the list says why, and a save is refused.
func TestWebUserCmdsSave(t *testing.T) {
	r, path := webUserCmdsRig(t)
	items := []webproto.UserCmd{{ID: "t", Title: "Test", Cmd: "go test ./..."}, {ID: "l", Title: "Lint", Cmd: "golangci-lint run", CWD: "sub"}}
	var list webproto.UserCmdList
	webResData(t, r.call(webproto.OpUserCmdsSave, &webproto.UserCmdSaveArgs{Items: items}), &list)
	if len(list.Items) != 2 || list.Items[1] != items[1] {
		t.Errorf("reply %+v", list.Items)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved []UserCommand
	if err := json.Unmarshal(data, &saved); err != nil || len(saved) != 2 || saved[0].Cmd != "go test ./..." || saved[1].CWD != "sub" {
		t.Errorf("saved %s (%v)", data, err)
	}
	found := false
	for _, tk := range userCommandTasks() {
		if tk.Label == "User: Lint" && tk.Command == "golangci-lint run" {
			found = true
		}
	}
	if !found {
		t.Errorf("the tasks do not list User: Lint: %+v", userCommandTasks())
	}
	webResData(t, r.call(webproto.OpUserCmdsSave, &webproto.UserCmdSaveArgs{Items: []webproto.UserCmd{}}), &list)
	if data, _ := os.ReadFile(path); string(data) != "[]\n" {
		t.Errorf("an empty list saved %q", data)
	}

	if err := os.WriteFile(path, []byte(`[{"id": broken`), 0o644); err != nil {
		t.Fatal(err)
	}
	webResData(t, r.call(webproto.OpUserCmdsList, &webproto.EmptyArgs{}), &list)
	if list.Error == "" || len(list.Items) != 0 {
		t.Errorf("a broken file lists %+v", list)
	}
	if f := r.call(webproto.OpUserCmdsSave, &webproto.UserCmdSaveArgs{Items: items}); webErrCode(f) != "conflict" {
		t.Errorf("a save over a broken file: %v", f)
	}
	if data, _ := os.ReadFile(path); string(data) != `[{"id": broken` {
		t.Errorf("the broken file was written over: %q", data)
	}
}
