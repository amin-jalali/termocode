package session

import (
	"testing"
)

// RC-5 (4e): a container workspace's session files are keyed on its
// workspace ID, so two containers with the same /app never share a
// socket, and neither shares one with a local /app.
func TestPathsForIDKeysAContainerOnItsID(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	a, err := PathsForID("container:default/api-1:/app")
	if err != nil {
		t.Fatal(err)
	}
	b, err := PathsForID("container:prod/api-1:/app")
	if err != nil {
		t.Fatal(err)
	}
	local, err := PathsFor("/app")
	if err != nil {
		t.Fatal(err)
	}
	if a.MCPSocket == b.MCPSocket || a.MCPSocket == local.MCPSocket {
		t.Fatalf("sockets must differ: %q %q local %q", a.MCPSocket, b.MCPSocket, local.MCPSocket)
	}
	if a.Key != KeyForID("container:default/api-1:/app") {
		t.Fatalf("key %q, want the ID's key", a.Key)
	}
	if a.Workspace != "container:default/api-1:/app" {
		t.Fatalf("Workspace %q, want the ID", a.Workspace)
	}
	// A local ID is the folder's paths, as always.
	l2, err := PathsForID("local:/app")
	if err != nil {
		t.Fatal(err)
	}
	if l2 != local {
		t.Fatalf("local ID paths %+v, want %+v", l2, local)
	}
	if _, err := PathsForID("bogus"); err == nil {
		t.Fatal("an ID of no known kind must be an error")
	}
}
