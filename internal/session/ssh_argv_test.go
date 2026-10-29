package session

import (
	"reflect"
	"testing"
)

func TestSSHArgv(t *testing.T) {
	got := sshArgv([]string{"-p", "2222", "me@box"}, "termocode", "/srv/my app", "")
	want := []string{"-t", "-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=3", "-o", "ConnectTimeout=10",
		"-p", "2222", "me@box", "--", "termocode --link '/srv/my app'"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("first connect:\n got %q\nwant %q", got, want)
	}
	// The user's own keep-alive and timeout win; a reconnect says so.
	got = sshArgv([]string{"-o", "ServerAliveInterval=30", "-oConnectTimeout=3", "box"}, "~/bin/termocode", "~/it's", "12s,dropped")
	want = []string{"-t", "-o", "ServerAliveInterval=30", "-oConnectTimeout=3", "box", "--",
		`~/bin/termocode --link --reconnected=12s,dropped ~/'it'\''s'`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reconnect:\n got %q\nwant %q", got, want)
	}
	// No folder: the login folder on the far side.
	if got := sshArgv([]string{"box"}, "termocode", "", ""); got[len(got)-1] != "termocode --link" {
		t.Errorf("no folder: %q", got[len(got)-1])
	}
}
