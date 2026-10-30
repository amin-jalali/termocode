//go:build browser && webdev

package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// The browser store against the hub's own replay goldens (WEB-7): for
// every topic golden in internal/app/testdata/web/replay, the page's
// reducers fold snap₀ and the recorded deltas, and the result must be the
// hub's snapₙ, compared as canonical JSON like the Go check does. The dev
// kit page exposes the real store and reducers as window.__replay.

const replayDir = "../../internal/app/testdata/web/replay"

type replayCase struct {
	Topic  string            `json:"topic"`
	Snap0  json.RawMessage   `json:"snap0"`
	Deltas []json.RawMessage `json:"deltas"`
	SnapN  json.RawMessage   `json:"snapn"`
}

type replayOut struct {
	OK   bool
	Why  string
	Data string
}

func canon(t *testing.T, b []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

func openReplay(t *testing.T) *Page {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	p.Do(chromedp.Navigate(s.Base + "/dev/kit"))
	p.WaitJS(`typeof window.__replay === "function"`)
	return p
}

func replay(p *Page, c []byte) replayOut {
	p.T.Helper()
	return Eval[replayOut](p, `(() => { const r = window.__replay(`+string(c)+`); return {OK: r.ok, Why: r.why || "", Data: JSON.stringify(r.data ?? null)}; })()`)
}

func TestStoreReplayMatchesGo(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(replayDir, "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := openReplay(t)
	topics := map[string]bool{}
	for _, f := range files {
		dir := filepath.Base(filepath.Dir(f))
		if strings.HasPrefix(dir, "_") { // the harness's own test topic
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var c replayCase
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		topics[strings.SplitN(c.Topic, ":", 2)[0]] = true
		got := replay(p, b)
		if !got.OK {
			t.Errorf("%s: the store refused a delta: %s", f, got.Why)
			continue
		}
		if a, w := canon(t, []byte(got.Data)), canon(t, c.SnapN); a != w {
			t.Errorf("%s: the browser store gives\n  %s\nthe hub's snapshot is\n  %s", f, a, w)
		}
	}
	for _, want := range []string{"status", "toasts", "chats", "chat", "cards", "tree", "bufs", "buf", "terms", "term"} {
		if !topics[want] {
			t.Errorf("no replay golden for topic %s", want)
		}
	}
}

// A gone delta leaves the hub's data as it was (the replay still
// matches) but the page can tell the file or terminal was closed.
func TestStoreGoneKeepsDataAndMarksIt(t *testing.T) {
	p := openReplay(t)
	for _, c := range []string{
		`{"topic":"buf:2","snap0":{"buf":2,"path":"a.txt","tick":3,"lines":["one"],"dirty":true,"ft":"text","readonly":false},"deltas":[{"kind":"gone"}]}`,
		`{"topic":"term:5","snap0":{"buf":5,"first":0,"rows":["$ "],"more":false,"tick":3,"screen":false,"cols":80,"height":24},"deltas":[{"kind":"gone"}]}`,
	} {
		var g replayCase
		if err := json.Unmarshal([]byte(c), &g); err != nil {
			t.Fatal(err)
		}
		got := Eval[[]any](p, `(() => { const r = window.__replay(`+c+`); return [r.ok, JSON.stringify(r.data), r.data && r.data.gone === true]; })()`)
		if got[0] != true || got[2] != true {
			t.Errorf("%s: ok %v, gone mark %v", g.Topic, got[0], got[2])
		}
		if a, w := canon(t, []byte(got[1].(string))), canon(t, g.Snap0); a != w {
			t.Errorf("%s: gone changed the data: %s, want %s", g.Topic, a, w)
		}
	}
}

// A delta that does not fit the data (an append to a finished message, a
// message far past the end, an unknown op) is refused: the topic breaks
// and waits for a fresh snap instead of showing a state the hub never had.
func TestStoreRejectsBadDelta(t *testing.T) {
	p := openReplay(t)
	chat := `{"id":"c-1","title":"T","busy":false,"messages":[{"i":0,"role":"assistant","text":"done"}]}`
	buf := `{"buf":2,"path":"a.txt","tick":3,"lines":["one","two"],"dirty":false,"ft":"text","readonly":false}`
	term := `{"buf":5,"first":10,"rows":["a","b"],"more":true,"tick":3,"screen":false,"cols":80,"height":24}`
	for name, c := range map[string]string{
		"append to a finished message": `{"topic":"chat","snap0":` + chat + `,"deltas":[{"op":"append","i":0,"text":"x"}]}`,
		"message past the end":         `{"topic":"chat","snap0":` + chat + `,"deltas":[{"op":"message","msg":{"i":5,"role":"user"}}]}`,
		"truncate past the end":        `{"topic":"chat","snap0":` + chat + `,"deltas":[{"op":"truncate","n":3}]}`,
		"unknown chat op":              `{"topic":"chat","snap0":` + chat + `,"deltas":[{"op":"explode"}]}`,
		"upsert without a card":        `{"topic":"cards","snap0":{"cards":[]},"deltas":[{"op":"upsert"}]}`,
		"chats delta not an object":    `{"topic":"chats","snap0":{"chats":[]},"deltas":["x"]}`,
		"unknown tree kind":            `{"topic":"tree","snap0":{"roots":[],"hidden":false,"dirs":{}},"deltas":[{"kind":"move","path":"a"}]}`,
		"hidden without on":            `{"topic":"tree","snap0":{"roots":[],"hidden":false,"dirs":{}},"deltas":[{"kind":"hidden"}]}`,
		"buf lines past the end":       `{"topic":"buf:2","snap0":` + buf + `,"deltas":[{"kind":"lines","first":1,"last":5,"lines":["x"],"tick":4}]}`,
		"buf lines not text":           `{"topic":"buf:2","snap0":` + buf + `,"deltas":[{"kind":"lines","first":0,"last":1,"lines":[7],"tick":4}]}`,
		"buf lines first after last":   `{"topic":"buf:2","snap0":` + buf + `,"deltas":[{"kind":"lines","first":1,"last":0,"lines":[],"tick":4}]}`,
		"unknown buf kind":             `{"topic":"buf:2","snap0":` + buf + `,"deltas":[{"kind":"explode"}]}`,
		"term lines need older rows":   `{"topic":"term:5","snap0":` + term + `,"deltas":[{"kind":"lines","first":10,"last":12,"lines":[],"tick":4}]}`,
		"term lines past the window":   `{"topic":"term:5","snap0":` + term + `,"deltas":[{"kind":"lines","first":11,"last":20,"lines":["x"],"tick":4}]}`,
		"term mode without cols":       `{"topic":"term:5","snap0":` + term + `,"deltas":[{"kind":"mode","screen":true}]}`,
	} {
		if got := replay(p, []byte(c)); got.OK {
			t.Errorf("%s: accepted, data %s", name, got.Data)
		}
	}
}
