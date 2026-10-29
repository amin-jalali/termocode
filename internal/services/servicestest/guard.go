package servicestest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GuardedCLIs are the container and cluster tools a test must never run
// for real: they reach the developer's (or the CI runner's) Docker
// daemon, podman machine or Kubernetes cluster.
var GuardedCLIs = []string{"docker", "podman", "kubectl", "docker-compose"}

// GuardTestEnv names the test that runs: the guard writes it in front of
// every blocked call, so a call can be traced back to its test.
const GuardTestEnv = "TERMOCODE_GUARD_TEST"

const guardScript = `#!/bin/sh
printf '%s|%s %s\n' "${TERMOCODE_GUARD_TEST-}" "${0##*/}" "$*" >> '@LOG@'
echo "test guard: the real ${0##*/} is blocked in tests (use servicestest)" >&2
exit 1
`

// Guard is a directory of stand-ins for GuardedCLIs that fail closed:
// each call is written to Log and exits 1 without running anything.
// A TestMain puts Bin first on PATH, so a test that forgets its fake
// never reaches the real tool; a fake (Fake.PathEnv) still comes first.
type Guard struct {
	Bin string
	Log string
}

// InstallGuard writes the stand-ins into dir (made if missing). It takes
// no testing.TB: TestMain calls it before any test runs.
func InstallGuard(dir string) (*Guard, error) {
	g := &Guard{Bin: filepath.Join(dir, "bin"), Log: filepath.Join(dir, "blocked.log")}
	if err := os.MkdirAll(g.Bin, 0o755); err != nil {
		return nil, err
	}
	body := strings.ReplaceAll(guardScript, "@LOG@", g.Log)
	for _, name := range GuardedCLIs {
		if err := os.WriteFile(filepath.Join(g.Bin, name), []byte(body), 0o755); err != nil {
			return nil, err
		}
	}
	return g, nil
}

// PathEnv is a PATH value with the guard first.
func (g *Guard) PathEnv() string {
	return g.Bin + string(os.PathListSeparator) + os.Getenv("PATH")
}

// Calls returns the blocked calls, "<test>|<tool> <args>", one per line.
// With test set, only that test's calls.
func (g *Guard) Calls(test string) []string {
	b, _ := os.ReadFile(g.Log)
	var out []string
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if l == "" {
			continue
		}
		if test == "" || strings.HasPrefix(l, test+"|") {
			out = append(out, l)
		}
	}
	return out
}

// Report is the message for blocked calls, or "" when there were none.
func (g *Guard) Report(test string) string {
	calls := g.Calls(test)
	if len(calls) == 0 {
		return ""
	}
	return fmt.Sprintf("a test ran a real container/cluster tool (blocked; use servicestest.New and put its PathEnv on PATH):\n  %s",
		strings.Join(calls, "\n  "))
}
