package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/picker"
)

// The fake token is split so no scanner sees a real-looking one.
var gitlabTestToken = "glpat-" + "apptest0123456789"

// gitlabFake is a GitLab with one project (g/p); it records the calls.
type gitlabFake struct {
	mu     sync.Mutex
	calls  []string
	tokens []string
	posted map[string]any
}

func (f *gitlabFake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.EscapedPath())
	f.tokens = append(f.tokens, r.Header.Get("PRIVATE-TOKEN"))
	if r.URL.EscapedPath() != "/api/v4/projects/g%2Fp/merge_requests" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.Method == http.MethodPost {
		_ = json.NewDecoder(r.Body).Decode(&f.posted)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"iid":13,"title":"Add login","source_branch":"login","target_branch":"main","web_url":"https://gitlab.example.com/g/p/-/merge_requests/13"}`)
		return
	}
	_, _ = io.WriteString(w, `[{"iid":12,"title":"Add login","author":{"username":"ann"},"source_branch":"login","target_branch":"main","web_url":"https://gitlab.example.com/g/p/-/merge_requests/12","draft":true}]`)
}

// gitlabRepo is a repo whose origin is on gitlab.example.com, served by
// a fake GitLab; the saved token is dropped after the test.
func gitlabRepo(t *testing.T) (*Model, string, *gitlabFake) {
	t.Helper()
	m, dir := covGitRepo(t)
	covGit(t, dir, "remote", "add", "origin", "git@gitlab.example.com:g/p.git")
	f := &gitlabFake{}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	old := gitlabAPI
	gitlabAPI = func(host string) string {
		if host != "gitlab.example.com" {
			t.Errorf("api host %q", host)
		}
		return srv.URL
	}
	t.Cleanup(func() { gitlabAPI = old })
	t.Cleanup(func() {
		if s, err := m.secretStore(); err == nil && s != nil {
			_ = s.Delete(gitlabSecretName("gitlab.example.com"))
		}
	})
	return m, dir, f
}

// GITX-39: Git: GitLab Merge Requests… asks the token once (masked; it is
// saved only in the secret store), lists the open merge requests and
// opens the picked one in the browser.
func TestGitxGitLabListMRs(t *testing.T) {
	m, _, f := gitlabRepo(t)
	var opened []string
	oldOpen := osOpen
	osOpen = func(u string) error { opened = append(opened, u); return nil }
	t.Cleanup(func() { osOpen = oldOpen })

	m.dispatchPaletteAction("git-gitlab-mrs")
	if !m.promptOpen || !m.prompt.Masked() {
		t.Fatalf("no masked token prompt (open %v)", m.promptOpen)
	}
	m.promptOpen = false
	covMsg[gitxDoneMsg](t, m.handlePromptSubmit(gitlabTestToken)).apply(m)
	if m.prompt.Value() == gitlabTestToken {
		t.Error("the typed token stays in the prompt")
	}
	s, err := m.secretStore()
	if err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := s.Get(gitlabSecretName("gitlab.example.com")); !ok || v != gitlabTestToken {
		t.Fatal("the token is not in the secret store")
	}
	if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "mr-12" {
		t.Fatalf("picker %v", gitxPickerIDs(m))
	}
	if it := m.gitx.items[0]; !strings.Contains(it.Title, "!12") || !strings.Contains(it.Title, "Add login") || !strings.Contains(it.Hint, "login → main") || !strings.Contains(it.Hint, "draft") {
		t.Errorf("row %+v", it)
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "mr-12"})
	if len(opened) != 1 || opened[0] != "https://gitlab.example.com/g/p/-/merge_requests/12" {
		t.Fatalf("opened %v", opened)
	}
	// The saved token is used: no prompt the second time.
	cmd := m.dispatchPaletteAction("git-gitlab-mrs")
	if m.promptOpen {
		t.Fatal("asked the token again")
	}
	covMsg[gitxDoneMsg](t, cmd).apply(m)
	if len(f.tokens) != 2 || f.tokens[1] != gitlabTestToken {
		t.Errorf("tokens sent %d", len(f.tokens))
	}
	if p := aiActionPolicies["git-gitlab-mrs"]; p.Gate != aiGateAuto {
		t.Error("listing merge requests is not read only for the AI")
	}
	if p := aiActionPolicies["git-gitlab-token"]; p.Gate != aiGateNever {
		t.Error("the AI may set the GitLab token")
	}
}

// GITX-39: without a GitLab remote the commands only say so.
func TestGitxGitLabNoRemote(t *testing.T) {
	m, _ := covGitRepo(t)
	m.dispatchPaletteAction("git-gitlab-mrs")
	if m.promptOpen || !toastHas(m, "no GitLab remote") {
		t.Fatal("no note without a GitLab remote")
	}
}

// GITX-39: Git: Create GitLab Merge Request… asks the title (the last
// commit subject first), then a Cancel-first card, then creates the merge
// request from the current branch into the default branch.
func TestGitxGitLabCreateMR(t *testing.T) {
	m, dir, f := gitlabRepo(t)
	s, _ := m.secretStore()
	if err := s.Set(gitlabSecretName("gitlab.example.com"), gitlabTestToken); err != nil {
		t.Fatal(err)
	}
	m.dispatchPaletteAction("git-gitlab-mr-create")
	if m.promptOpen || !toastHas(m, "Switch to the branch") {
		t.Fatal("offered a merge request from the default branch")
	}
	covGit(t, dir, "checkout", "-q", "-b", "login")
	covWrite(t, dir, "a.txt", "login\n")
	covCommit(t, dir, "Add login")
	covRefresh(m)
	m.dispatchPaletteAction("git-gitlab-mr-create")
	if !m.promptOpen || m.prompt.Value() != "Add login" {
		t.Fatalf("title prompt %v %q", m.promptOpen, m.prompt.Value())
	}
	m.promptOpen = false
	m.handlePromptSubmit("Add login")
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,create,draft" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card %q", gitxCardIDList(m))
	}
	m.confirmOpen = false
	if m.handleConfirmSelect("cancel") != nil || len(f.calls) != 0 {
		t.Fatal("Cancel created a merge request")
	}
	m.dispatchPaletteAction("git-gitlab-mr-create")
	m.promptOpen = false
	m.handlePromptSubmit("Add login")
	m.confirmOpen = false
	covMsg[gitxDoneMsg](t, m.handleConfirmSelect("draft")).apply(m)
	if len(f.calls) != 1 || f.calls[0] != "POST /api/v4/projects/g%2Fp/merge_requests" {
		t.Fatalf("calls %v", f.calls)
	}
	if f.posted["source_branch"] != "login" || f.posted["target_branch"] != "main" || f.posted["title"] != "Draft: Add login" {
		t.Errorf("posted %v", f.posted)
	}
	if !toastHas(m, "https://gitlab.example.com/g/p/-/merge_requests/13") {
		t.Error("no link to the new merge request")
	}
	if p := aiActionPolicies["git-gitlab-mr-create"]; p.Gate != aiGateNever {
		t.Error("the AI may create a merge request")
	}
}
