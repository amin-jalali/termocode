package nvim

import (
	"os/exec"
	"testing"
	"time"
)

func newAttachedClient(t *testing.T) *Client {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed; skipping integration test")
	}
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	return c
}

// waitNotify drains the event channel until a NotifyMsg for `method`
// arrives (redraw batches are skipped).
func waitNotify(t *testing.T, c *Client, method string) NotifyMsg {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case msg := <-c.events:
			if n, ok := msg.(NotifyMsg); ok && n.Method == method {
				return n
			}
		case <-deadline:
			t.Fatalf("no %q notification within 3s", method)
		}
	}
}

func TestRegisterNotifyDelivers(t *testing.T) {
	c := newAttachedClient(t)
	if c.ChannelID() <= 0 {
		t.Fatalf("ChannelID = %d, want > 0", c.ChannelID())
	}
	if err := c.RegisterNotify("tc_test", nil); err != nil {
		t.Fatalf("RegisterNotify: %v", err)
	}
	if err := c.ExecLua(`_G.termocode_notify('tc_test', 7, 'hi', { a = 1 })`); err != nil {
		t.Fatalf("ExecLua: %v", err)
	}
	n := waitNotify(t, c, "tc_test")
	if len(n.Args) != 3 {
		t.Fatalf("args = %#v, want 3 values", n.Args)
	}
	if toInt(n.Args[0]) != 7 || toString(n.Args[1]) != "hi" {
		t.Errorf("args = %#v", n.Args)
	}
	if mp, ok := n.Args[2].(map[string]interface{}); !ok || toInt(mp["a"]) != 1 {
		t.Errorf("table arg = %#v", n.Args[2])
	}
}

func TestRegisterNotifyFilter(t *testing.T) {
	c := newAttachedClient(t)
	if err := c.RegisterNotify("tc_filtered", func(args []any) bool {
		return len(args) > 0 && toString(args[0]) == "keep"
	}); err != nil {
		t.Fatalf("RegisterNotify: %v", err)
	}
	_ = c.ExecLua(`_G.termocode_notify('tc_filtered', 'drop')`)
	_ = c.ExecLua(`_G.termocode_notify('tc_filtered', 'keep')`)
	n := waitNotify(t, c, "tc_filtered")
	if toString(n.Args[0]) != "keep" {
		t.Errorf("filter let %q through", toString(n.Args[0]))
	}
}

func TestRegisterNotifyRejectsReserved(t *testing.T) {
	c := &Client{}
	if err := c.RegisterNotify("redraw", nil); err == nil {
		t.Error("redraw must be rejected")
	}
	if err := c.RegisterNotify("", nil); err == nil {
		t.Error("empty method must be rejected")
	}
}

func TestCursorContext(t *testing.T) {
	c := newAttachedClient(t)
	if err := c.ExecLua(`
		vim.api.nvim_buf_set_lines(0, 0, -1, false, { 'one', 'two three', 'four' })
		vim.bo.filetype = 'text'
		vim.api.nvim_win_set_cursor(0, { 2, 4 })
	`); err != nil {
		t.Fatalf("setup: %v", err)
	}
	ci, err := c.CursorContext()
	if err != nil {
		t.Fatalf("CursorContext: %v", err)
	}
	if ci.Line != 2 || ci.Col != 5 || ci.Filetype != "text" {
		t.Errorf("pos/ft = %d:%d %q", ci.Line, ci.Col, ci.Filetype)
	}
	if ci.Before != "one\ntwo " || ci.After != "three\nfour" {
		t.Errorf("before=%q after=%q", ci.Before, ci.After)
	}
	// Window of 0 lines keeps only the cursor line.
	ci, err = c.CursorContextWindow(0, 0)
	if err != nil {
		t.Fatalf("CursorContextWindow: %v", err)
	}
	if ci.Before != "two " || ci.After != "three" {
		t.Errorf("window 0: before=%q after=%q", ci.Before, ci.After)
	}
}

func TestParseCursorInfoShort(t *testing.T) {
	if got := parseCursorInfo([]interface{}{"x"}); got != (CursorInfo{}) {
		t.Errorf("short tuple should yield zero value, got %+v", got)
	}
}
