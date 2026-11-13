package services

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCaptureInputFeedsStdinAndReportsExit(t *testing.T) {
	x := SpawnExec{}
	ctx := context.Background()
	out, _, code, err := x.CaptureInput(ctx, t.TempDir(), []byte("hello"), "cat")
	if err != nil || code != 0 || out != "hello" {
		t.Errorf("cat = %q, %d, %v", out, code, err)
	}
	_, errOut, code, err := x.CaptureInput(ctx, t.TempDir(), []byte("x"), "sh", "-c", "cat >/dev/null; echo oops >&2; exit 4")
	if err != nil || code != 4 || !strings.Contains(errOut, "oops") {
		t.Errorf("exit 4 = %q, %d, %v", errOut, code, err)
	}
	if _, _, code, err := x.CaptureInput(ctx, t.TempDir(), nil); err == nil || code != -1 {
		t.Errorf("no command = %d, %v", code, err)
	}
	if _, _, code, err := x.CaptureInput(ctx, t.TempDir(), nil, "/no/such/program"); err == nil || code != -1 {
		t.Errorf("a program that cannot start = %d, %v", code, err)
	}
	slow, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if _, _, code, err := x.CaptureInput(slow, t.TempDir(), nil, "sleep", "5"); err == nil || code != -1 {
		t.Errorf("a cancelled run = %d, %v", code, err)
	}
}

func TestWithEnvReplacesNamesAndKeepsOtherExecs(t *testing.T) {
	x := WithEnv(SpawnExec{Env: []string{"A=1", "B=2", "AB=3"}}, "A=9", "C=4")
	se, ok := x.(SpawnExec)
	if !ok {
		t.Fatalf("WithEnv gave %T", x)
	}
	got := strings.Join(se.Env, ",")
	if got != "B=2,AB=3,A=9,C=4" {
		t.Errorf("env = %s", got)
	}
	// Without an environment of its own the process's is the base.
	t.Setenv("TERMOCODE_COV_X", "keep")
	se2 := WithEnv(SpawnExec{}, "TERMOCODE_COV_Y=1").(SpawnExec)
	if !strings.Contains(strings.Join(se2.Env, "\n"), "TERMOCODE_COV_X=keep") {
		t.Error("the process environment is the base")
	}
	other := &fakeExec{}
	if WithEnv(other, "A=1") != Exec(other) {
		t.Error("another Exec is returned unchanged")
	}
}

type fakeExec struct{ Exec }

func TestGitDeployBaseFollowsXDGThenHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/data")
	if got := GitDeployBase(); got != filepath.Join("/data", "termocode", "stacks") {
		t.Errorf("with XDG = %s", got)
	}
	t.Setenv("XDG_DATA_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := GitDeployBase(); got != filepath.Join(home, ".local", "share", "termocode", "stacks") {
		t.Errorf("without XDG = %s", got)
	}
}

func TestReplicaRankOrdersWorstLast(t *testing.T) {
	order := []State{Healthy, Running, Starting, Stopping, Paused, Unknown, NotCreated, Stopped, Unhealthy, Restarting, Exited}
	for i := 1; i < len(order); i++ {
		if replicaRank(order[i-1]) >= replicaRank(order[i]) {
			t.Errorf("%v should rank below %v", order[i-1], order[i])
		}
	}
	if replicaRank(Pulling) != replicaRank(Starting) {
		t.Error("pulling ranks as starting")
	}
	if replicaRank(State(-7)) != replicaRank(Unknown) {
		t.Error("an unknown state ranks as unknown")
	}
}
