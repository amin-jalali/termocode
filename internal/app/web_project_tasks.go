package app

import (
	"context"
	"errors"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/gitlab"
	"github.com/amin-jalali/termocode/internal/projtasks"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Project tasks on the phone (MOB-52). project.tasks reads, off the UI
// thread, the workspace's checklists (TASKS.md, TODO.md; projtasks), its
// open GitHub issues and pull requests (the laptop's gh, through the CI
// watcher's runner) and its open GitLab merge requests. The GitLab
// token is read only from an open secret store and never leaves the
// laptop; a locked store or no token is a note ("set it on the laptop"),
// never a prompt from the phone. Each source that fails says why in its
// section and the others still answer. The door (MOB-50) routes the call
// to the project's hub, so it always reads this hub's own workspace.

func init() {
	webOps[webproto.OpProjectTasks] = (*Model).webProjectTasks
}

// webProjectTasksTimeout bounds the whole read (gh and GitLab).
const webProjectTasksTimeout = 30 * time.Second

// webPTasksIn is what the read needs from the model, taken on the UI
// thread.
type webPTasksIn struct {
	root    string // the workspace folder (checklists)
	repo    string // the git root ("" not a repository, or git not read yet)
	only    string
	done    bool
	run     ghci.Runner
	store   secrets.Store // open store, or nil
	storeNo string        // why there is no open store
}

func (m *Model) webProjectTasks(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ProjectTasksArgs)
	root, err := m.rootDir()
	if err != nil {
		call.fail(webproto.CodeConflict, "No workspace is open on the laptop.")
		return nil
	}
	in := webPTasksIn{root: root, only: a.Only, done: a.Done}
	if m.gitLoaded && m.gitIsRepo {
		if r, err := m.gitRootDir(); err == nil {
			in.repo = r
		}
	}
	if in.repo != "" && (a.Only == "" || a.Only == "issues" || a.Only == "prs") {
		in.run = m.ciW().runner()
	}
	if in.repo != "" && (a.Only == "" || a.Only == "mrs") {
		switch s, err := m.secretStore(); {
		case err != nil || s == nil:
			in.storeNo = "The laptop has no secret store for a GitLab token."
		case s.Locked():
			in.storeNo = "Unlock the secret store on the laptop to read merge requests."
		default:
			in.store = s
		}
	}
	call.later()
	ctx := call.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, webProjectTasksTimeout)
		defer cancel()
		call.reply(webReadProjectTasks(ctx, in))
		return nil
	}
}

// webReadProjectTasks builds the answer; it runs off the UI thread.
func webReadProjectTasks(ctx context.Context, in webPTasksIn) webproto.ProjectTasks {
	out := webproto.ProjectTasks{Sections: []webproto.ProjectTaskSection{}}
	want := func(k string) bool { return in.only == "" || in.only == k }
	if want("checklist") {
		out.Sections = append(out.Sections, webChecklistSections(in.root, in.done)...)
	}
	if in.repo == "" {
		if in.only != "" && in.only != "checklist" {
			out.Sections = append(out.Sections, webproto.ProjectTaskSection{Kind: in.only, Title: webPTaskTitles[in.only], Items: []webproto.ProjectTask{},
				Note: "This workspace is not a git repository."})
		}
		return out
	}
	gl, isGitLab := gitlab.Project{}, false
	if base, err := git.RemoteWeb(in.repo); err == nil {
		gl, isGitLab = gitlab.FromWebBase(base)
	}
	if !isGitLab {
		if want("issues") && in.run != nil {
			out.Sections = append(out.Sections, webIssueSection(ctx, in.run, in.repo))
		}
		if want("prs") && in.run != nil {
			out.Sections = append(out.Sections, webPRSection(ctx, in.run, in.repo))
		}
		if in.only == "mrs" {
			out.Sections = append(out.Sections, webPTaskNote("mrs", "This repository has no GitLab remote."))
		}
		return out
	}
	if want("mrs") {
		out.Sections = append(out.Sections, webMRSection(ctx, in, gl))
	}
	if in.only == "issues" || in.only == "prs" {
		out.Sections = append(out.Sections, webPTaskNote(in.only, "This repository has no GitHub remote."))
	}
	return out
}

var webPTaskTitles = map[string]string{"checklist": "Checklist", "issues": "Issues", "prs": "Pull requests", "mrs": "Merge requests"}

func webPTaskNote(kind, note string) webproto.ProjectTaskSection {
	return webproto.ProjectTaskSection{Kind: kind, Title: webPTaskTitles[kind], Items: []webproto.ProjectTask{}, Note: note}
}

// webChecklistSections is one section per checklist file, in
// projtasks.Files order; none when the project has no such file.
func webChecklistSections(root string, done bool) []webproto.ProjectTaskSection {
	items, err := projtasks.Read(root)
	if errors.Is(err, projtasks.ErrNone) {
		return nil
	}
	var out []webproto.ProjectTaskSection
	at := map[string]int{}
	for _, it := range items {
		i, ok := at[it.File]
		if !ok {
			i = len(out)
			at[it.File] = i
			out = append(out, webproto.ProjectTaskSection{Kind: "checklist", Title: webName(it.File), Items: []webproto.ProjectTask{}})
		}
		s := &out[i]
		s.Total++
		if !it.Done {
			s.Open++
		}
		if (it.Done && !done) || len(s.Items) >= webproto.MaxProjectTaskItems {
			continue
		}
		state := "open"
		if it.Done {
			state = "done"
		}
		s.Items = append(s.Items, webproto.ProjectTask{ID: webName(it.ID()), Kind: "ptask", Title: webName(it.Text), State: state,
			Path: webName(it.File), Line: it.Line, Group: webName(it.Group), Depth: it.Depth})
	}
	if err != nil {
		note := "Could not read every checklist file: " + webName(ai.Redact(err.Error()))
		if len(out) == 0 {
			return []webproto.ProjectTaskSection{webPTaskNote("checklist", note)}
		}
		out[len(out)-1].Note = note
	}
	return out
}

// webGHNote says why gh gave nothing, as webGHFail does for one op.
func webGHNote(err error) string {
	switch {
	case errors.Is(err, ghci.ErrNoGitHub):
		return "This repository has no GitHub remote."
	case ghci.Quiet(err):
		return ciGHHint + " (on the laptop)"
	}
	return "gh said: " + webName(ai.Redact(err.Error()))
}

func webPTaskLabels(ls []string) []string {
	var out []string
	for _, l := range ls {
		if len(out) == webproto.MaxProjectTaskLabels {
			break
		}
		out = append(out, webName(l))
	}
	return out
}

func webIssueSection(ctx context.Context, run ghci.Runner, dir string) webproto.ProjectTaskSection {
	s := webproto.ProjectTaskSection{Kind: "issues", Title: webPTaskTitles["issues"], Items: []webproto.ProjectTask{}}
	issues, err := ghci.ListIssues(ctx, run, dir)
	if err != nil {
		s.Note = webGHNote(err)
		return s
	}
	s.Total, s.Open = len(issues), len(issues)
	for _, it := range issues {
		if len(s.Items) >= webproto.MaxProjectTaskItems {
			break
		}
		s.Items = append(s.Items, webproto.ProjectTask{ID: "issue:" + strconv.Itoa(it.Number), Kind: "issue", Title: webName(it.Title), State: "open",
			Number: it.Number, Author: webName(it.Author), Labels: webPTaskLabels(it.Labels), URL: webName(it.URL)})
	}
	return s
}

func webPRSection(ctx context.Context, run ghci.Runner, dir string) webproto.ProjectTaskSection {
	s := webproto.ProjectTaskSection{Kind: "prs", Title: webPTaskTitles["prs"], Items: []webproto.ProjectTask{}}
	prs, err := ghci.ListPRs(ctx, run, dir)
	if err != nil {
		s.Note = webGHNote(err)
		return s
	}
	s.Total, s.Open = len(prs), len(prs)
	for _, p := range prs {
		if len(s.Items) >= webproto.MaxProjectTaskItems {
			break
		}
		state := "open"
		if p.Draft {
			state = "draft"
		}
		s.Items = append(s.Items, webproto.ProjectTask{ID: "pr:" + strconv.Itoa(p.Number), Kind: "pr", Title: webName(p.Title), State: state,
			Number: p.Number, Author: webName(p.Author), Checks: webCIStates[p.ChecksState()], Branch: webName(p.Branch), URL: webName(p.URL)})
	}
	return s
}

// webMRSection reads the GitLab merge requests with the token of the open
// store (a ${env:…} / ${cmd:…} reference is resolved here, on the laptop).
func webMRSection(ctx context.Context, in webPTasksIn, p gitlab.Project) webproto.ProjectTaskSection {
	s := webPTaskNote("mrs", "")
	if in.store == nil {
		s.Note = in.storeNo
		return s
	}
	stored, ok, err := in.store.Get(gitlabSecretName(p.Host))
	switch {
	case err != nil:
		s.Note = "Could not read the GitLab token on the laptop."
		return s
	case !ok || stored == "":
		s.Note = "Set a GitLab token on the laptop (Git: Set GitLab Token…)."
		return s
	}
	token := stored
	if ref, err := secrets.ParseRef(stored); err == nil {
		v, rerr := newRefResolver(in.repo).Resolve(ref)
		if rerr != nil {
			s.Note = "Could not read the GitLab token on the laptop."
			return s
		}
		token = v
	}
	ai.RegisterSecret(token)
	mrs, err := gitlab.New(gitlabAPI(p.Host), token).ListMRs(ctx, p.Path)
	if err != nil {
		s.Note = "GitLab said: " + webName(errLine(ai.Redact(err.Error())))
		if errors.Is(err, gitlab.ErrUnauthorized) {
			s.Note = "GitLab refused the token. Set it again on the laptop (Git: Set GitLab Token…)."
		}
		return s
	}
	s.Total, s.Open = len(mrs), len(mrs)
	for _, mr := range mrs {
		if len(s.Items) >= webproto.MaxProjectTaskItems {
			break
		}
		state := "open"
		if mr.Draft {
			state = "draft"
		}
		s.Items = append(s.Items, webproto.ProjectTask{ID: "mr:" + strconv.Itoa(mr.IID), Kind: "mr", Title: webName(mr.Title), State: state,
			Number: mr.IID, Author: webName(mr.Author), Branch: webName(mr.Source), URL: webName(mr.URL)})
	}
	return s
}
