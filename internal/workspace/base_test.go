package workspace

import "testing"

// A container root is a "/" path on every OS; its title is the last part.
func TestBaseOfContainerRoot(t *testing.T) {
	cases := map[Workspace]string{
		{Kind: KindContainer, Root: "/srv/app"}:  "app",
		{Kind: KindContainer, Root: "/srv/app/"}: "app",
		{Kind: KindContainer, Root: "/"}:         "/",
	}
	for w, want := range cases {
		if got := w.Base(); got != want {
			t.Errorf("Base(%q) = %q, want %q", w.Root, got, want)
		}
	}
}
