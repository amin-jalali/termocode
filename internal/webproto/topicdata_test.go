package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (WEB-12): the data of a topic is a registered type; proto.d.ts maps
// each topic to its snap and delta types and each ev kind to its data, and
// an enum tag becomes a union of literals (TypeScript) and an enum (schema).
func TestTopicDataGenerated(t *testing.T) {
	b, err := GenerateTS()
	if err != nil {
		t.Fatal(err)
	}
	ts := string(b)
	for _, want := range []string{
		`export interface TopicData {`,
		`  "status": { snap: StatusSnap; delta: StatusSnap };`,
		`  "toasts": { snap: ToastsSnap; delta: ToastsDelta };`,
		`export interface EventData {`,
		`  "attention": Attention;`,
		`  op: "push" | "dismiss";`,
		`  severity: "info" | "warn" | "error";`,
		`  colors: Record<string, string>;`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("proto.d.ts misses %q", want)
		}
	}
	sb, err := GenerateSchema()
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Topics []struct {
			Name   string
			Snap   string
			Deltas []string
		}
		Events map[string]string
		Defs   map[string]struct {
			AdditionalProperties bool `json:"additionalProperties"`
			Properties           map[string]map[string]any
		}
	}
	if err := json.Unmarshal(sb, &s); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tp := range s.Topics {
		if tp.Name == "toasts" {
			found = tp.Snap == "ToastsSnap" && len(tp.Deltas) == 1 && tp.Deltas[0] == "ToastsDelta"
		}
	}
	if !found {
		t.Errorf("schema topics: %+v", s.Topics)
	}
	if s.Events["attention"] != "Attention" {
		t.Errorf("schema events: %v", s.Events)
	}
	d := s.Defs["ToastsDelta"]
	if !d.AdditionalProperties {
		t.Error("ToastsDelta (hub → browser) must allow new fields")
	}
	if e, _ := d.Properties["op"]["enum"].([]any); len(e) != 2 {
		t.Errorf("ToastsDelta.op enum: %v", d.Properties["op"])
	}
}

// Spec (WEB-12): only known topics get data, each once.
func TestTopicDataRegistry(t *testing.T) {
	for name := range topicData {
		if _, ok := topicDef(name); !ok {
			t.Errorf("data for unknown topic %s", name)
		}
	}
	mustPanic := func(name string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s did not panic", name)
			}
		}()
		f()
	}
	mustPanic("unknown topic", func() { registerTopic("nope", struct{}{}, struct{}{}) })
	mustPanic("twice", func() { registerTopic("status", struct{}{}, struct{}{}) })
	mustPanic("no delta", func() { registerTopic("status", struct{}{}) })
	mustPanic("not a struct", func() { registerEvent("x-test", 1) })
}

// Spec (WEB-16/17): the file topics and their op results are in
// proto.d.ts and schema.json; a topic with several delta shapes lists
// them all, and each has a literal kind, so the browser store can narrow.
func TestFileTopicDataGenerated(t *testing.T) {
	b, err := GenerateTS()
	if err != nil {
		t.Fatal(err)
	}
	ts := string(b)
	for _, want := range []string{
		`  "tree": { snap: TreeSnap; delta: TreeDirDelta | TreeRemoveDelta | TreeHiddenDelta };`,
		`  "bufs": { snap: BufsSnap; delta: BufsSnap };`,
		`  kind: "lines";`, `  kind: "dir";`,
		`  "buf.open": BufOpenRes;`,
		"  dirs: Record<string, TreeDir>;",
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("proto.d.ts misses %q", want)
		}
	}
	sb, err := GenerateSchema()
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Topics []struct {
			Name   string
			Snap   string
			Deltas []string
		}
		Ops  []struct{ Op, Result string }
		Defs map[string]struct {
			Properties map[string]map[string]any
		}
	}
	if err := json.Unmarshal(sb, &s); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tp := range s.Topics {
		if tp.Name == "buf" {
			found = tp.Snap == "BufSnap" && len(tp.Deltas) == 5
		}
	}
	if !found {
		t.Errorf("schema topic buf: %+v", s.Topics)
	}
	res := ""
	for _, o := range s.Ops {
		if o.Op == OpBufOpen {
			res = o.Result
		}
	}
	if res != "BufOpenRes" {
		t.Errorf("schema op buf.open result %q", res)
	}
	if k := s.Defs["LinesDelta"].Properties["kind"]["const"]; k != "lines" {
		t.Errorf("LinesDelta.kind schema: %v", s.Defs["LinesDelta"].Properties["kind"])
	}
}

// Every registered delta of a topic with more than one delta struct has a
// Kind with a const tag, and the kinds of one topic differ.
func TestTopicDeltaKinds(t *testing.T) {
	for _, d := range topicDataList() {
		if len(d.Deltas) < 2 {
			continue
		}
		kinds := map[string]bool{}
		for _, x := range d.Deltas {
			f, ok := x.FieldByName("Kind")
			k := f.Tag.Get("const")
			if !ok || k == "" {
				t.Errorf("%s: %s has no Kind with a const tag", d.Topic, x.Name())
				continue
			}
			if kinds[k] {
				t.Errorf("%s: kind %q twice", d.Topic, k)
			}
			kinds[k] = true
		}
	}
}
