package containers

import "testing"

// CTX-36: a running container's image has an update when the registry
// digest of its tag differs from the local repo digest; with no local
// digest for that repo the answer is unknown (no notice).
func TestImageHasUpdate(t *testing.T) {
	const a = "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const b = "sha256:" + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	local := []string{"redis@" + a, "ghcr.io/acme/api@" + b}
	cases := []struct {
		ref, remote  string
		update, know bool
	}{
		{"redis:7", a, false, true},
		{"redis:7", b, true, true},
		{"docker.io/library/redis:7", b, true, true},
		{"ghcr.io/acme/api:1.4", b, false, true},
		{"nginx:latest", a, false, false},
		{"redis:7", "", false, false},
		{"redis:7", "not-a-digest", false, false},
	}
	for _, c := range cases {
		u, k := ImageHasUpdate(local, c.ref, c.remote)
		if u != c.update || k != c.know {
			t.Errorf("%s vs %s: update=%v known=%v, want %v %v", c.ref, c.remote, u, k, c.update, c.know)
		}
	}
}
