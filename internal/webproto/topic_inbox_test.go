package webproto

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// Spec (MOB-2): the inbox topic exists (v2, not keyed) and its data is
// typed for the page.
func TestInboxTopicDefined(t *testing.T) {
	tp, err := ParseTopic("inbox")
	if err != nil || tp.Name != "inbox" {
		t.Fatalf("inbox: %v %+v", err, tp)
	}
	if d, _ := topicDef("inbox"); d.Phase != PhaseV2 || d.Keyed {
		t.Errorf("inbox def %+v", d)
	}
	if _, err := ParseTopic("inbox:1"); err == nil {
		t.Error("inbox:1 accepted")
	}
	b, err := GenerateTS()
	if err != nil {
		t.Fatal(err)
	}
	ts := string(b)
	for _, want := range []string{
		`  "inbox": { snap: InboxSnap; delta: InboxSnap };`,
		`  kind: "card" | "run" | "lane" | "workflow" | "task" | "ci" | "race";`,
		`  state: "needs_you" | "working" | "failed" | "done";`,
		`  counts: InboxCounts;`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("proto.d.ts misses %q", want)
		}
	}
}

// Spec (MOB-2): a title is cut at MaxInboxTitle bytes on a rune edge and
// ends with "…"; a short one stays as it is.
func TestInboxTitleCut(t *testing.T) {
	if got := InboxTitle("build failed"); got != "build failed" {
		t.Errorf("short %q", got)
	}
	exact := strings.Repeat("x", MaxInboxTitle)
	if got := InboxTitle(exact); got != exact {
		t.Errorf("exact length cut: %q", got)
	}
	for _, s := range []string{strings.Repeat("a", 500), strings.Repeat("é", 90), strings.Repeat("日本", 40)} {
		got := InboxTitle(s)
		if len(got) > MaxInboxTitle || !utf8.ValidString(got) || !strings.HasSuffix(got, "…") || !strings.HasPrefix(s, strings.TrimSuffix(got, "…")) {
			t.Errorf("cut %d bytes → %d %q", len(s), len(got), got)
		}
	}
}

// Spec (MOB-2): Counts.Add counts the four states and nothing else.
func TestInboxCountsAdd(t *testing.T) {
	var c InboxCounts
	for _, s := range []string{InboxNeedsYou, InboxNeedsYou, InboxWorking, InboxFailed, InboxDone, "other"} {
		c.Add(s)
	}
	if c != (InboxCounts{NeedsYou: 2, Working: 1, Failed: 1, Done: 1}) {
		t.Errorf("counts %+v", c)
	}
}

// The topic's wire form is pinned by testdata/golden/topics/inbox.json.
func TestInboxSnapGolden(t *testing.T) {
	snap := InboxSnap{Items: []InboxItem{
		{ID: "card:card-3", Kind: InboxCard, Ref: "card-3", Title: "AI wants to: Write a.go", Since: 1_700_000_300_000, State: InboxNeedsYou, Can: []string{"answer"}},
		{ID: "run:c1/m4", Kind: InboxRun, Ref: "c1/m4", Title: "fix the toast flicker", Chat: "toast", Since: 1_700_000_200_000, State: InboxWorking, Can: []string{"stop", "pause"}},
		{ID: "lane:a1b2c3d4e5f6", Kind: InboxLane, Ref: "a1b2c3d4e5f6", Title: "Claude Code · feat/wt-sidebar", Lane: "a1b2c3d4e5f6", Since: 1_700_000_100_000, State: InboxNeedsYou},
		{ID: "task:test", Kind: InboxTask, Ref: "test", Title: "test", Since: 1_700_000_050_000, State: InboxFailed, Can: []string{"run"}},
		{ID: "ci:main", Kind: InboxCI, Ref: "main", Title: "CI · main", State: InboxDone},
	}, Counts: InboxCounts{NeedsYou: 2, Working: 1, Failed: 1, Done: 1}}
	got, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join("testdata", "golden", "topics", "inbox.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(p, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bytes.TrimSuffix(want, []byte("\n"))) {
		t.Errorf("inbox topic changed\n got %s\nwant %s", got, want)
	}
}
