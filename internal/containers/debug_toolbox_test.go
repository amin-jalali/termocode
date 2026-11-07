package containers

import (
	"reflect"
	"testing"
)

// The debug toolbox is a separate, throw-away container that joins the
// target's process and network namespaces; the target is not changed.
func TestDebugToolboxJoinsTheTargetWithoutChangingIt(t *testing.T) {
	e := &Engine{Binary: "/usr/bin/docker", Kind: "docker"}
	c, err := e.DebugToolbox("api-1", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/usr/bin/docker", "run", "--rm", "-it",
		"--pid", "container:api-1", "--network", "container:api-1",
		"--label", "termocode.debug=api-1",
		DefaultDebugImage, "sh"}
	if c.Label != "debug: api-1" || !reflect.DeepEqual(c.Argv, want) {
		t.Fatalf("command = %q %q", c.Label, c.Argv)
	}
	for _, a := range c.Argv {
		switch a {
		case "--privileged", "--cap-add", "-v", "--volumes-from", "exec", "cp":
			t.Fatalf("the toolbox asks for %s: %q", a, c.Argv)
		}
	}
	c, err = e.DebugToolbox("api-1", "nicolaka/netshoot:v0.13")
	if err != nil || c.Argv[len(c.Argv)-2] != "nicolaka/netshoot:v0.13" {
		t.Fatalf("own image: %q, %v", c.Argv, err)
	}
}

func TestDebugToolboxRefusesFlagsAsNames(t *testing.T) {
	e := &Engine{Binary: "docker", Kind: "docker"}
	for _, tc := range [][2]string{{"", ""}, {"-x", ""}, {"api-1", "--privileged"}, {"api 1", ""}, {"api-1", "img with space"}} {
		if _, err := e.DebugToolbox(tc[0], tc[1]); err == nil {
			t.Errorf("DebugToolbox(%q, %q) was accepted", tc[0], tc[1])
		}
	}
}
