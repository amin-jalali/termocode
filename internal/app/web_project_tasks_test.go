//go:build unix

package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/webproto"
)

const ptasksIssueText = `[{"number":41,"title":"Crash on start","author":{"login":"ann"},"url":"https://github.com/o/r/issues/41","state":"OPEN",
 "labels":[{"name":"bug"},{"name":"a"},{"name":"b"},{"name":"c"},{"name":"d"},{"name":"e"},{"name":"f"}],"assignees":[],"updatedAt":"2026-11-06T10:00:00Z"}]`

const ptasksChecklist = "# Phase 1\n- [ ] **MOB-52** project tasks\n- [x] done item\n  - [ ] nested\n"

// webTestPTasks is a web hub on a git workspace with a TASKS.md whose gh
// is the fake.
func webTestPTasks(t *testing.T, f *prFakeGH) (*Model, *webConn, string, func(args ...string) string) {
	t.Helper()
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir, run := webTestRepo(t, &m)
	webTestWrite(t, dir, "TASKS.md", ptasksChecklist)
	m.ciW().run = f.run
	return &m, webTestSub(t, &m), dir, run
}

func ptasksSection(t *testing.T, r webproto.ProjectTasks, kind string) webproto.ProjectTaskSection {
	t.Helper()
	for _, s := range r.Sections {
		if s.Kind == kind {
			return s
		}
	}
	t.Fatalf("no %s section in %+v", kind, r)
	return webproto.ProjectTaskSection{}
}

// Spec (MOB-52): project.tasks answers the workspace's checklist (open
// items; done ones only on request), its open issues and its open pull
// requests in one read, each with an id the phone can attach.
func TestProjectTasks(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"issue list": ptasksIssueText, "pr list": prListText}}
	m, c, _, _ := webTestPTasks(t, f)

	var r webproto.ProjectTasks
	webTestData(t, webTestCallBg(t, m, c, webproto.OpProjectTasks, `{}`), &r)
	if len(r.Sections) != 3 || r.Sections[0].Kind != "checklist" {
		t.Fatalf("sections %+v", r.Sections)
	}
	cl := r.Sections[0]
	if cl.Title != "TASKS.md" || cl.Total != 3 || cl.Open != 2 || len(cl.Items) != 2 {
		t.Fatalf("checklist %+v", cl)
	}
	if it := cl.Items[0]; it.ID != "TASKS.md:2" || it.Kind != "ptask" || it.State != "open" || it.Group != "Phase 1" || it.Line != 2 || it.Path != "TASKS.md" {
		t.Errorf("item %+v", it)
	}
	if cl.Items[1].Depth != 1 {
		t.Errorf("nested %+v", cl.Items[1])
	}

	is := ptasksSection(t, r, "issues")
	if len(is.Items) != 1 || is.Items[0].ID != "issue:41" || is.Items[0].Number != 41 || is.Items[0].Author != "ann" || len(is.Items[0].Labels) != webproto.MaxProjectTaskLabels {
		t.Errorf("issues %+v", is)
	}
	pr := ptasksSection(t, r, "prs")
	if len(pr.Items) != 2 || pr.Items[0].ID != "pr:14" || pr.Items[0].Checks != "pass" || pr.Items[1].State != "draft" || pr.Items[1].Checks != "fail" {
		t.Errorf("prs %+v", pr)
	}

	r = webproto.ProjectTasks{}
	webTestData(t, webTestCallBg(t, m, c, webproto.OpProjectTasks, `{"only":"checklist","done":true}`), &r)
	if len(r.Sections) != 1 || len(r.Sections[0].Items) != 3 || r.Sections[0].Items[1].State != "done" {
		t.Errorf("only checklist with done %+v", r.Sections)
	}
	if strings.Count(strings.Join(f.calls, "\n"), "issue list") != 1 {
		t.Error("only checklist still asked gh")
	}
}

// Spec (MOB-52): a source that fails says why in its own section; the
// others still answer. No gh is a plain note, never an error.
func TestProjectTasksNotes(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"pr list": prListText}, errs: map[string]error{"issue list": ghci.ErrNoGH}}
	m, c, _, _ := webTestPTasks(t, f)
	var r webproto.ProjectTasks
	webTestData(t, webTestCallBg(t, m, c, webproto.OpProjectTasks, `{}`), &r)
	if is := ptasksSection(t, r, "issues"); is.Note == "" || len(is.Items) != 0 {
		t.Errorf("issues %+v", is)
	}
	if pr := ptasksSection(t, r, "prs"); pr.Note != "" || len(pr.Items) != 2 {
		t.Errorf("prs %+v", pr)
	}
	r = webproto.ProjectTasks{}
	webTestData(t, webTestCallBg(t, m, c, webproto.OpProjectTasks, `{"only":"mrs"}`), &r)
	if s := ptasksSection(t, r, "mrs"); s.Note == "" {
		t.Errorf("mrs on a GitHub repo %+v", s)
	}
}

// Spec (MOB-52): on a GitLab remote the merge requests come with the
// token of the laptop's secret store; the token is never in the answer.
// No token is a note that says to set it on the laptop.
func TestProjectTasksGitLab(t *testing.T) {
	m, c, _, run := webTestPTasks(t, &prFakeGH{})
	run("remote", "add", "origin", "git@gitlab.example.com:g/p.git")
	f := &gitlabFake{}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	old := gitlabAPI
	gitlabAPI = func(string) string { return srv.URL }
	t.Cleanup(func() { gitlabAPI = old })
	s, err := m.secretStore()
	if err != nil || s == nil {
		t.Skip("no secret store:", err)
	}
	name := gitlabSecretName("gitlab.example.com")
	_ = s.Delete(name)
	t.Cleanup(func() { _ = s.Delete(name) })

	var r webproto.ProjectTasks
	webTestData(t, webTestCallBg(t, m, c, webproto.OpProjectTasks, `{"only":"mrs"}`), &r)
	if mr := ptasksSection(t, r, "mrs"); !strings.Contains(mr.Note, "on the laptop") || len(f.calls) != 0 {
		t.Errorf("no token %+v calls %v", mr, f.calls)
	}

	if err := s.Set(name, gitlabTestToken); err != nil {
		t.Fatal(err)
	}
	r = webproto.ProjectTasks{}
	reply := webTestCallBg(t, m, c, webproto.OpProjectTasks, `{}`)
	if raw, _ := json.Marshal(reply); strings.Contains(string(raw), gitlabTestToken) {
		t.Fatal("the token reached the phone")
	}
	webTestData(t, reply, &r)
	mr := ptasksSection(t, r, "mrs")
	if len(mr.Items) != 1 || mr.Items[0].ID != "mr:12" || mr.Items[0].State != "draft" || mr.Items[0].Author != "ann" || mr.Items[0].Branch != "login" {
		t.Errorf("mrs %+v", mr)
	}
	for _, sec := range r.Sections {
		if sec.Kind == "issues" || sec.Kind == "prs" {
			t.Errorf("a GitLab repo asked gh: %+v", sec)
		}
	}
	if len(f.tokens) != 1 || f.tokens[0] != gitlabTestToken {
		t.Errorf("tokens sent %d", len(f.tokens))
	}
}

// Spec (MOB-52): a workspace that is not a git repository still lists its
// checklist; asking only for issues says why there are none.
func TestProjectTasksNoRepo(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	dir := t.TempDir()
	webTestWrite(t, dir, "TODO.md", "- [ ] one\n")
	m.ws.Root = dir
	c := webTestSub(t, &m)
	var r webproto.ProjectTasks
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpProjectTasks, `{}`), &r)
	if len(r.Sections) != 1 || r.Sections[0].Title != "TODO.md" || len(r.Sections[0].Items) != 1 {
		t.Errorf("sections %+v", r.Sections)
	}
	r = webproto.ProjectTasks{}
	webTestData(t, webTestCallBg(t, &m, c, webproto.OpProjectTasks, `{"only":"issues"}`), &r)
	if len(r.Sections) != 1 || r.Sections[0].Note == "" {
		t.Errorf("issues on no repo %+v", r.Sections)
	}
}
