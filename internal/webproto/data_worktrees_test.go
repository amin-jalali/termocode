package webproto

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (WTX-16): the worktrees topic exists (v2); refresh is a read;
// pin, sleep and note need a controller; archive and delete wait for a
// laptop card. A worktree is named by its id (12 hex digits), never by a
// path the page could make up.
func TestWorktreesTopicAndRows(t *testing.T) {
	if tp, err := ParseTopic("worktrees"); err != nil || tp.Name != "worktrees" {
		t.Fatalf("worktrees: %v %+v", err, tp)
	}
	want := map[string]struct {
		role Role
		card bool
	}{OpWorktreesRefresh: {RoleViewer, false}, OpWorktreesPin: {RoleController, false}, OpWorktreesSleep: {RoleController, false},
		OpWorktreesNote: {RoleController, false}, OpWorktreesArchive: {RoleController, true}, OpWorktreesDelete: {RoleController, true}}
	for op, w := range want {
		p, ok := Lookup(op)
		if !ok || p.Role != w.role || p.LaptopCard != w.card || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v", op, p)
		}
	}
	for _, a := range []string{`{"id":""}`, `{"id":"../etc"}`, `{"id":"/src/app"}`, `{"id":"0123456789ABCDEF"}`, `{"id":"0123456789ab","path":"/x"}`} {
		if _, err := DecodeArgs(OpWorktreesArchive, json.RawMessage(a)); err == nil {
			t.Errorf("archive %s: accepted", a)
		}
	}
	if _, err := DecodeArgs(OpWorktreesPin, json.RawMessage(`{"id":"0123456789ab","on":true}`)); err != nil {
		t.Errorf("pin: %v", err)
	}
	if _, err := DecodeArgs(OpWorktreesNote, json.RawMessage(`{"id":"0123456789ab","text":"`+strings.Repeat("x", MaxWorktreeNote+1)+`"}`)); err == nil {
		t.Error("note: too long")
	}
	if _, err := DecodeArgs(OpWorktreesNote, json.RawMessage(`{"id":"0123456789ab","text":"a\nb"}`)); err == nil {
		t.Error("note: two lines")
	}
	if _, err := DecodeArgs(OpWorktreesNote, json.RawMessage(`{"id":"0123456789ab","text":"tests pass"}`)); err != nil {
		t.Errorf("note: %v", err)
	}
}

// The topic's wire form is pinned by testdata/golden/topics/worktrees.json.
func TestWorktreesSnapGolden(t *testing.T) {
	snap := WorktreesSnap{State: "ready", Worktrees: []WorktreeItem{
		{ID: "a1b2c3d4e5f6", Title: "feat/wt-sidebar", Branch: "feat/wt-sidebar", Where: "~/src/app.worktrees/wt-sidebar", Kind: "user",
			Group: "pinned", State: "working", Pinned: true, Agent: "Claude Code", Agents: 1, Status: true, Ahead: 3, Dirty: 4,
			Note: "adding the pin row", Checkpoint: "working", Link: "#412", LastSeen: 1_700_000_000_000},
		{ID: "0f0e0d0c0b0a", Title: "main", Branch: "main", Where: "~/src/app", Kind: "main", Group: "active", State: "idle", Here: true, Status: true},
		{ID: "112233445566", Title: "spike/grpc", Branch: "spike/grpc", Where: "~/src/app.worktrees/grpc", Kind: "user", Group: "asleep", State: "asleep",
			Parent: "a1b2c3d4e5f6", Depth: 1},
		{ID: "665544332211", Title: "feat/old-ui", Branch: "feat/old-ui", Where: "~/src/app.worktrees/old-ui", Kind: "user", Group: "archived", State: "archived", Gone: true},
	}}
	got, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join("testdata", "golden", "topics", "worktrees.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bytes.TrimSuffix(want, []byte("\n"))) {
		t.Errorf("worktrees topic changed\n got %s\nwant %s", got, want)
	}
}
