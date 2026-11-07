// Package pairsync keeps a folder in a container and the local
// workspace in step, both ways (pairing mode, CMP-65).
//
// A sync is three steps. Scan lists the files of each side (size and
// change time only; no file is read). Diff compares both lists with the
// Base, the lists recorded at the end of the last sync, and plans one
// action per changed path: copy to the container, copy to the workspace,
// delete on one side, or a conflict when both sides changed. Apply runs
// the plan; NextBase records the new lists. A conflict is never solved
// by the package: both copies stay as they are and the path is listed.
package pairsync

import (
	"bytes"
	"context"
	"errors"
	"sort"

	"github.com/amin-jalali/termocode/internal/workspace"
)

// Entry is one file as a scan sees it. MTime is in nanoseconds (a
// container listing has whole seconds).
type Entry struct {
	Size  int64  `json:"s"`
	MTime int64  `json:"t"`
	Mode  uint32 `json:"m,omitempty"` // permission bits
}

// same reports whether e and o look like the same file version.
func (e Entry) same(o Entry) bool { return e.Size == o.Size && e.MTime == o.MTime }

// Snapshot is the files of one side by slash path relative to the root.
type Snapshot map[string]Entry

// Op is what a sync does with one path.
type Op int

const (
	OpNone         Op = iota
	OpPush            // workspace → container
	OpPull            // container → workspace
	OpDeleteRemote    // deleted in the workspace: delete in the container
	OpDeleteLocal     // deleted in the container: delete in the workspace
	OpAdopt           // new on both sides with the same bytes: only recorded
	OpConflict        // changed on both sides: left alone
)

func (o Op) String() string {
	switch o {
	case OpPush:
		return "to container"
	case OpPull:
		return "to workspace"
	case OpDeleteRemote:
		return "delete in container"
	case OpDeleteLocal:
		return "delete in workspace"
	case OpAdopt:
		return "same on both sides"
	case OpConflict:
		return "conflict"
	}
	return "none"
}

// Action is one planned step.
type Action struct {
	Path   string
	Op     Op
	Reason string // why a conflict is one
}

// Plan is every planned step, sorted by path.
type Plan struct{ Actions []Action }

// Count is how many actions have op.
func (p Plan) Count(op Op) int {
	n := 0
	for _, a := range p.Actions {
		if a.Op == op {
			n++
		}
	}
	return n
}

// Empty: nothing to do and nothing to report.
func (p Plan) Empty() bool { return len(p.Actions) == 0 }

// Paths are the paths of the actions with op, in order.
func (p Plan) Paths(op Op) []string {
	var out []string
	for _, a := range p.Actions {
		if a.Op == op {
			out = append(out, a.Path)
		}
	}
	return out
}

// Diff plans the sync of local and remote since base.
func Diff(base Base, local, remote Snapshot) Plan {
	seen := map[string]bool{}
	var paths []string
	for _, s := range []Snapshot{base.Local, base.Remote, local, remote} {
		for p := range s {
			if !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	sort.Strings(paths)
	var plan Plan
	for _, p := range paths {
		l, lok := local[p]
		r, rok := remote[p]
		bl, blok := base.Local[p]
		br, brok := base.Remote[p]
		known := blok && brok
		lch := lok && (!known || !l.same(bl))
		rch := rok && (!known || !r.same(br))
		ldel := !lok && known
		rdel := !rok && known
		var a Action
		switch {
		case ldel && rdel, !lch && !rch && !ldel && !rdel:
			continue
		case lch && rch:
			a = Action{Op: OpConflict, Reason: "changed on both sides"}
			if !known {
				a.Reason = "new on both sides"
			}
		case lch && rdel:
			a = Action{Op: OpConflict, Reason: "changed in the workspace, deleted in the container"}
		case rch && ldel:
			a = Action{Op: OpConflict, Reason: "changed in the container, deleted in the workspace"}
		case lch:
			a.Op = OpPush
		case rch:
			a.Op = OpPull
		case ldel:
			a.Op = OpDeleteRemote
		case rdel:
			a.Op = OpDeleteLocal
		}
		a.Path = p
		plan.Actions = append(plan.Actions, a)
	}
	return plan
}

// ResolveSame turns a conflict whose two files hold the same bytes into
// OpAdopt (only files up to max bytes are read). A first sync of two
// copies of one repo then needs no copy at all.
func ResolveSame(ctx context.Context, plan Plan, local, remote workspace.FS, max int64) Plan {
	out := Plan{Actions: make([]Action, len(plan.Actions))}
	copy(out.Actions, plan.Actions)
	for i, a := range out.Actions {
		if a.Op != OpConflict || ctx.Err() != nil {
			continue
		}
		ls, err := local.Stat(ctx, a.Path)
		if err != nil || !ls.Mode().IsRegular() || ls.Size() > max {
			continue
		}
		rs, err := remote.Stat(ctx, a.Path)
		if err != nil || !rs.Mode().IsRegular() || rs.Size() != ls.Size() {
			continue
		}
		lb, err := local.ReadFile(ctx, a.Path)
		if err != nil {
			continue
		}
		rb, err := remote.ReadFile(ctx, a.Path)
		if err != nil {
			continue
		}
		if bytes.Equal(lb, rb) {
			out.Actions[i] = Action{Path: a.Path, Op: OpAdopt}
		}
	}
	return out
}

// GuardMassDelete refuses a sync when one side has no file at all while
// the base knows files: a wrong folder or an unmounted volume would
// otherwise delete everything on the other side.
func GuardMassDelete(base Base, local, remote Snapshot) error {
	if len(base.Local) == 0 || len(base.Remote) == 0 {
		return nil
	}
	if len(local) == 0 {
		return errors.New("the workspace folder looks empty; nothing was changed")
	}
	if len(remote) == 0 {
		return errors.New("the container folder looks empty; nothing was changed")
	}
	return nil
}
