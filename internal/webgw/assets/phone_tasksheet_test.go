package assets

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The phone's task action sheet (MOBUX-8, mobile plan 2.4): a task of a
// project opens one sheet with Talk about it · Run a workflow · Many
// agents · Many models · Just start, Cancel first and focused. Before
// anything starts, the sheet shows a rough cost and the lane count. A
// step the laptop must confirm (a race, a real workflow run) goes through
// AskLaptop (Cancel first, armed after 600 ms); a viewer gets only Talk
// about it. The sheet is phone code: it comes with the phone's lazy
// chunk, never app.js.

func readTaskSheetSrc(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// taskActionRe reads one row of TASK_ACTIONS in src/state/taskactions.ts:
//
//	{ id: "talk", kind: "ask", label: "ph.task.talk", viewer: true },
var taskActionRe = regexp.MustCompile(`(?m)^\s*\{\s*id:\s*"([a-z]+)",\s*kind:\s*"([a-z]+)",\s*label:\s*"([a-z0-9_.]+)"(,\s*viewer:\s*true)?\s*\},?\s*$`)

func TestTaskSheetActionsInOrder(t *testing.T) {
	src := readTaskSheetSrc(t, "src/state/taskactions.ts")
	rows := taskActionRe.FindAllStringSubmatch(src, -1)
	var ids []string
	keys := map[string]bool{}
	for _, e := range parseStrings(t, "src/strings.ts", "S") {
		keys[e.key] = true
	}
	kinds := map[string]string{"talk": "ask", "workflow": "card", "agents": "card", "models": "ask", "start": "op"}
	for _, r := range rows {
		id, kind, label, viewer := r[1], r[2], r[3], r[4] != ""
		ids = append(ids, id)
		if kinds[id] != kind {
			t.Errorf("%s: kind %q, want %q", id, kind, kinds[id])
		}
		if !keys[label] {
			t.Errorf("%s: label %q is not in strings.ts", id, label)
		}
		if viewer != (id == "talk") {
			t.Errorf("%s: viewer = %v; a viewer sees only Talk about it", id, viewer)
		}
	}
	if got := strings.Join(ids, ","); got != "talk,workflow,agents,models,start" {
		t.Fatalf("TASK_ACTIONS = %q", got)
	}
	for _, c := range []string{"RACE_MIN_LANES = 2", "RACE_MAX_LANES = 4"} {
		if !strings.Contains(src, c) {
			t.Errorf("taskactions.ts has no %s (the race limits of ai.race_start)", c)
		}
	}
	// The estimate is pure and reads only finished runs with a cost.
	for _, fn := range []string{"export function typicalRunUSD(", "export function estimate(", "export function costLine(", "export function taskRef("} {
		if !strings.Contains(src, fn) {
			t.Errorf("taskactions.ts has no %s", fn)
		}
	}
	if strings.Contains(src, "document.") || strings.Contains(src, "link.") {
		t.Error("taskactions.ts must stay pure (no DOM, no network)")
	}
}

func TestTaskSheetSafeAndConfirmed(t *testing.T) {
	src := readTaskSheetSrc(t, "src/phone/TaskSheet.tsx")
	if !regexp.MustCompile(`export function TaskSheet\b`).MatchString(src) {
		t.Fatal("TaskSheet.tsx does not export function TaskSheet")
	}
	if !regexp.MustCompile(`\bTaskSheet\b`).MatchString(readTaskSheetSrc(t, "src/phone/index.ts")) {
		t.Error("src/phone/index.ts does not export TaskSheet")
	}
	// Cancel is the safe action of every step.
	if n := strings.Count(src, `safe: true`); n < 2 {
		t.Errorf("TaskSheet has %d safe actions; Cancel / Back is safe on the menu and on each step", n)
	}
	// Card ops go through AskLaptop; the dry run and Just start do not.
	for _, op := range []string{`"ai.race_start"`, `"workflows.run"`} {
		i := strings.Index(src, op)
		if i < 0 {
			t.Errorf("TaskSheet does not send %s", op)
			continue
		}
		// The op is inside an <AskLaptop … /> element: after its start
		// and before the element closes.
		j := strings.LastIndex(src[:i], "<AskLaptop")
		if j < 0 || strings.Contains(src[j:i], "/>") {
			t.Errorf("%s is not sent from an AskLaptop", op)
		}
	}
	for _, op := range []string{`"workflows.dry_run"`, `"chat.send"`} {
		if !strings.Contains(src, op) {
			t.Errorf("TaskSheet does not send %s", op)
		}
	}
	// The cost and lane line shows on each step that starts work.
	if n := strings.Count(src, `class="ph-task-cost"`); n < 1 {
		t.Error("TaskSheet shows no rough cost line (class ph-task-cost)")
	}
	if !strings.Contains(src, "costLine(") {
		t.Error("TaskSheet does not use costLine")
	}
	// Never a file's contents or a secret: only the task's reference.
	if strings.Contains(src, "files.read") || strings.Contains(src, "secrets") {
		t.Error("TaskSheet reads file contents or secrets")
	}
}

func TestTaskSheetTargets(t *testing.T) {
	css := readPhoneCSS(t)
	for _, cls := range []string{".ph-lane-step", ".ph-pick"} {
		body := ruleBody(css, cls)
		if body == "" {
			t.Errorf("phone.css has no %s { … } rule", cls)
			continue
		}
		if !strings.Contains(body, "min-height: var(--p-tap)") {
			t.Errorf("%s has no min-height: var(--p-tap) (44 px target)", cls)
		}
	}
	if ruleBody(css, ".ph-task-cost") == "" {
		t.Error("phone.css has no .ph-task-cost rule")
	}
}
