package worktrees

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ── A port per worktree (CMP-5) ──────────────────────────────────────────
//
// Each worktree that runs the repository's dev server gets its own port,
// so parallel worktrees never fight for the same one. The port is the
// lowest one of the range that no other worktree holds and nothing
// listens on; it is kept in worktrees.json (Meta.Port), so a worktree
// keeps its port across restarts.

// PortRange is an inclusive range of TCP ports.
type PortRange struct{ Lo, Hi int }

// DefaultPortRange is used when the repository names none.
var DefaultPortRange = PortRange{Lo: 3001, Hi: 3099}

// MaxPortRange is the widest range accepted.
const MaxPortRange = 1000

// ParsePortRange reads "lo-hi" or one port; empty is DefaultPortRange.
func ParsePortRange(s string) (PortRange, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultPortRange, nil
	}
	lo, hi, dash := strings.Cut(s, "-")
	if !dash {
		hi = lo
	}
	a, err1 := strconv.Atoi(strings.TrimSpace(lo))
	b, err2 := strconv.Atoi(strings.TrimSpace(hi))
	if err1 != nil || err2 != nil {
		return PortRange{}, fmt.Errorf("ports %q: write it like 3000-3099", s)
	}
	switch {
	case a < 1024 || b > 65535:
		return PortRange{}, errors.New("ports must be between 1024 and 65535")
	case a > b:
		return PortRange{}, fmt.Errorf("ports %q: the first port is larger than the last", s)
	case b-a+1 > MaxPortRange:
		return PortRange{}, fmt.Errorf("ports %q: at most %d ports", s, MaxPortRange)
	}
	return PortRange{Lo: a, Hi: b}, nil
}

// String is the range as written in the config.
func (r PortRange) String() string {
	if r.Lo == r.Hi {
		return strconv.Itoa(r.Lo)
	}
	return strconv.Itoa(r.Lo) + "-" + strconv.Itoa(r.Hi)
}

// PickPort is the lowest port of r not in taken that free says is free.
func PickPort(r PortRange, taken map[int]bool, free func(int) bool) (int, bool) {
	for p := r.Lo; p <= r.Hi; p++ {
		if !taken[p] && free(p) {
			return p, true
		}
	}
	return 0, false
}

// TakenPorts are the ports the worktrees other than except hold.
func TakenPorts(meta map[string]Meta, except string) map[int]bool {
	out := map[int]bool{}
	for path, md := range meta {
		if path != except && md.Port > 0 {
			out[md.Port] = true
		}
	}
	return out
}

// PortFree says whether nothing listens on port p of this machine.
func PortFree(p int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}
