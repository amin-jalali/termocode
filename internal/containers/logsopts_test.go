package containers

import (
	"reflect"
	"testing"
)

func TestLogsFollowWithOptions(t *testing.T) {
	e := Engine{Binary: "/usr/bin/docker"}
	cases := []struct {
		opt  LogsOptions
		want []string
	}{
		{LogsOptions{}, []string{"/usr/bin/docker", "logs", "-f", "--tail", "200", "abc"}},
		{LogsOptions{Tail: -1, Timestamps: true}, []string{"/usr/bin/docker", "logs", "-f", "--tail", "all", "--timestamps", "abc"}},
		{LogsOptions{Tail: 50, Since: "10m"}, []string{"/usr/bin/docker", "logs", "-f", "--tail", "50", "--since", "10m", "abc"}},
		{LogsOptions{Tail: 50, Since: "2026-11-07T10:00:00Z"}, []string{"/usr/bin/docker", "logs", "-f", "--tail", "50", "--since", "2026-11-07T10:00:00Z", "abc"}},
	}
	for _, c := range cases {
		cmd, err := e.LogsFollowWith("abc", c.opt)
		if err != nil {
			t.Fatalf("%+v: %v", c.opt, err)
		}
		if !reflect.DeepEqual(cmd.Argv, c.want) || cmd.Label != "logs: abc" {
			t.Fatalf("%+v:\n got %q\nwant %q", c.opt, cmd.Argv, c.want)
		}
	}
	if got := e.LogsFollow("abc").Argv; !reflect.DeepEqual(got, cases[0].want) {
		t.Fatalf("LogsFollow must keep its default: %q", got)
	}
}

func TestLogsOptionsRejectBadSince(t *testing.T) {
	e := Engine{Binary: "docker"}
	for _, bad := range []string{"--privileged", "yesterday", "10", "-5m", "10m;rm", "abc"} {
		if _, err := e.LogsFollowWith("abc", LogsOptions{Since: bad}); err == nil {
			t.Fatalf("since %q must be rejected", bad)
		}
	}
	if _, err := e.LogsFollowWith("abc", LogsOptions{Tail: 100001}); err == nil {
		t.Fatal("huge tail must be rejected")
	}
}

func TestParseLogsSince(t *testing.T) {
	for in, want := range map[string]string{"": "", " 15m ": "15m", "1h30m": "1h30m", "2026-11-07": "2026-11-07", "2026-11-07T10:00:00+02:00": "2026-11-07T10:00:00+02:00"} {
		got, err := ParseLogsSince(in)
		if err != nil || got != want {
			t.Fatalf("%q: %q %v", in, got, err)
		}
	}
}
