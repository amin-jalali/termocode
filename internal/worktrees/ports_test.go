package worktrees

import (
	"net"
	"testing"
)

// CMP-5: a run port range is "lo-hi" (or one port); empty is the
// default; nonsense, privileged ports and huge ranges are refused.
func TestParsePortRange(t *testing.T) {
	for in, want := range map[string]PortRange{
		"":             DefaultPortRange,
		"  ":           DefaultPortRange,
		"3000-3010":    {3000, 3010},
		" 4000 - 4005": {4000, 4005},
		"5173":         {5173, 5173},
	} {
		got, err := ParsePortRange(in)
		if err != nil || got != want {
			t.Errorf("%q: %+v %v, want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"abc", "3010-3000", "80-90", "3000-", "60000-70000", "1100-9999", "3000-3010-3020"} {
		if _, err := ParsePortRange(in); err == nil {
			t.Errorf("%q: no error", in)
		}
	}
	if DefaultPortRange.Lo < 1024 || DefaultPortRange.Hi < DefaultPortRange.Lo {
		t.Errorf("default %+v", DefaultPortRange)
	}
}

// CMP-5: the port of a worktree is the lowest one of the range that no
// other worktree holds and nothing listens on.
func TestPickPort(t *testing.T) {
	r := PortRange{3000, 3003}
	all := func(int) bool { return true }
	if p, ok := PickPort(r, nil, all); !ok || p != 3000 {
		t.Errorf("empty: %d %v", p, ok)
	}
	if p, ok := PickPort(r, map[int]bool{3000: true, 3001: true}, all); !ok || p != 3002 {
		t.Errorf("taken: %d %v", p, ok)
	}
	busy := func(p int) bool { return p != 3002 }
	if p, ok := PickPort(r, map[int]bool{3000: true}, busy); !ok || p != 3001 {
		t.Errorf("busy: %d %v", p, ok)
	}
	if p, ok := PickPort(r, map[int]bool{3000: true, 3001: true, 3002: true}, busy); !ok || p != 3003 {
		t.Errorf("last: %d %v", p, ok)
	}
	if _, ok := PickPort(r, map[int]bool{3000: true, 3001: true, 3002: true, 3003: true}, all); ok {
		t.Error("a full range gave a port")
	}
}

// CMP-5: the ports other worktrees hold; the asking worktree's own port
// does not count.
func TestTakenPorts(t *testing.T) {
	m := map[string]Meta{"/a": {Port: 3000}, "/b": {Port: 3001}, "/c": {Name: "x"}}
	got := TakenPorts(m, "/a")
	if len(got) != 1 || !got[3001] {
		t.Errorf("taken %v", got)
	}
}

// CMP-5: the port is kept in worktrees.json, so the worktree gets the
// same port next time.
func TestMetaPortKept(t *testing.T) {
	common := t.TempDir()
	if err := Update(common, func(m map[string]Meta) { m["/src/app.worktrees/x"] = Meta{Port: 3004} }); err != nil {
		t.Fatal(err)
	}
	m, err := Load(common)
	if err != nil || m["/src/app.worktrees/x"].Port != 3004 {
		t.Fatalf("%+v %v", m, err)
	}
}

// CMP-5: PortFree says no for a port something listens on.
func TestPortFree(t *testing.T) {
	l, p, err := listenAny()
	if err != nil {
		t.Skip("no loopback:", err)
	}
	defer l.Close()
	if PortFree(p) {
		t.Errorf("port %d is in use but free", p)
	}
}

// listenAny listens on a free loopback port.
func listenAny() (net.Listener, int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}
	return l, l.Addr().(*net.TCPAddr).Port, nil
}
