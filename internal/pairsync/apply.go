package pairsync

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/amin-jalali/termocode/internal/workspace"
)

// Result says what Apply did.
type Result struct {
	Pushed, Pulled, DeletedLocal, DeletedRemote, Adopted, Conflicts int
	Failed                                                          []string        // "path: reason"
	OK                                                              map[string]bool // paths whose action succeeded
}

// Apply runs plan. local and remote are the scans the plan came from
// (their modes become the mode of a new copy). A file bigger than max
// bytes is not copied. One failed path never stops the others.
func Apply(ctx context.Context, plan Plan, local, remote workspace.FS, localSnap, remoteSnap Snapshot, max int64) Result {
	res := Result{OK: map[string]bool{}}
	for _, a := range plan.Actions {
		if err := ctx.Err(); err != nil {
			res.Failed = append(res.Failed, a.Path+": "+err.Error())
			continue
		}
		var err error
		switch a.Op {
		case OpPush:
			err = copyFile(ctx, local, remote, a.Path, localSnap[a.Path], max)
			if err == nil {
				res.Pushed++
			}
		case OpPull:
			err = copyFile(ctx, remote, local, a.Path, remoteSnap[a.Path], max)
			if err == nil {
				res.Pulled++
			}
		case OpDeleteRemote:
			err = remote.Remove(ctx, a.Path)
			if err == nil {
				res.DeletedRemote++
			}
		case OpDeleteLocal:
			err = local.Remove(ctx, a.Path)
			if err == nil {
				res.DeletedLocal++
			}
		case OpAdopt:
			res.Adopted++
		case OpConflict:
			res.Conflicts++
			continue
		default:
			continue
		}
		if err != nil {
			res.Failed = append(res.Failed, a.Path+": "+err.Error())
			continue
		}
		res.OK[a.Path] = true
	}
	return res
}

func copyFile(ctx context.Context, from, to workspace.FS, p string, e Entry, max int64) error {
	if e.Size > max {
		return fmt.Errorf("bigger than %d bytes; copy it by hand", max)
	}
	data, err := from.ReadFile(ctx, p)
	if err != nil {
		return err
	}
	if int64(len(data)) > max {
		return fmt.Errorf("bigger than %d bytes; copy it by hand", max)
	}
	perm := fs.FileMode(e.Mode) & 0o777
	if perm == 0 {
		perm = 0o644
	}
	return to.WriteFile(ctx, p, data, perm)
}

// NextBase is the base after a sync: old is the base before it, plan
// and ok what ran, preL/preR the scans the plan came from, postL/postR
// fresh scans taken after Apply.
//
// A path is recorded only when both sides have it and every side the
// sync did not write still looks as it did before the sync; otherwise
// (a conflict, a failed step, a file edited while the sync ran) the old
// record stays, so the next sync sees the change again.
func NextBase(old Base, plan Plan, ok map[string]bool, preL, preR, postL, postR Snapshot) Base {
	acted := map[string]Op{}
	for _, a := range plan.Actions {
		acted[a.Path] = a.Op
	}
	nb := Base{Version: baseVersion, Local: Snapshot{}, Remote: Snapshot{}}
	keep := func(p string) {
		bl, lok := old.Local[p]
		br, rok := old.Remote[p]
		if lok && rok {
			nb.Local[p], nb.Remote[p] = bl, br
		}
	}
	seen := map[string]bool{}
	for _, s := range []Snapshot{postL, postR, old.Local} {
		for p := range s {
			if seen[p] {
				continue
			}
			seen[p] = true
			op, has := acted[p]
			if has && (op == OpConflict || !ok[p]) {
				keep(p)
				continue
			}
			pl, plok := postL[p]
			pr, prok := postR[p]
			if !plok || !prok {
				continue // gone on a side: it drops out
			}
			if op != OpPull { // the workspace side was not written
				if e, ok := preL[p]; !ok || !e.same(pl) {
					keep(p)
					continue
				}
			}
			if op != OpPush { // the container side was not written
				if e, ok := preR[p]; !ok || !e.same(pr) {
					keep(p)
					continue
				}
			}
			nb.Local[p], nb.Remote[p] = pl, pr
		}
	}
	return nb
}
