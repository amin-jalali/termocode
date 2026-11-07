package tests

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// ExitMarker starts the last line a remote run prints: the exit code of
// the test command. A stream through the engine CLI has no exit status
// of its own, so the wrapper script says it.
const ExitMarker = "__termocode_test_exit="

// remoteScript runs "$@" with stderr on stdout (the stream carries only
// stdout) and prints the exit code as the last line. pytest falls back
// to `python3 -m pytest` when the image has no pytest script. The test
// command stays positional arguments: no shell reads it as code. %s is
// the env prefix ("" or "env 'K=V' … ").
const remoteScript = `if [ "$1" = pytest ] && ! command -v pytest >/dev/null 2>&1; then shift; set -- python3 -m pytest "$@"; fi
%s"$@" 2>&1
echo "` + ExitMarker + `$?"`

// RemoteArgv is the argv that runs c on a workspace host with a POSIX sh
// (a container): `sh -c <script> sh args…`, run in c.Dir. No file is
// written there; jest/vitest JSON comes back in the output
// (ReportOnStdout).
func RemoteArgv(c Command) []string {
	prefix := ""
	if len(c.Env) > 0 {
		prefix = "env " + envWords(c.Env) + " "
	}
	argv := []string{"sh", "-c", strings.Replace(remoteScript, "%s", prefix, 1), "sh"}
	return append(argv, c.Args...)
}

// envWords quotes KEY=VALUE pairs for the wrapper script. The values are
// termocode's own (CI=true, NO_COLOR=1 …), never the user's input, but
// they are quoted all the same.
func envWords(env []string) string {
	parts := make([]string, 0, len(env))
	for _, kv := range env {
		parts = append(parts, "'"+strings.ReplaceAll(kv, "'", `'\''`)+"'")
	}
	return strings.Join(parts, " ")
}

// ParseExitMarker reads the exit code from the marker line.
func ParseExitMarker(line string) (int, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), ExitMarker)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return n, true
}

// ExtractJSONReport finds the jest/vitest JSON report inside a run's
// output (ReportOnStdout): the first JSON object that starts a line and
// has "testResults". nil when there is none.
func ExtractJSONReport(out []byte) []byte {
	for start := 0; start < len(out); {
		i := bytes.IndexByte(out[start:], '{')
		if i < 0 {
			return nil
		}
		i += start
		if i == 0 || out[i-1] == '\n' {
			dec := json.NewDecoder(bytes.NewReader(out[i:]))
			var raw json.RawMessage
			if dec.Decode(&raw) == nil && bytes.Contains(raw, []byte(`"testResults"`)) {
				return raw
			}
		}
		start = i + 1
	}
	return nil
}
