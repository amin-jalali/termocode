package app

import (
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/blocks"
	"github.com/amin-jalali/termocode/internal/views/feature"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestViewCmd runs what a view fetch returned, as Update would: batches
// in turn, applyMsg on the Model.
func webTestViewCmd(m *Model, cmd tea.Cmd) {
	for depth := 0; cmd != nil && depth < 32; depth++ {
		msg := cmd()
		cmd = nil
		switch v := msg.(type) {
		case tea.BatchMsg:
			for _, c := range v {
				webTestViewCmd(m, c)
			}
		case applyMsg:
			cmd = v(m)
		}
	}
}

// webTestView runs spec for c and gives the block or the error.
func webTestView(t *testing.T, m *Model, c *webConn, spec blocks.ViewSpec) (blocks.Block, error) {
	t.Helper()
	call := &webCall{ctx: c.ctx, conn: c, id: 1, op: "view.run"}
	var got *blocks.Block
	var gotErr error
	answered := false
	cmd := m.webFeatureView(call, "b1", spec, func(_ *Model, b blocks.Block, err error) tea.Cmd {
		answered, got, gotErr = true, &b, err
		return nil
	})
	webTestViewCmd(m, cmd)
	if !answered {
		t.Fatalf("%s: no answer", spec.View)
	}
	return *got, gotErr
}

// webTestViewOps gives the hub its own op table with fakes for ops.
func webTestViewOps(m *Model, fakes map[string]webOpFunc) {
	ops := maps.Clone(webOps)
	maps.Copy(ops, fakes)
	m.web.ops = ops
}

// Spec (MOBUX-13): every feature view has a fetch on the hub.
func TestFeatureViewsAllHaveFetches(t *testing.T) {
	for _, d := range feature.Defs() {
		if webFeatureFetches[d.ID] == nil {
			t.Errorf("%s: no fetch", d.ID)
		}
	}
	for id := range webFeatureFetches {
		if _, ok := feature.Lookup(id); !ok {
			t.Errorf("fetch %s has no view", id)
		}
	}
}

// Spec (MOBUX-13): the usage view reads the laptop's USAGE data; a
// subscription line has no dollar cost; the spec's sort is applied on
// the laptop.
func TestFeatureViewUsage(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	cost := 0.5
	m.ai.usageLog = []aiUsageEntry{
		{Model: "cheap", Agent: "plan", U: ai.Usage{Input: 100, Output: 10}, Cost: &cost},
		{Model: "sub", Agent: "code", U: ai.Usage{Input: 900, Output: 90}, Sub: true},
	}
	b, err := webTestView(t, &m, c, blocks.ViewSpec{View: feature.ViewUsage})
	if err != nil {
		t.Fatal(err)
	}
	if b.Kind != blocks.KindTable || b.Source != feature.ViewUsage || b.Spec == nil || len(b.Rows) != 2 {
		t.Fatalf("block %+v", b)
	}
	if b.Rows[0][0] != "cheap" || b.Rows[1][1] != "" {
		t.Fatalf("rows %v: priced first by cost, the subscription with no cost", b.Rows)
	}
	b, err = webTestView(t, &m, c, blocks.ViewSpec{View: feature.ViewUsage, Args: map[string]string{"by": "agent"}, Layout: blocks.KindList})
	if err != nil || b.Kind != blocks.KindList || len(b.Items) != 2 {
		t.Fatalf("by agent: %v %+v", err, b)
	}
}

// Spec (MOBUX-13): an op-backed view runs the op's own handler in process
// (here a fake that answers later, as gh does) and renders its result.
func TestFeatureViewByOpLater(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	webTestViewOps(&m, map[string]webOpFunc{
		webproto.OpPRList: func(_ *Model, call *webCall) tea.Cmd {
			call.later()
			return func() tea.Msg {
				call.reply(webproto.PRList{Items: []webproto.PRItem{
					{Number: 7, Title: "old", Checks: "pass"}, {Number: 9, Title: "new", Checks: "fail", Review: "APPROVED"}}})
				return nil
			}
		},
	})
	webTestNow(t, c) // what the subscribe queued
	b, err := webTestView(t, &m, c, blocks.ViewSpec{View: feature.ViewPRList, Fields: []string{"title", "number"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Rows) != 2 || b.Rows[0][0] != "new" || b.Rows[1][0] != "old" {
		t.Fatalf("rows %v, want newest number first", b.Rows)
	}
	if c.inflight.Load() != 0 {
		t.Fatalf("in-process call touched the connection's in-flight count: %d", c.inflight.Load())
	}
	if frames := webTestNow(t, c); len(frames) != 0 {
		t.Fatalf("in-process call sent frames to the page: %v", frames)
	}
}

// Spec (MOBUX-13): a source op's refusal comes back with its own code.
func TestFeatureViewKeepsOpError(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	webTestViewOps(&m, map[string]webOpFunc{
		webproto.OpCIStatus: func(_ *Model, call *webCall) tea.Cmd {
			call.fail(webproto.CodeConflict, "This repository has no GitHub remote.")
			return nil
		},
	})
	_, err := webTestView(t, &m, c, blocks.ViewSpec{View: feature.ViewCIChecks})
	var ve *webViewError
	if !errors.As(err, &ve) || ve.Code != webproto.CodeConflict || !strings.Contains(ve.Msg, "GitHub") {
		t.Fatalf("err %v", err)
	}
}

// Spec (MOBUX-13): a log view is a text block of redacted lines.
func TestFeatureViewLogsRedacted(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	var gotID string
	webTestViewOps(&m, map[string]webOpFunc{
		webproto.OpContainersLogs: func(_ *Model, call *webCall) tea.Cmd {
			gotID = call.args.(*webproto.ContainerLogsArgs).ID
			call.reply(webproto.ContainerLogs{ID: gotID, Lines: []string{"ready", "Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123456789"}})
			return nil
		},
	})
	b, err := webTestView(t, &m, c, blocks.ViewSpec{View: feature.ViewLogsContainer, Args: map[string]string{"id": "0123456789ab"}}) // a container id is 12-64 hex
	if err != nil {
		t.Fatal(err)
	}
	if gotID != "0123456789ab" || b.Kind != blocks.KindText || b.MD == nil {
		t.Fatalf("id %q block %+v", gotID, b)
	}
	text := b.MD.C[0].Text
	if !strings.HasPrefix(text, "ready\n") || strings.Contains(text, "abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Fatalf("text %q: want the lines with the token hidden", text)
	}
}

// Spec (MOBUX-13): db.rows renders the table's own columns; a filter on
// one of them is applied on the laptop.
func TestFeatureViewDBRows(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	var asked webproto.DBRowsArgs
	webTestViewOps(&m, map[string]webOpFunc{
		webproto.OpDBRows: func(_ *Model, call *webCall) tea.Cmd {
			asked = *call.args.(*webproto.DBRowsArgs)
			call.reply(webproto.DBRows{Columns: []string{"id", "status"}, Rows: [][]webproto.DBCell{{{V: "1"}, {V: "paid"}}, {{V: "2"}, {V: "open"}}}})
			return nil
		},
	})
	b, err := webTestView(t, &m, c, blocks.ViewSpec{View: feature.ViewDBRows, Args: map[string]string{"conn": "local", "table": "orders"},
		Fields: []string{"c_id", "c_status"}, Filter: []blocks.ViewCond{{Field: "c_status", Op: "eq", Value: "paid"}}})
	if err != nil {
		t.Fatal(err)
	}
	if asked.Conn != "local" || asked.Table != "orders" {
		t.Fatalf("asked %+v", asked)
	}
	if len(b.Columns) != 2 || len(b.Rows) != 1 || b.Rows[0][1] != "paid" {
		t.Fatalf("block cols %+v rows %v", b.Columns, b.Rows)
	}
	if _, err := webTestView(t, &m, c, blocks.ViewSpec{View: feature.ViewDBRows, Args: map[string]string{"conn": "local", "table": "orders"}, Fields: []string{"c_nope"}}); err == nil {
		t.Fatal("an unknown column was accepted")
	}
}

// Spec (MOBUX-13): an unknown view, a missing required param and a laptop
// card op are refused before any source runs.
func TestFeatureViewRefusals(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	ran := false
	webTestViewOps(&m, map[string]webOpFunc{
		webproto.OpKubeLogs: func(_ *Model, call *webCall) tea.Cmd { ran = true; call.reply(webproto.KubeText{}); return nil },
	})
	var ve *webViewError
	if _, err := webTestView(t, &m, c, blocks.ViewSpec{View: "nope"}); !errors.As(err, &ve) || ve.Code != webproto.CodeNotFound {
		t.Fatalf("unknown view: %v", err)
	}
	if _, err := webTestView(t, &m, c, blocks.ViewSpec{View: feature.ViewLogsPod}); err == nil || ran {
		t.Fatalf("pod logs with no pod: err %v ran %v", err, ran)
	}
	call := &webCall{ctx: c.ctx, conn: c, id: 1, op: "view.run"}
	var code webproto.ErrCode
	webTestViewCmd(&m, m.webInnerCall(call, webproto.OpPRMerge, webproto.PRNumberArgs{Number: 1}, func(_ *Model, _ json.RawMessage, e *webproto.ErrMsg) tea.Cmd {
		if e != nil {
			code = e.Code
		}
		return nil
	}))
	if code != webproto.CodeNotFound {
		t.Fatalf("a laptop card op ran in process: code %q", code)
	}
}

// Spec (MOBUX-13): a viewer connection runs a feature view (every source
// is a read).
func TestFeatureViewViewer(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	c.role = webproto.RoleViewer
	webTestViewOps(&m, map[string]webOpFunc{
		webproto.OpContainersList: func(_ *Model, call *webCall) tea.Cmd {
			call.reply(webproto.ContainersList{Engine: "docker", Containers: []webproto.ContainerItem{{ID: "x1", Name: "db", State: "running"}}})
			return nil
		},
	})
	b, err := webTestView(t, &m, c, blocks.ViewSpec{View: feature.ViewContainers, Layout: blocks.KindList})
	if err != nil || len(b.Items) != 1 || b.Items[0].Tone != blocks.ToneOK || b.Items[0].Ref == nil || b.Items[0].Ref.ID != "x1" {
		t.Fatalf("err %v block %+v", err, b)
	}
}
