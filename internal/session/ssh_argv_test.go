package session

import (
	"reflect"
	"testing"
)

func TestSSHArgv(t *testing.T) {
	got := sshArgv([]string{"-p", "2222", "me@box"}, "termocode", "/srv/my app", "", 0)
	want := []string{"-t", "-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=3", "-o", "ConnectTimeout=10",
		"-p", "2222", "me@box", "--", "termocode --link '/srv/my app'"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("first connect:\n got %q\nwant %q", got, want)
	}
	// The user's own keep-alive and timeout win; a reconnect says so.
	got = sshArgv([]string{"-o", "ServerAliveInterval=30", "-oConnectTimeout=3", "box"}, "~/bin/termocode", "~/it's", "12s,dropped", 0)
	want = []string{"-t", "-o", "ServerAliveInterval=30", "-oConnectTimeout=3", "box", "--",
		`~/bin/termocode --link --reconnected=12s,dropped ~/'it'\''s'`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reconnect:\n got %q\nwant %q", got, want)
	}
	// No folder: the login folder on the far side.
	if got := sshArgv([]string{"box"}, "termocode", "", "", 0); got[len(got)-1] != "termocode --link" {
		t.Errorf("no folder: %q", got[len(got)-1])
	}
	// With --web the far termocode is told the local port of the forward.
	if got := sshArgv([]string{"box"}, "termocode", "/w", "3s", 41237); got[len(got)-1] != "termocode --link --web=41237 --reconnected=3s '/w'" {
		t.Errorf("web: %q", got[len(got)-1])
	}
}

// The web forward (WEB-29) is a second ssh with the user's own arguments:
// no command, no terminal, no prompt (it has no terminal to ask on), no
// shared master (a stale one cannot forward), and one local forward from
// 127.0.0.1 only to the far gateway's unix socket.
func TestSSHForwardArgv(t *testing.T) {
	got := SSHForwardArgv([]string{"-p", "2222", "me@box"}, 41237, "/run/user/1000/termocode/0123456789abcdef.webhttp.sock")
	want := []string{"-N", "-T", "-o", "ExitOnForwardFailure=yes", "-o", "ControlPath=none", "-o", "BatchMode=yes",
		"-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=3", "-o", "ConnectTimeout=10",
		"-L", "127.0.0.1:41237:/run/user/1000/termocode/0123456789abcdef.webhttp.sock",
		"-p", "2222", "me@box"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("forward:\n got %q\nwant %q", got, want)
	}
	// The user's own keep-alive and timeout win, as for the main ssh.
	got = SSHForwardArgv([]string{"-o", "ServerAliveInterval=30", "-oConnectTimeout=3", "box"}, 2000, "/s.sock")
	for _, a := range got {
		if a == "ServerAliveInterval=5" || a == "ConnectTimeout=10" {
			t.Errorf("forward overrides the user's %s: %q", a, got)
		}
	}
	if got[len(got)-1] != "box" {
		t.Errorf("the destination is not last: %q", got)
	}
}
