package ghci

import (
	"context"
	"strings"
	"testing"
)

func TestMergeArgs(t *testing.T) {
	for m, want := range map[MergeMethod]string{
		MergeSquash: "pr merge 7 --squash", MergeCommit: "pr merge 7 --merge", MergeRebase: "pr merge 7 --rebase",
	} {
		f := &fakeGH{answers: map[string]fakeAnswer{"pr merge": {}}}
		if err := Merge(context.Background(), f.run, "/r", 7, m); err != nil {
			t.Fatal(err)
		}
		got := strings.Join(f.calls[0], " ")
		if got != want {
			t.Errorf("args %q want %q", got, want)
		}
		for _, bad := range []string{"--admin", "--auto", "--delete-branch"} {
			if strings.Contains(got, bad) {
				t.Errorf("%s passed", bad)
			}
		}
	}
}

func TestMergeUnknownMethodAndError(t *testing.T) {
	f := &fakeGH{}
	if err := Merge(context.Background(), f.run, "/r", 7, "fast"); err == nil || len(f.calls) != 0 {
		t.Errorf("err %v calls %d", err, len(f.calls))
	}
	f = &fakeGH{answers: map[string]fakeAnswer{"pr merge": {errOut: "X Pull request #7 is not mergeable: the base branch policy prohibits the merge.", err: errString("exit 1")}}}
	if err := Merge(context.Background(), f.run, "/r", 7, MergeSquash); err == nil || !strings.Contains(err.Error(), "not mergeable") {
		t.Errorf("err %v", err)
	}
}
