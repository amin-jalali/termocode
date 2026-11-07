package containers

import (
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
	"github.com/amin-jalali/termocode/internal/services"
)

// Run lens (CTX-37): a "▸ build" mark on each FROM line of a Dockerfile
// and a "▸ up" mark on each service of a compose file. A click on the
// mark (or the palette command with the cursor in that block) runs it
// after a card.

// RunTarget is one runnable spot in a Dockerfile or compose file.
type RunTarget struct {
	Line    int    // 1-based line of the mark
	Kind    string // "build" (Dockerfile stage) or "service" (compose)
	Stage   string // Dockerfile: the stage name after AS ("" = unnamed)
	Last    bool   // Dockerfile: the last stage (a plain build)
	Service string // compose: the service name
}

// RunTarget kinds.
const (
	RunBuild   = "build"
	RunService = "service"
)

var (
	fromRe       = lazyre.New(`(?i)^\s*FROM\s+(?:--\S+\s+)*\S+(?:\s+AS\s+([A-Za-z0-9._-]+))?\s*$`)
	composeKeyRe = lazyre.New(`^(\s+)([A-Za-z0-9._-]+):\s*(?:#.*)?$`)
)

// IsComposeFileName reports a compose file by its name: compose.yaml,
// docker-compose.yml and their ".<x>." variants (compose.dev.yaml).
func IsComposeFileName(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	for _, n := range services.ComposeFileNames {
		if base == n {
			return true
		}
	}
	for _, pre := range []string{"compose.", "docker-compose."} {
		if strings.HasPrefix(base, pre) && (strings.HasSuffix(base, ".yaml") || strings.HasSuffix(base, ".yml")) {
			return true
		}
	}
	return false
}

// RunTargets finds the marks of a Dockerfile or compose file; nil for
// any other file.
func RunTargets(path string, lines []string) []RunTarget {
	switch {
	case IsDockerfileName(path):
		return dockerfileTargets(lines)
	case IsComposeFileName(path):
		return composeTargets(lines)
	}
	return nil
}

func dockerfileTargets(lines []string) []RunTarget {
	var out []RunTarget
	for i, l := range lines {
		if m := fromRe.Regexp().FindStringSubmatch(l); m != nil {
			out = append(out, RunTarget{Line: i + 1, Kind: RunBuild, Stage: m[1]})
		}
	}
	if len(out) > 0 {
		out[len(out)-1].Last = true
	}
	return out
}

// composeTargets are the keys right under the top-level services: key.
func composeTargets(lines []string) []RunTarget {
	var out []RunTarget
	in, indent := false, ""
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if l[0] != ' ' && l[0] != '\t' { // a top-level key
			in = strings.HasPrefix(t, "services:") && strings.TrimSpace(strings.TrimPrefix(t, "services:")) == ""
			indent = ""
			continue
		}
		if !in {
			continue
		}
		m := composeKeyRe.Regexp().FindStringSubmatch(l)
		if indent == "" {
			if m == nil {
				continue
			}
			indent = m[1]
		}
		if m != nil && m[1] == indent {
			out = append(out, RunTarget{Line: i + 1, Kind: RunService, Service: m[2]})
		}
	}
	return out
}

// RunTargetAt is the target whose block holds line: the last mark at or
// above it. false when line is above the first mark.
func RunTargetAt(ts []RunTarget, line int) (RunTarget, bool) {
	var got RunTarget
	ok := false
	for _, t := range ts {
		if t.Line > line {
			break
		}
		got, ok = t, true
	}
	return got, ok
}

// RunTargetOn is the target marked exactly on line.
func RunTargetOn(ts []RunTarget, line int) (RunTarget, bool) {
	for _, t := range ts {
		if t.Line == line {
			return t, true
		}
	}
	return RunTarget{}, false
}

// RunTargetCommand is the job of one target of file: a build of the stage
// (tag defaultTag, --target for a named stage that is not the last) or
// `compose -f file up <service>`.
func (e *Engine) RunTargetCommand(file string, t RunTarget, defaultTag string) services.Command {
	if t.Kind == RunService {
		c := e.ComposeWithFiles("", filepath.Dir(file), []string{file}, "up", t.Service)
		c.Label = "up: " + t.Service
		return c
	}
	o := BuildOptions{Dockerfile: file, Tag: defaultTag}
	if t.Stage != "" && !t.Last {
		o.Target = t.Stage
		if name, _, ok := strings.Cut(defaultTag, ":"); ok {
			o.Tag = name + ":" + strings.ToLower(t.Stage)
		}
	}
	return e.BuildCommand(o)
}
