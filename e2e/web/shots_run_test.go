//go:build browser

package web

import "github.com/amin-jalali/termocode/internal/webproto"

// The Run, Test and Debug views in TestShots (WEBP-4): their topics'
// data, the answers of their reads, and the shots to take.

func runShotSnaps() map[string]any {
	return map[string]any{
		"tests": testsTree("idle", 1),
	}
}

func runShotAnswer(op string) (any, bool) {
	switch op {
	case webproto.OpTestsOutput:
		return webproto.TestOutput{Lines: []string{"=== RUN   TestA", "--- PASS: TestA (0.01s)", "=== RUN   TestB", "    a_test.go:11: want 2, got 3", "--- FAIL: TestB (0.00s)", "FAIL", "FAIL	example.com/m/pkg	0.012s"}}, true
	}
	return nil, false
}

type runShot struct {
	name, hash, ready, prep string
}

func runShots(vp Viewport) []runShot {
	out := []runShot{{name: "testing", hash: "#/more/testing", ready: `document.querySelectorAll(".tx-row").length === 4`}}
	if !vp.Mobile {
		out = append(out, runShot{name: "test-results", hash: "#/more/testing", prep: `document.querySelector(".tx-sum .tx-link").click()`, ready: `document.querySelectorAll(".tres-rows .trow").length === 7`})
	}
	return out
}
