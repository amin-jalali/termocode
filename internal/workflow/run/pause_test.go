package run

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestPauseStopsTheRunAndResumeGoesOn(t *testing.T) {
	src := strings.Replace(flow, `"when": "item.ready"`, `"when": "true"`, 1)
	fk := newFakes(items("a", "c")...)
	fk.fail["code/a"] = []error{fmt.Errorf("budget_usd reached: %w", ErrPaused)}
	var ev events
	var st memStore
	opts := fk.options()
	opts.OnEvent, opts.Store = ev.on, &st
	rf := wait(t, Start(context.Background(), mustParse(t, src), opts))
	its := rf.Steps["fix"].Each.Items
	if rf.State != RunPause || its[0].State != StatePaused || its[0].Steps["code"].State != StatePaused || its[1].State != StatePending {
		t.Fatalf("run %q items %q %q code %q", rf.State, its[0].State, its[1].State, its[0].Steps["code"].State)
	}
	if fk.tried("code/a") != 1 {
		t.Fatalf("a pause was retried: %d tries", fk.tried("code/a"))
	}
	if k := ev.kinds(); k[len(k)-1] != "run_paused" || !strings.Contains(rf.Error, "budget_usd") {
		t.Fatalf("last event %q error %q", k[len(k)-1], rf.Error)
	}
	disk := st.last(t)
	if disk.State != RunPause || Recover(disk).State != RunPause {
		t.Fatalf("disk %q", disk.State)
	}

	opts.Resume = disk
	opts.OnEvent, opts.Store = nil, nil
	end := wait(t, Start(context.Background(), mustParse(t, src), opts))
	if end.State != RunOK || fk.tried("code/a") != 2 || !slices.Contains(fk.all(), `run code/c prompt="fix T c" wt=/wt/c`) {
		t.Fatalf("after resume %q tries %d calls %q", end.State, fk.tried("code/a"), fk.all())
	}
}
