//go:build unix

package agentenv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunSetupRunsInOrderInTheDir(t *testing.T) {
	dir := t.TempDir()
	out, err := RunSetup(context.Background(), dir, []string{"PATH=" + os.Getenv("PATH"), "GREETING=hi"},
		[]string{"echo $GREETING > a.txt", "cat a.txt >> b.txt"}, time.Minute, nil)
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	b, err := os.ReadFile(filepath.Join(dir, "b.txt"))
	if err != nil || strings.TrimSpace(string(b)) != "hi" {
		t.Errorf("b.txt %q %v", b, err)
	}
}

func TestRunSetupStopsAtTheFirstFailureAndRedacts(t *testing.T) {
	dir := t.TempDir()
	out, err := RunSetup(context.Background(), dir, []string{"PATH=" + os.Getenv("PATH"), "TOK=s3cret"},
		[]string{"echo using $TOK; exit 3", "touch never"}, time.Minute, []string{"s3cret"})
	if err == nil {
		t.Fatal("no error")
	}
	if strings.Contains(out, "s3cret") || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("secret in output: %q / %v", out, err)
	}
	if !strings.Contains(err.Error(), "setup 1") {
		t.Errorf("err %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "never")); err == nil {
		t.Error("ran after a failure")
	}
}

func TestRunSetupTimeout(t *testing.T) {
	_, err := RunSetup(context.Background(), t.TempDir(), []string{"PATH=" + os.Getenv("PATH")},
		[]string{"sleep 5"}, 50*time.Millisecond, nil)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("err %v", err)
	}
}
