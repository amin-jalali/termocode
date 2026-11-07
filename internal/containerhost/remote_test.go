package containerhost

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
)

func TestEndpointReadsTheContext(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	d := fakedocker.New()
	d.SetEndpoint("prod-ssh", "ssh://me@prod")
	d.SetEndpoint("", "unix:///var/run/docker.sock")
	if got := Endpoint(t.Context(), d, "", "prod-ssh"); got != "ssh://me@prod" {
		t.Fatalf("Endpoint(prod-ssh) = %q", got)
	}
	for _, c := range d.Calls() {
		if c.Context != "prod-ssh" {
			t.Fatalf("call without the context flag: %+v", c)
		}
	}
	if got := Endpoint(t.Context(), d, "", ""); got != "unix:///var/run/docker.sock" {
		t.Fatalf("Endpoint(current) = %q", got)
	}
	if got := Endpoint(t.Context(), d, "", "missing"); got != "" {
		t.Fatalf("Endpoint(missing) = %q", got)
	}
	if got := Endpoint(t.Context(), d, "", "-H"); got != "" {
		t.Fatalf("an option as context: %q", got)
	}
}

func TestEndpointPrefersDockerHostForTheCurrentContext(t *testing.T) {
	t.Setenv("DOCKER_HOST", "ssh://box")
	d := fakedocker.New()
	if got := Endpoint(t.Context(), d, "", ""); got != "ssh://box" {
		t.Fatalf("Endpoint = %q", got)
	}
	if len(d.Calls()) != 0 {
		t.Fatal("DOCKER_HOST needs no engine call")
	}
}

func TestRemoteEndpoint(t *testing.T) {
	for ep, want := range map[string]bool{
		"ssh://me@host": true, "SSH://h": true, "tcp://h:2376": true,
		"unix:///var/run/docker.sock": false, "npipe:////./pipe/docker_engine": false, "": false,
	} {
		if got := RemoteEndpoint(ep); got != want {
			t.Errorf("RemoteEndpoint(%q) = %v", ep, got)
		}
	}
	if !SSHEndpoint("ssh://h") || SSHEndpoint("tcp://h") {
		t.Fatal("SSHEndpoint")
	}
}
