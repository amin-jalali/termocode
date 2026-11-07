package testkit

import (
	"os"
	"testing"
)

// STAB-8: git sets GIT_DIR, GIT_INDEX_FILE, GIT_WORK_TREE … for hooks
// (a pre-push `go test`) and a user may export GIT_CONFIG_GLOBAL. Leaked
// into a test, every git call in the temp workspace then works on the
// outer repo (its stash, its index) and git tests fail with
// "stash@{0} is not a valid reference". The sandbox drops them all.
func TestSandboxDropsLeakedGitRepoEnv(t *testing.T) {
	outer := t.TempDir()
	for _, k := range sandboxGitUnset {
		t.Setenv(k, outer)
	}
	sandbox(t, false)
	for _, k := range sandboxGitUnset {
		if v, ok := os.LookupEnv(k); ok {
			t.Errorf("%s still set to %q inside the sandbox", k, v)
		}
	}
}

// The list covers the variables that point git at another repo or config.
func TestSandboxGitUnsetListCoversRepoVars(t *testing.T) {
	want := []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR",
		"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE",
		"GIT_CEILING_DIRECTORIES", "GIT_CONFIG", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM",
		"GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_PREFIX"}
	have := map[string]bool{}
	for _, k := range sandboxGitUnset {
		have[k] = true
	}
	for _, k := range want {
		if !have[k] {
			t.Errorf("sandboxGitUnset misses %s", k)
		}
	}
}
