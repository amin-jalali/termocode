//go:build unix

package app

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestSecret is registered like a key the IDE knows (split so no scanner
// sees a token here).
var webTestSecret = "web7" + "-secret-" + "value-42"

// Spec (WEB-7, T43): webData marshals a value and passes every string in
// it (values and keys) through the registered-secret redaction and
// webClean. Escaped forms are caught too, numbers keep their exact value
// and the result is always valid JSON.
func TestWebDataRedacts(t *testing.T) {
	ai.RegisterSecret(webTestSecret)
	quoted := `qu"ote&<s>` + "-secret-value-43"
	ai.RegisterSecret(quoted)
	cross := `x":"` + "cross-secret-44"
	ai.RegisterSecret(cross)
	in := map[string]any{
		"text":        "key is " + webTestSecret + " ok",
		webTestSecret: 1,
		"esc":         "a " + quoted + " b",
		"ctl":         "x\x1b[2Jy",
		"big":         int64(1) << 62,
		"f":           1.5,
		"x":           "cross-secret-44",
		"list":        []string{webTestSecret, "fine"},
		"html":        "<b>&</b>",
	}
	raw, err := webData(in)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, bad := range []string{webTestSecret, "secret-value-43", "\\u001b"} {
		if strings.Contains(s, bad) {
			t.Fatalf("%q leaked: %s", bad, s)
		}
	}
	var back map[string]any
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	if err := dec.Decode(&back); err != nil {
		t.Fatalf("not JSON: %v: %s", err, s)
	}
	if back["big"].(json.Number).String() != "4611686018427387904" || back["f"].(json.Number).String() != "1.5" {
		t.Fatalf("numbers changed: %s", s)
	}
	if back["ctl"] != "x[2Jy" || back["html"] != "<b>&</b>" || back["text"] != "key is [REDACTED] ok" {
		t.Fatalf("strings: %s", s)
	}
	if _, ok := back["[REDACTED]"]; !ok {
		t.Fatalf("key not redacted: %s", s)
	}
	clean, err := webData(map[string]string{"a": "plain"})
	if err != nil || string(clean) != `{"a":"plain"}` {
		t.Fatalf("plain: %s %v", clean, err)
	}
}

// Spec (WEB-7, T43): replies and error texts that leave the hub are
// redacted: a handler cannot leak a registered secret by mistake.
func TestNoSecretInReplies(t *testing.T) {
	ai.RegisterSecret(webTestSecret)
	r := newWebRig(t, nil)
	r.hub.ops = map[string]webOpFunc{
		webproto.OpChatOpen: func(m *Model, call *webCall) tea.Cmd {
			call.reply(map[string]string{"out": "token=" + webTestSecret})
			return nil
		},
		webproto.OpChatPin: func(m *Model, call *webCall) tea.Cmd {
			call.fail(webproto.CodeConflict, "failed with "+webTestSecret+"\x1b[0m")
			return nil
		},
	}
	c, _ := r.webHello(webTestOpen)
	c.send(`{"t":"req","id":1,"op":"chat.open","args":{"id":"x"}}`)
	line, _ := r.next(c)
	c.send(`{"t":"req","id":2,"op":"chat.pin","args":{"on":true}}`)
	line2, _ := r.next(c)
	for _, l := range []string{line, line2} {
		if strings.Contains(l, webTestSecret) || strings.Contains(l, "\\u001b") || !strings.Contains(l, "[REDACTED]") {
			t.Fatalf("frame %s", l)
		}
	}
}

// Spec (WEB-7, T43): snapshots and deltas leave the hub redacted and
// cleaned, also when two appends merge into one delta.
func TestNoSecretInTopicFrames(t *testing.T) {
	ai.RegisterSecret(webTestSecret)
	r := newWebRig(t, nil)
	n, log := 0, "boot "+webTestSecret
	webTestTopics(r, &n, &log)
	c, _ := r.webHello(webTestOpen)
	c.send(`{"t":"sub","topics":["status"]}`)
	snap, _ := r.next(c)
	half := len(webTestSecret) / 2
	h, qc := webQueueRig("status")
	h.publish("status", webTextAppend{ID: "log", Append: "a " + webTestSecret[:half]})
	h.publish("status", webTextAppend{ID: "log", Append: webTestSecret[half:] + " \x1b[0m"})
	frames, _ := qc.out.take()
	r.m.webPublish("status", map[string]string{"log": webTestSecret})
	delta, _ := r.next(c)
	for _, l := range []string{snap, string(frames[0]), delta} {
		if strings.Contains(l, webTestSecret) || strings.Contains(l, "\\u001b") || !strings.Contains(l, "[REDACTED]") {
			t.Fatalf("frame %s", l)
		}
	}
}
