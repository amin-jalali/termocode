package containers

import (
	"strings"
	"testing"
)

const detailJSON = `[{"Id":"a1b2","Name":"/api-1","RestartCount":3,
 "Config":{"Env":["PATH=/usr/bin","DB_PASSWORD=hunter2","EMPTY="],"Labels":{"b":"2","a":"1"}},
 "State":{"Status":"exited","ExitCode":137,"Health":{"Status":"unhealthy","FailingStreak":2,
   "Log":[{"Start":"2026-11-01T10:00:00Z","ExitCode":1,"Output":"curl: (7) refused\n"}]}},
 "Mounts":[{"Type":"bind","Source":"/home/u/app","Destination":"/app","RW":true},
           {"Type":"volume","Name":"data","Source":"/var/lib/docker/volumes/data/_data","Destination":"/data","RW":false}]}]`

// CTX-10: the detail page reads exit code, restart count, health log,
// mounts, labels and the env NAMES only — never a value.
func TestParseDetail(t *testing.T) {
	d, err := ParseDetail([]byte(detailJSON))
	if err != nil {
		t.Fatal(err)
	}
	if d.ExitCode != 137 || d.RestartCount != 3 || d.Status != "exited" {
		t.Errorf("state %+v", d)
	}
	if strings.Join(d.EnvNames, ",") != "DB_PASSWORD,EMPTY,PATH" {
		t.Errorf("env names %v", d.EnvNames)
	}
	if strings.Contains(strings.ToLower(strings.Join(append(d.EnvNames, d.Health.Status), " ")), "hunter2") {
		t.Error("an env value leaked")
	}
	if d.Health.Status != "unhealthy" || d.Health.FailingStreak != 2 || len(d.Health.Log) != 1 || d.Health.Log[0].ExitCode != 1 || d.Health.Log[0].Output != "curl: (7) refused" {
		t.Errorf("health %+v", d.Health)
	}
	if len(d.Mounts) != 2 || d.Mounts[0].Destination != "/app" || !d.Mounts[0].RW || d.Mounts[1].Name != "data" || d.Mounts[1].RW {
		t.Errorf("mounts %+v", d.Mounts)
	}
	if len(d.Labels) != 2 || d.Labels[0].Key != "a" || d.Labels[1].Value != "2" {
		t.Errorf("labels %+v", d.Labels)
	}
	if _, err := ParseDetail([]byte(`[]`)); err == nil {
		t.Error("want an error for an empty inspect")
	}
}

// CTX-10: `top` output is a header and rows; the last column (the
// command) keeps its spaces.
func TestParseTop(t *testing.T) {
	out := "UID   PID   PPID  C  STIME  TTY  TIME      CMD\n" +
		"root  1234  1200  0  10:00  ?    00:00:01  nginx: master process nginx -g daemon off;\n" +
		"101   1300  1234  0  10:00  ?    00:00:00  nginx: worker process\n"
	tp := ParseTop(out)
	if strings.Join(tp.Titles, "|") != "UID|PID|PPID|C|STIME|TTY|TIME|CMD" {
		t.Fatalf("titles %q", tp.Titles)
	}
	if len(tp.Rows) != 2 || tp.Rows[0][1] != "1234" || tp.Rows[0][7] != "nginx: master process nginx -g daemon off;" || tp.Rows[1][7] != "nginx: worker process" {
		t.Errorf("rows %q", tp.Rows)
	}
	if len(ParseTop("").Rows) != 0 {
		t.Error("empty output has rows")
	}
}
