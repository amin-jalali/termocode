package kube

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestErrorLine(t *testing.T) {
	for _, c := range []struct {
		name, stderr string
		err          error
		kind         ErrKind
		want         string
	}{
		{"no kubectl", "", fmt.Errorf("start: %w", exec.ErrNotFound), ErrNoKubectl, "kubectl is not installed"},
		{"timeout", "", context.DeadlineExceeded, ErrTimeout, "kubectl took too long; the cluster may be down"},
		{"no context", "error: current-context is not set\n", nil, ErrNoConfig, "No cluster set up: kubeconfig has no current context"},
		{"no config", "The connection to the server localhost:8080 was refused - did you specify the right host or port?\n", nil, ErrNoConfig, "No cluster set up: kubeconfig has no current context"},
		{"down", "E1029 12:00:00 memcache.go:265] couldn't get current server API group list: Get \"https://10.0.0.1:6443/api\": dial tcp 10.0.0.1:6443: connect: connection refused\nUnable to connect to the server: dial tcp 10.0.0.1:6443: connect: connection refused\n", nil, ErrNoCluster, "Cannot reach the cluster"},
		{"forbidden", "Error from server (Forbidden): pods is forbidden: User \"dev\" cannot list resource \"pods\" in API group \"\" in the namespace \"kube-system\"\n", nil, ErrForbidden,
			"Not allowed: User \"dev\" cannot list resource \"pods\" in API group \"\" in the namespace \"kube-system\""},
		{"other", "error: the server doesn't have a resource type \"jobz\"\nmore\n", nil, ErrOther, "the server doesn't have a resource type \"jobz\""},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := ErrorLine(c.stderr, c.err)
			if KindOf(err) != c.kind {
				t.Errorf("kind = %v, want %v", KindOf(err), c.kind)
			}
			if err.Error() != c.want {
				t.Errorf("line = %q, want %q", err.Error(), c.want)
			}
			if strings.Contains(err.Error(), "\n") {
				t.Errorf("more than one line: %q", err.Error())
			}
		})
	}
}

func TestErrorLineClipsLongLines(t *testing.T) {
	err := ErrorLine("error: "+strings.Repeat("x", 500), nil)
	if n := len([]rune(err.Error())); n > maxLine {
		t.Errorf("line is %d runes", n)
	}
}
