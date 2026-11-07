package session

import (
	"reflect"
	"strings"
	"testing"
)

// Spec (WEB-54): a port forward request of the far UI is taken out of
// the output wherever the reads split it, next to a web hand-off, and
// read back; open carries a port and an id, close only the id.
func TestAPCFwdFilter(t *testing.T) {
	open := PortForwardAPC(PortForward{Open: true, Port: 3000, ID: "tc0a1b2c3d4e5f"})
	closeIt := PortForwardAPC(PortForward{ID: "tc0a1b2c3d4e5f"})
	if open != "\x1b_termocode:fwd;1;3000;tc0a1b2c3d4e5f\x1b\\" || closeIt != "\x1b_termocode:fwd;0;tc0a1b2c3d4e5f\x1b\\" {
		t.Fatalf("APC %q / %q", open, closeIt)
	}
	stream := []byte("a" + WebHandoffAPC(testHandoff) + "b" + open + "c" + closeIt + "d")
	for i := 0; i <= len(stream); i++ {
		out, found := feed(stream[:i], stream[i:])
		if string(out) != "abcd" {
			t.Fatalf("split %d: out %q", i, out)
		}
		if len(found) != 3 || found[0].Fwd != nil || found[1].Fwd == nil || found[2].Fwd == nil {
			t.Fatalf("split %d: found %+v", i, found)
		}
		if *found[1].Fwd != (PortForward{Open: true, Port: 3000, ID: "tc0a1b2c3d4e5f"}) ||
			*found[2].Fwd != (PortForward{ID: "tc0a1b2c3d4e5f"}) {
			t.Fatalf("split %d: %+v %+v", i, *found[1].Fwd, *found[2].Fwd)
		}
	}
}

// Spec (WEB-54): there is no host field. A request that names a host,
// a bad port or an odd id is removed but never used, and the encoder
// refuses to make one.
func TestAPCFwdFilterRefusesBad(t *testing.T) {
	for _, bad := range []string{
		"1;3000;evil.example;tc1",
		"1;127.0.0.2:3000;tc1",
		"1;0;tc1", "1;70000;tc1", "1;03000;tc1", "1;-1;tc1",
		"1;3000;TC1", "1;3000;", "1;3000;" + strings.Repeat("a", 17),
		"2;3000;tc1", "0;", "0;a/b",
	} {
		out, found := feed([]byte("x\x1b_termocode:fwd;" + bad + "\x1b\\y"))
		if string(out) != "xy" || len(found) != 0 {
			t.Errorf("%q: out %q found %+v", bad, out, found)
		}
	}
	for _, f := range []PortForward{{Open: true, Port: 0, ID: "a"}, {Open: true, Port: 3000, ID: "A"}, {ID: ""}} {
		if s := PortForwardAPC(f); s != "" {
			t.Errorf("%+v encoded as %q", f, s)
		}
	}
}

// Spec (WEB-54): the preview's ssh forwards 127.0.0.1 here to 127.0.0.1
// there, with the user's own arguments after the options, no command
// and no terminal.
func TestSSHPortForwardArgvLoopbackOnly(t *testing.T) {
	got := SSHPortForwardArgv([]string{"-p", "2222", "me@box"}, 50123, 3000)
	want := []string{"-N", "-T", "-o", "ExitOnForwardFailure=yes", "-o", "ControlPath=none", "-o", "BatchMode=yes",
		"-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=3", "-o", "ConnectTimeout=10",
		"-L", "127.0.0.1:50123:127.0.0.1:3000", "-p", "2222", "me@box"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("argv %q\nwant %q", got, want)
	}
}
