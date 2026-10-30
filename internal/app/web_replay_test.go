package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The per-topic replay goldens (WEB-7): for each topic a scripted
// scenario records snap₀, the deltas as they leave the hub (coalesced)
// and snapₙ; the check is apply(snap₀, deltas…) == snapₙ. The same
// fixtures later run against the browser store. A golden lives in
// testdata/web/replay/<topic>/<name>.json; UPDATE_GOLDEN=1 rewrites it.

// webReplayCase is one replay golden.
type webReplayCase struct {
	Topic  string            `json:"topic"`
	Snap0  json.RawMessage   `json:"snap0"`
	Deltas []json.RawMessage `json:"deltas"`
	SnapN  json.RawMessage   `json:"snapn"`
}

// webReplayRecord subscribes a socketless connection to topic, runs the
// script (which changes the Model and publishes), and records the case.
func webReplayRecord(t *testing.T, m *Model, h *webHub, topic string, script func()) webReplayCase {
	t.Helper()
	_, c := webQueueRig()
	c.hub = h
	h.live[c] = true
	defer delete(h.live, c)
	m.onWebSub(webSubMsg{hub: h, conn: c, topics: []string{topic}})
	script()
	frames := webTestTake(t, c)
	out := webReplayCase{Topic: topic}
	for i, f := range frames {
		b, _ := json.Marshal(f["data"])
		switch {
		case i == 0 && f["t"] == "snap":
			out.Snap0 = b
		case f["t"] == "delta":
			out.Deltas = append(out.Deltas, b)
		default:
			t.Fatalf("frame %d: %v", i, f)
		}
	}
	tp, _ := webproto.ParseTopic(topic)
	snap, err := h.topics[tp.Name].snapshot(m, tp.Key)
	if err != nil {
		t.Fatal(err)
	}
	if out.SnapN, err = webData(snap); err != nil {
		t.Fatal(err)
	}
	return out
}

// webReplayCheck folds the deltas into snap₀ with the topic's apply.
func webReplayCheck(t *testing.T, def webTopicDef, c webReplayCase) {
	t.Helper()
	if def.apply == nil {
		t.Fatalf("topic %s has no apply", c.Topic)
	}
	state := c.Snap0
	for i, d := range c.Deltas {
		next, err := def.apply(state, d)
		if err != nil {
			t.Fatalf("%s delta %d: %v", c.Topic, i, err)
		}
		state = next
	}
	if a, b := webCanonJSON(t, state), webCanonJSON(t, c.SnapN); a != b {
		t.Fatalf("%s: replay gives %s, snapshot is %s", c.Topic, a, b)
	}
}

// webReplayPath is where the golden name of a topic lives.
func webReplayPath(topic, name string) string {
	tp, _ := webproto.ParseTopic(topic)
	return filepath.Join("testdata", "web", "replay", tp.Name, name+".json")
}

// webReplayGolden compares c with the golden at path (or writes it).
func webReplayGolden(t *testing.T, path string, c webReplayCase) {
	t.Helper()
	got, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (UPDATE_GOLDEN=1 writes it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from the recorded scenario:\n%s", path, got)
	}
}

func webCanonJSON(t *testing.T, b []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

// Spec (WEB-7): the harness on a test topic: deltas (one of them two
// merged appends) replay to the final snapshot, and the golden pins the
// frames.
func TestTopicReplayGoldens(t *testing.T) {
	m := aiTestModel(t)
	h := newWebHub()
	t.Cleanup(h.stop)
	n, log := 0, ""
	h.topics = map[string]webTopicDef{"status": {
		snapshot: func(*Model, string) (any, error) { return map[string]any{"n": n, "log": log}, nil },
		apply:    webTestApply,
	}}
	c := webReplayRecord(t, &m, h, "status", func() {
		n = 1
		h.publish("status", map[string]int{"n": 1})
		for _, s := range []string{"build ", "ok"} {
			log += s
			h.publish("status", webTextAppend{ID: "log", Append: s})
		}
		n = 2
		h.publish("status", map[string]int{"n": 2})
	})
	if len(c.Deltas) != 3 {
		t.Fatalf("deltas %d, want 3 (two appends merged)", len(c.Deltas))
	}
	webReplayCheck(t, h.topics["status"], c)
	// Not under status/: the real status topic brings its own goldens.
	webReplayGolden(t, filepath.Join("testdata", "web", "replay", "_harness", "status.json"), c)
}

// Spec (WEB-7): every served topic has an apply and at least one replay
// golden.
func TestEveryTopicHasReplayGolden(t *testing.T) {
	var names []string
	for name := range webTopics {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if webTopics[name].apply == nil {
			t.Errorf("topic %s has no apply", name)
		}
		files, _ := filepath.Glob(filepath.Join("testdata", "web", "replay", name, "*.json"))
		if len(files) == 0 {
			t.Errorf("topic %s has no replay golden in testdata/web/replay/%s/", name, name)
		}
	}
}
