//go:build browser

package web

import "github.com/amin-jalali/termocode/internal/webproto"

// The Run, Test and Debug views in TestShots (WEBP-4): their topics'
// data, the answers of their reads, and the shots to take.

func runShotSnaps() map[string]any {
	return map[string]any{
		"tests":    testsTree("idle", 1),
		"debug":    debugPaused(),
		"services": servicesSnap(),
	}
}

func runShotAnswer(op string) (any, bool) {
	if d, ok := kubeAnswer(&webproto.Req{Op: op, Args: []byte(`{"context":"dev"}`)}); ok {
		return d, true
	}
	switch op {
	case webproto.OpDebugSources:
		return webproto.DebugSources{Sources: []webproto.DebugSource{{Name: "main.go", Path: "cmd/covreport/main.go"}, {Name: "proc.go", Outside: true}}}, true
	case webproto.OpTestsOutput:
		return webproto.TestOutput{Lines: []string{"=== RUN   TestA", "--- PASS: TestA (0.01s)", "=== RUN   TestB", "    a_test.go:11: want 2, got 3", "--- FAIL: TestB (0.00s)", "FAIL", "FAIL	example.com/m/pkg	0.012s"}}, true
	}
	return nil, false
}

type runShot struct {
	name, hash, ready string
}

func runShots(vp Viewport) []runShot {
	out := []runShot{{name: "testing", hash: "#/more/testing", ready: `document.querySelectorAll(".tx-row").length === 4`}}
	out = append(out, runShot{name: "kube", hash: "#/more/kube", ready: `(document.querySelector('.kb-row[data-kube="pod/api-7d9f-x2"]')?.click(), document.querySelectorAll(".kb-text .trow").length === 2)`})
	out = append(out, runShot{name: "services", hash: "#/more/services", ready: `document.querySelectorAll(".svc").length === 3`})
	out = append(out, runShot{name: "debug", hash: "#/more/debug", ready: `document.querySelectorAll(".dv-frame").length === 2`})
	if !vp.Mobile {
		out = append(out, runShot{name: "debug-console", hash: "#/more/debug", ready: `(document.querySelector('[data-act="console"]')?.click(), document.querySelectorAll(".dcon-row").length === 4)`})
		out = append(out, runShot{name: "test-results", hash: "#/more/testing", ready: `(document.querySelector(".tx-sum .tx-link")?.click(), document.querySelectorAll(".tres-rows .trow").length === 7)`})
	}
	return out
}

// debugPaused is a session paused at a breakpoint in filter.go.
func debugPaused() webproto.DebugSnap {
	return webproto.DebugSnap{Available: true, Active: true, Stopped: true, Name: "Debug covreport", Status: "Paused on breakpoint", Thread: 1, Frame: 1001,
		Threads: []webproto.DebugThread{{ID: 1, Name: "main", Stopped: true}},
		Frames: []webproto.DebugFrame{{ID: 1001, Name: "main.shortenCoverLine", Path: "cmd/covreport/filter.go", Line: 13, Col: 2},
			{ID: 1002, Name: "runtime.main", File: "proc.go", Outside: true, Line: 283}},
		Scopes: []webproto.DebugScope{{Key: "s:Locals", Name: "Locals", Loaded: true, Vars: []webproto.DebugVar{
			{Name: "line", Value: `"ok  pkg 0.4s coverage: 81% of statements in a,b"`, Type: "string"},
			{Name: "marker", Value: `"of statements in "`, Type: "string"},
			{Name: "lf", Value: "*main.lineFilter {w: ..., f: ..., buf: []uint8 len: 0}", Type: "*main.lineFilter", Ref: 7},
			{Name: "i", Value: "-1", Type: "int"}}},
			{Key: "s:Globals", Name: "Globals", Expensive: true, Vars: []webproto.DebugVar{}}},
		Children:    []webproto.DebugChildren{},
		Open:        []string{},
		Watches:     []webproto.DebugWatch{{Expr: "len(line)", Value: "48"}, {Expr: "tail", Value: "undefined: tail", Error: true}},
		Breakpoints: []webproto.DebugBreakpoint{{Path: "cmd/covreport/filter.go", Line: 13, Text: "i := strings.Index(line, marker)", Verified: "yes"}, {Path: "cmd/covreport/filter.go", Line: 18, Condition: "i > 3"}},
		Configs:     []webproto.DebugConfig{{Name: "Debug covreport", Type: "delve", Debug: true, Source: "launch.json"}, {Name: "Serve docs", Type: "shell", Source: "launch.json"}},
		Selected:    "Debug covreport",
		Console: []webproto.DebugLine{{Cat: "console", Text: "Starting Debug covreport…"}, {Cat: "stdout", Text: "reading coverage.out"},
			{Cat: "input", Text: "› len(line)"}, {Cat: "result", Text: "48"}},
	}
}

// servicesSnap is a compose project with db (healthy) and api (exited
// 1), and a local command web (running).
func servicesSnap() webproto.ServicesSnap {
	return webproto.ServicesSnap{State: "ready", Project: "shop", ComposeFiles: []string{"compose.yaml"}, Services: []webproto.ServiceItem{
		{Name: "db", Kind: "compose", State: "healthy", Image: "postgres:17", Source: "compose.yaml:3", Status: "Up 2 hours (healthy)", Ports: []webproto.ServicePort{{Published: 5432, Target: 5432, Proto: "tcp", Open: "yes"}}},
		{Name: "api", Kind: "compose", State: "exited", Exit: 1, Source: "compose.yaml:9", Status: "Exited (1) 3 minutes ago", Ports: []webproto.ServicePort{}},
		{Name: "web", Kind: "local", State: "running", Command: "npm run dev", Source: "Procfile", Ports: []webproto.ServicePort{{Published: 3000, Target: 3000}}},
	}}
}
