package kube_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/kube"
)

func TestLogOptionsArgs(t *testing.T) {
	cases := []struct {
		o    kube.LogOptions
		want []string
	}{
		{kube.LogOptions{}, nil},
		{kube.LogOptions{Previous: true}, []string{"--previous"}},
		{kube.LogOptions{Since: 10 * time.Minute}, []string{"--since", "10m0s"}},
		{kube.LogOptions{Since: -time.Second}, nil},
		{kube.LogOptions{Timestamps: true}, []string{"--timestamps"}},
		{kube.LogOptions{Previous: true, Since: time.Hour, Timestamps: true}, []string{"--previous", "--since", "1h0m0s", "--timestamps"}},
	}
	for _, tc := range cases {
		if got := tc.o.Args(); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%+v args = %v, want %v", tc.o, got, tc.want)
		}
	}
}

func TestLogsCommandWithOptions(t *testing.T) {
	c := kube.Client{Binary: "/bin/kubectl", Context: "dev", Namespace: "shop"}
	// The plain command stays what it was.
	if plain, with := c.LogsCommand("api-1", "api"), c.LogsCommandWith("api-1", "api", kube.LogOptions{}); !reflect.DeepEqual(plain, with) {
		t.Errorf("zero options changed the command: %v vs %v", plain, with)
	}
	ts := c.LogsCommandWith("api-1", "api", kube.LogOptions{Since: 5 * time.Minute, Timestamps: true})
	if want := []string{"/bin/kubectl", "logs", "--follow", "--tail", "200", "--since", "5m0s", "--timestamps", "--context", "dev", "--namespace", "shop", "api-1", "--container", "api"}; !reflect.DeepEqual(ts.Argv, want) {
		t.Errorf("since argv = %v", ts.Argv)
	}
	if ts.Label != "logs: api-1/api (last 5m0s)" {
		t.Errorf("since label = %q", ts.Label)
	}
	// The previous container has ended: nothing to follow.
	prev := c.LogsCommandWith("api-1", "api", kube.LogOptions{Previous: true})
	if want := []string{"/bin/kubectl", "logs", "--tail", "200", "--previous", "--context", "dev", "--namespace", "shop", "api-1", "--container", "api"}; !reflect.DeepEqual(prev.Argv, want) {
		t.Errorf("previous argv = %v", prev.Argv)
	}
	if prev.Label != "logs: api-1/api (previous)" {
		t.Errorf("previous label = %q", prev.Label)
	}
}

func TestDeploymentLogsCommand(t *testing.T) {
	c := kube.Client{Binary: "/bin/kubectl", Context: "dev", Namespace: "shop"}
	d := c.DeploymentLogsCommand("api", kube.LogOptions{Timestamps: true})
	want := []string{"/bin/kubectl", "logs", "--follow", "--tail", "200", "--timestamps", "--all-pods", "--all-containers", "--prefix", "--max-log-requests", "20",
		"--context", "dev", "--namespace", "shop", "deployment/api"}
	if !reflect.DeepEqual(d.Argv, want) {
		t.Errorf("deployment argv = %v", d.Argv)
	}
	if d.Label != "logs: deployment/api" {
		t.Errorf("deployment label = %q", d.Label)
	}
}

func TestLogsWithReadsAllForSave(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "logs.txt", "old 1\nold 2\n")
	text, err := c.LogsWith(context.Background(), "api-1", "api", -1, kube.LogOptions{Previous: true, Timestamps: true})
	if err != nil || text != "old 1\nold 2\n" {
		t.Fatalf("logs = %q, %v", text, err)
	}
	if got := f.Calls(); len(got) != 1 || got[0] != "logs --tail -1 --previous --timestamps --context dev --namespace shop api-1 --container api" {
		t.Errorf("calls = %v", got)
	}
}
