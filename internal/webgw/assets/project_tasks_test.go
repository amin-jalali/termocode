package assets

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Phone project tasks view (MOB-52): one project.tasks read, a chip per
// kind the op knows, status as colour + shape (StatusMark through Row),
// links only through the confirm sheet, English words only, and kept in
// the phone's files (never app.js; TestPhoneKitNotInPageBundle).
func TestPhoneProjectTasksView(t *testing.T) {
	b, err := os.ReadFile("src/phone/ProjectTasks.tsx")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	// requestWithID: the project page (MOBUX-7) sends it to that project's hub.
	if !regexp.MustCompile(`\.request(WithID)?\("project\.tasks"`).MatchString(src) {
		t.Error("the view does not read project.tasks")
	}
	for _, only := range []string{`"checklist"`, `"issues"`, `"prs"`, `"mrs"`} {
		if !strings.Contains(src, "only: "+only) {
			t.Errorf("no chip for %s", only)
		}
	}
	for _, s := range []string{`"done"`, `"fail"`, `"pending"`, `"pass"`} {
		if !strings.Contains(src, s) {
			t.Errorf("taskStatus does not handle %s", s)
		}
	}
	if !strings.Contains(src, "askLink(") || strings.Contains(src, "window.open") || strings.Contains(src, "href=") {
		t.Error("a task link must open through askLink (the confirm sheet) only")
	}
	if strings.Contains(src, "innerHTML") || strings.Contains(src, "dangerouslySetInnerHTML") {
		t.Error("the view writes HTML")
	}
	if regexp.MustCompile(`[\x{0600}-\x{06FF}]`).MatchString(src) || strings.Contains(src, `t("`) {
		t.Error("the phone app is English only: its words live in the view, not the translated strings")
	}
	idx, err := os.ReadFile("src/phone/index.ts")
	if err != nil || !strings.Contains(string(idx), "ProjectTasksView") {
		t.Error("src/phone/index.ts does not export ProjectTasksView")
	}
	css := readPhoneCSS(t)
	for _, sel := range []string{".ph-row-link", ".ph-note", ".ph-task"} {
		if !strings.Contains(css, sel) {
			t.Errorf("phone.css has no %s", sel)
		}
	}
	if body := ruleBody(css, ".ph-row-link,\n.ph-retry"); !strings.Contains(body, "min-height: var(--p-tap)") || !strings.Contains(body, "min-width: var(--p-tap)") {
		t.Error("the link and retry buttons are not 44 px targets")
	}
}
