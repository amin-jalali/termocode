package mcp

import "testing"

// Spec (MCPX-36): "gateway": true opts a server in to being re-served to
// outside agents; only the user's own file can opt in.
func TestServerGateway(t *testing.T) {
	cases := []struct {
		raw, source string
		want        bool
	}{
		{`{"command":"x"}`, SourceUser, false},
		{`{"command":"x","gateway":true}`, SourceUser, true},
		{`{"command":"x","gateway":false}`, SourceUser, false},
		{`{"command":"x","gateway":"yes"}`, SourceUser, false},
		{`{"command":"x","gateway":true}`, SourceProject, false},
	}
	for _, c := range cases {
		s := parseEntry("srv", []byte(c.raw))
		s.Source = c.source
		if got := s.Gateway(); got != c.want {
			t.Errorf("%s (%s): %v", c.raw, c.source, got)
		}
	}
}
