package kube_test

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/kube"
)

func TestDiffFileNoChanges(t *testing.T) {
	f, c := fakeClient(t)
	d, err := c.DiffFile(context.Background(), "/work/app.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if d.Changed || d.Text != "" {
		t.Errorf("diff = %+v, want no changes", d)
	}
	if got := f.Calls(); len(got) != 1 || got[0] != "diff --context dev --namespace shop -f /work/app.yaml" {
		t.Errorf("calls = %v", got)
	}
}

func TestDiffFileChangesAreNotAnError(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "diff.txt", "-  replicas: 1\n+  replicas: 3\n")
	d, err := c.DiffFile(context.Background(), "app.yaml")
	if err != nil {
		t.Fatalf("exit 1 with a diff must not be an error: %v", err)
	}
	if !d.Changed || !strings.Contains(d.Text, "+  replicas: 3") {
		t.Errorf("diff = %+v", d)
	}
}

func TestDiffFileServerErrorIsAnError(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "diff.err", "Error from server (Forbidden): deployments.apps \"api\" is forbidden: User \"dev\" cannot patch resource\n")
	if _, err := c.DiffFile(context.Background(), "app.yaml"); kube.KindOf(err) != kube.ErrForbidden {
		t.Errorf("err = %v", err)
	}
}

func TestDiffFileNeedsAPath(t *testing.T) {
	f, c := fakeClient(t)
	for _, p := range []string{"", "-", "--server-side"} {
		if _, err := c.DiffFile(context.Background(), p); err == nil {
			t.Errorf("%q: want error", p)
		}
		if _, err := c.ApplyFile(context.Background(), p); err == nil {
			t.Errorf("apply %q: want error", p)
		}
	}
	if n := len(f.Calls()); n != 0 {
		t.Errorf("kubectl ran %d times for a bad path", n)
	}
}

func TestApplyFile(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "apply.txt", "deployment.apps/api configured\n")
	out, err := c.ApplyFile(context.Background(), "/work/app.yaml")
	if err != nil || !strings.Contains(out, "deployment.apps/api configured") {
		t.Fatalf("apply = %q, %v", out, err)
	}
	if got := f.Calls(); len(got) != 1 || got[0] != "apply --context dev --namespace shop -f /work/app.yaml" {
		t.Errorf("calls = %v", got)
	}
	f.Set(t, "apply.err", "error: error validating \"app.yaml\": unknown field\n")
	if _, err := c.ApplyFile(context.Background(), "/work/app.yaml"); err == nil {
		t.Error("apply failure must be an error")
	}
}
