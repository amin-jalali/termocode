package assets

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Projects list (MOBUX-6): one card per project with its name, its state
// as a colour and a shape (● ◐ ✓ ◌), the open task count and the CI
// result. Checked in the source until the phone e2e runs the list.

func phoneProjSrc(t *testing.T, f string) string {
	t.Helper()
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The state follows the needs-you-first rule: a waiting answer beats work,
// work beats a quiet open project, an asleep project comes last.
func TestPhoneProjectsStateAndOrder(t *testing.T) {
	src := phoneProjSrc(t, "src/phone/projects.ts")
	for _, want := range []string{
		`if (waiting > 0) return "needs";`,
		`if (working > 0) return "working";`,
		`if (state === "asleep" || state === "") return "asleep";`,
		`return "done";`,
		`const RANK: Partial<Record<Status, number>> = { needs: 0, working: 1, done: 2, asleep: 3 };`,
		`return out.sort(`,
		`a.name.localeCompare(b.name)`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("projects.ts lost %q", want)
		}
	}
	// CI is drawn as a status, so it is a colour and a shape too.
	if !strings.Contains(src, `CI_LOOK: Readonly<Record<CI, Status>> = { passed: "done", failed: "failed", running: "working" }`) {
		t.Error("projects.ts: CI results are not mapped onto status shapes")
	}
}

// The phone never shows the laptop's absolute folders; the laptop sends
// ~-relative paths and anything else is dropped.
func TestPhoneProjectsHideAbsolutePaths(t *testing.T) {
	src := phoneProjSrc(t, "src/phone/projects.ts")
	if !strings.Contains(src, `where: absolute(where) ? "" : where,`) {
		t.Error("projects.ts shows a where it did not check")
	}
	for _, want := range []string{`p.startsWith("/")`, `/^[A-Za-z]:[\\/]/.test(p)`, `p.startsWith("\\\\")`} {
		if !strings.Contains(src, want) {
			t.Errorf("absolute() lost %q", want)
		}
	}
	// A field the laptop does not send yet is not drawn: the count and CI are optional.
	for _, want := range []string{`tasks?: number;`, `ci?: CI;`, `if (typeof p.tasks === "number")`, `if (ci) card.ci = ci;`} {
		if !strings.Contains(src, want) {
			t.Errorf("projects.ts lost %q", want)
		}
	}
}

// Each card is one button (one 44 px target), its state a shape in its
// colour, the whole card said to a screen reader; the list reads the
// projects topic with its skeleton, refusal and empty states.
func TestPhoneProjectsCard(t *testing.T) {
	src := phoneProjSrc(t, "src/phone/ProjectsList.tsx")
	for _, want := range []string{
		`<TopicBody topic="projects"`,
		`empty={<p>{t("ph.proj.none")}</p>}`,
		`class={` + "`ph-proj st-${p.status}",
		`aria-label={label(p)}`,
		`onClick={() => onOpen(p)}`,
		`<StatusMark s={p.status}`,
		`<StatusMark s={CI_LOOK[p.ci]} hidden />`,
		`tn("ph.proj.tasks", p.tasks)`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("ProjectsList.tsx lost %q", want)
		}
	}
	// A lazy chunk may not bring a style sheet (cmd/webbuild), so the
	// list's styles are in app.css: main.tsx imports them.
	if !strings.Contains(phoneProjSrc(t, "src/main.tsx"), `import "./styles/phone-projects.css";`) {
		t.Error("main.tsx does not import the projects list's styles/phone-projects.css")
	}
	if n := strings.Count(src, "<button"); n != 1 {
		t.Errorf("a project card has %d buttons; want one target per card", n)
	}
	css := phoneProjSrc(t, "src/styles/phone-projects.css")
	body := regexp.MustCompile(`(?m)^\.ph-proj\s*\{([^}]*)\}`).FindStringSubmatch(css)
	if body == nil || !strings.Contains(body[1], "min-height: var(--p-tap)") {
		t.Error(".ph-proj has no min-height: var(--p-tap) (44 px target)")
	}
	if !strings.Contains(css, ".ph-proj:focus-visible") {
		t.Error(".ph-proj has no focus ring")
	}
	// Colours come from the phone tokens only, so dark and light both work.
	if m := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|rgba?\(`).FindString(css); m != "" {
		t.Errorf("phone-projects.css has a raw colour %q; use the phone tokens", m)
	}
}

// The list belongs to the phone's lazy chunk, never to app.js or the desktop.
func TestPhoneProjectsNotInPageBundle(t *testing.T) {
	for _, f := range []string{"src/main.tsx", "src/app.tsx", "src/workbench.tsx"} {
		s := phoneProjSrc(t, f)
		if strings.Contains(s, "ProjectsList") || f != "src/main.tsx" && strings.Contains(s, "phone-projects.css") {
			t.Errorf("%s imports the phone projects list", f)
		}
	}
}

// Every text of the list is a string key in English only (the phone app
// UI is English, PhoneOnly); the empty state says that adding a folder
// happens on the laptop.
func TestPhoneProjectsStrings(t *testing.T) {
	en := phoneProjSrc(t, "src/strings.ts")
	for _, k := range []string{"ph.switch_title", "ph.proj.none", "ph.proj.here", "ph.proj.waiting", "ph.proj.working", "ph.proj.tasks.one", "ph.proj.tasks.other", "ph.proj.ci_passed", "ph.proj.ci_failed", "ph.proj.ci_running"} {
		if !strings.Contains(en, `"`+k+`":`) {
			t.Errorf("strings.ts has no %s", k)
		}
	}
	m := regexp.MustCompile(`"ph\.proj\.none":\s*"([^"]*)"`).FindStringSubmatch(en)
	if m == nil || !strings.Contains(strings.ToLower(m[1]), "laptop") {
		t.Errorf("ph.proj.none should say a project is added on the laptop: %v", m)
	}
	if fa := phoneProjSrc(t, "src/strings.fa.ts"); strings.Contains(fa, `"ph.proj.`) {
		t.Error("strings.fa.ts has phone projects strings; the phone app is English only")
	}
}
