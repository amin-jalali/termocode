package webproto

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// Spec (MOB-5): chat.send carries up to 8 attaches by reference; each
// kind names its item with its own fields only, and anything else
// (content, an absolute path, a ".." part, a bad spec) is refused.
func TestChatSendAttachRules(t *testing.T) {
	send := func(attach string) string { return `{"text":"look","attach":[` + attach + `]}` }
	good := []string{
		`{"kind":"file","path":"a/b.go","label":"b.go"}`,
		`{"kind":"lines","path":"a.go","from":3,"to":3}`,
		`{"kind":"lines","path":"a.go","from":1,"to":2000}`,
		`{"kind":"diff"}`,
		`{"kind":"diff","path":"internal/x.go"}`,
		`{"kind":"ptask","path":"docs/TASKS.md","id":"MOB-5"}`,
		`{"kind":"task","id":"go build ./..."}`,
		`{"kind":"issue","id":"12"}`,
		`{"kind":"pr","id":"!7"}`,
		`{"kind":"worktree","id":"wt-1"}`,
		`{"kind":"run","id":"r-1"}`,
		`{"kind":"race","id":"race-3"}`,
		`{"kind":"workflow","id":"nightly"}`,
		`{"kind":"card","id":"card-9"}`,
		`{"kind":"resource","id":"file:///notes/today.md"}`,
		`{"kind":"block","id":"b-1"}`,
		`{"kind":"project","id":"termocode"}`,
		`{"kind":"view","id":"git.changes","spec":"{\"view\":\"git.changes\",\"limit\":20}"}`,
		`{"kind":"view","spec":"{}"}`,
	}
	for _, a := range good {
		if _, err := DecodeArgs(OpChatSend, json.RawMessage(send(a))); err != nil {
			t.Errorf("%s refused: %v", a, err)
		}
	}
	bad := []string{
		`{"kind":"secret","id":"x"}`,
		`{"kind":""}`,
		`{"kind":"file"}`,
		`{"kind":"file","path":"/etc/passwd"}`,
		`{"kind":"file","path":"../up.txt"}`,
		`{"kind":"file","path":"a.go","id":"x"}`,
		`{"kind":"file","path":"a.go","from":1,"to":2}`,
		`{"kind":"file","path":"a.go","text":"content"}`,
		`{"kind":"lines","path":"a.go"}`,
		`{"kind":"lines","path":"a.go","from":5,"to":4}`,
		`{"kind":"lines","path":"a.go","from":1,"to":2001}`,
		`{"kind":"lines","path":"a.go","from":-1,"to":2}`,
		`{"kind":"diff","path":"../x"}`,
		`{"kind":"ptask","id":"MOB-5"}`,
		`{"kind":"ptask","path":"TASKS.md"}`,
		`{"kind":"task"}`,
		`{"kind":"task","id":"a\u0007"}`,
		`{"kind":"issue","id":"12","path":"a.go"}`,
		`{"kind":"run","id":"` + strings.Repeat("r", MaxID+1) + `"}`,
		`{"kind":"card","id":"c","spec":"{}"}`,
		`{"kind":"resource","id":"` + strings.Repeat("u", MaxAttachURI+1) + `"}`,
		`{"kind":"view"}`,
		`{"kind":"view","spec":"[1,2]"}`,
		`{"kind":"view","spec":"{not json"}`,
		`{"kind":"view","spec":"{}","path":"a.go"}`,
		`{"kind":"view","spec":"{\"a\":\"` + strings.Repeat("x", MaxAttachSpec) + `\"}"}`,
		`{"kind":"file","path":"a.go","label":"` + strings.Repeat("l", MaxAttachLabel+1) + `"}`,
		`{"kind":"file","path":"a.go","label":"a\nb"}`,
	}
	for _, a := range bad {
		_, err := DecodeArgs(OpChatSend, json.RawMessage(send(a)))
		if err == nil {
			t.Errorf("%s accepted", a)
		}
	}
	nine := strings.TrimSuffix(strings.Repeat(`{"kind":"diff"},`, MaxAttach+1), ",")
	if _, err := DecodeArgs(OpChatSend, json.RawMessage(send(nine))); !errors.Is(err, ErrInvalid) {
		t.Errorf("%d attaches: %v", MaxAttach+1, err)
	}
	eight := strings.TrimSuffix(strings.Repeat(`{"kind":"diff"},`, MaxAttach), ",")
	if _, err := DecodeArgs(OpChatSend, json.RawMessage(send(eight))); err != nil {
		t.Errorf("%d attaches: %v", MaxAttach, err)
	}
}

// Spec (MOB-5): AttachKinds is the enum of Attach.Kind, in its order.
func TestAttachKindsMatchEnum(t *testing.T) {
	f, ok := reflect.TypeOf(Attach{}).FieldByName("Kind")
	if !ok {
		t.Fatal("no Kind")
	}
	if got, want := strings.Join(AttachKinds(), "|"), f.Tag.Get("enum"); got != want {
		t.Fatalf("kinds %q, enum %q", got, want)
	}
}

// FuzzChatSendAttach feeds chat.send random attach lists. Invariants: no
// panic; an accepted list has at most MaxAttach items, each a known kind
// with no path outside the workspace.
func FuzzChatSendAttach(f *testing.F) {
	f.Add(`[{"kind":"file","path":"a.go"}]`)
	f.Add(`[{"kind":"lines","path":"a.go","from":1,"to":9},{"kind":"task","id":"build"}]`)
	f.Add(`[{"kind":"view","spec":"{\"view\":\"runs\"}"}]`)
	f.Add(`[{"kind":"file","path":"../.env"}]`)
	f.Fuzz(func(t *testing.T, list string) {
		a, err := DecodeArgs(OpChatSend, json.RawMessage(`{"text":"x","attach":`+list+`}`))
		if err != nil {
			return
		}
		got := a.(*ChatSendArgs).Attach
		if len(got) > MaxAttach {
			t.Fatalf("%d attaches accepted", len(got))
		}
		kinds := map[string]bool{}
		for _, k := range AttachKinds() {
			kinds[k] = true
		}
		for _, at := range got {
			if !kinds[at.Kind] {
				t.Fatalf("kind %q accepted", at.Kind)
			}
			if strings.HasPrefix(at.Path, "/") || strings.Contains("/"+at.Path+"/", "/../") {
				t.Fatalf("path %q accepted", at.Path)
			}
		}
	})
}
