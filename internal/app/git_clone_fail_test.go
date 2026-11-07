package app

import (
	"errors"
	"testing"
)

// A failed clone's toast detail starts with git's reason, not with the
// exit status and the URL (they cut the reason off in the card).
func TestCloneFailDetailLeadsWithTheReason(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"exit status 128: fatal: unable to access 'http://127.0.0.1:1/repo.git/': Failed to connect to 127.0.0.1 port 1 after 0 ms: Couldn't connect to server",
			"Failed to connect to 127.0.0.1 port 1 after 0 ms: Couldn't connect to server"},
		{"exit status 128: fatal: repository 'https://example.com/x.git/' not found", "repository 'https://example.com/x.git/' not found"},
		{"exit status 1: line one\nfatal: unable to access 'u': Could not resolve host: example.com", "Could not resolve host: example.com"},
		{"exit status 1", "exit status 1"},
		{"destination path exists", "destination path exists"},
	} {
		if got := cloneFailDetail(errors.New(c.in)); got != c.want {
			t.Errorf("cloneFailDetail(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
