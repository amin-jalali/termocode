package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/workflow"
)

// Poll limits.
const (
	pollLimit    = 50 // items per gh list
	mentionLimit = 30 // issues and pulls whose comments are read
)

// GitHub polls gh for the github triggers. It is safe for one goroutine
// at a time (the app's poll loop).
type GitHub struct {
	Run   ghci.Runner // ghci.DefaultRunner or a fake
	Dir   string      // the workspace (gh finds the repo from its remote)
	State *State
}

// ID is the state id of trigger i of workflow wf: a change of the
// trigger's fields gives a new id, so it starts again with a quiet
// first poll.
func ID(wf string, i int, t workflow.Trigger) string {
	labels := append([]string(nil), t.Labels...)
	sort.Strings(labels)
	return fmt.Sprintf("%s#%d|%s|%s|%s|%s|%s", wf, i, t.GitHub, t.Repo, strings.Join(labels, ","), t.Text, t.Branch)
}

type ghLabel struct {
	Name string `json:"name"`
}

type ghAuthor struct {
	Login string `json:"login"`
}

type ghComment struct {
	ID     string   `json:"id"`
	URL    string   `json:"url"`
	Body   string   `json:"body"`
	Author ghAuthor `json:"author"`
}

type ghItem struct {
	Number   int         `json:"number"`
	Title    string      `json:"title"`
	URL      string      `json:"url"`
	Body     string      `json:"body"`
	Author   ghAuthor    `json:"author"`
	Labels   []ghLabel   `json:"labels"`
	Head     string      `json:"headRefName"`
	Comments []ghComment `json:"comments"`
}

type ghRun struct {
	ID         int64  `json:"databaseId"`
	Title      string `json:"displayTitle"`
	Branch     string `json:"headBranch"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
	Workflow   string `json:"workflowName"`
}

func (it ghItem) labels() []string {
	out := make([]string, 0, len(it.Labels))
	for _, l := range it.Labels {
		out = append(out, l.Name)
	}
	return out
}

// Poll runs trigger i of workflow wf once and returns its new events.
// The first poll of a trigger only records what is there.
func (g *GitHub) Poll(ctx context.Context, wf string, i int, t workflow.Trigger) ([]Event, error) {
	if g.Run == nil {
		return nil, errors.New("github trigger: gh is not available")
	}
	if g.State == nil {
		g.State = MemState()
	}
	var found []Event
	var labels map[string][]string
	var err error
	switch t.GitHub {
	case KindIssueOpened, KindIssueLabeled:
		found, labels, err = g.issues(ctx, t)
	case KindPROpened:
		found, err = g.pulls(ctx, t)
	case KindMention:
		found, err = g.mentions(ctx, t)
	case KindCIFailed:
		found, err = g.runs(ctx, t)
	default:
		return nil, fmt.Errorf("github trigger: unknown event %q", t.GitHub)
	}
	if err != nil {
		return nil, err
	}
	id := ID(wf, i, t)
	g.State.mu.Lock()
	defer g.State.mu.Unlock()
	st := g.State.get(id)
	var out []Event
	for _, e := range found {
		if t.GitHub == KindIssueLabeled {
			// A label counts when the issue did not have it last time
			// (or the issue is new since the first poll).
			old, known := st.Labels[strconv.Itoa(e.Number)]
			if known && hasAny(old, e.Labels) {
				st.mark(e.Key)
				continue
			}
		}
		if st.mark(e.Key) && st.Ready {
			e.Workflow, e.Trigger = wf, i
			out = append(out, e)
		}
	}
	if labels != nil {
		st.Labels = labels
	}
	st.Ready = true
	if err := g.State.save(); err != nil {
		return out, fmt.Errorf("github trigger: state: %w", err)
	}
	return out, nil
}

func (g *GitHub) gh(ctx context.Context, t workflow.Trigger, args ...string) ([]byte, error) {
	if t.Repo != "" {
		args = append(args, "--repo", t.Repo)
	}
	out, stderr, err := g.Run(ctx, g.Dir, args...)
	if err != nil {
		if s := strings.TrimSpace(stderr); s != "" {
			return nil, fmt.Errorf("github trigger: gh %s: %s", args[0], firstLine(s))
		}
		return nil, fmt.Errorf("github trigger: gh %s: %w", args[0], err)
	}
	return []byte(out), nil
}

func (g *GitHub) issues(ctx context.Context, t workflow.Trigger) ([]Event, map[string][]string, error) {
	b, err := g.gh(ctx, t, "issue", "list", "--state", "open", "--limit", strconv.Itoa(pollLimit), "--json", "number,title,url,body,author,labels")
	if err != nil {
		return nil, nil, err
	}
	var items []ghItem
	if err := json.Unmarshal(b, &items); err != nil {
		return nil, nil, fmt.Errorf("github trigger: gh issue list: %w", err)
	}
	labels := map[string][]string{}
	var out []Event
	for _, it := range items {
		ls := it.labels()
		labels[strconv.Itoa(it.Number)] = ls
		e := itemEvent(it, t)
		switch t.GitHub {
		case KindIssueOpened:
			if len(t.Labels) > 0 && !hasAny(ls, t.Labels) {
				continue
			}
			e.Kind, e.Key = KindIssueOpened, "issue:"+strconv.Itoa(it.Number)
		case KindIssueLabeled:
			hit := matching(ls, t.Labels)
			if len(hit) == 0 {
				continue
			}
			e.Kind, e.Key, e.Labels = KindIssueLabeled, "labeled:"+strconv.Itoa(it.Number)+":"+strings.ToLower(strings.Join(hit, ",")), hit
		}
		out = append(out, e)
	}
	return out, labels, nil
}

func (g *GitHub) pulls(ctx context.Context, t workflow.Trigger) ([]Event, error) {
	b, err := g.gh(ctx, t, "pr", "list", "--state", "open", "--limit", strconv.Itoa(pollLimit), "--json", "number,title,url,body,author,labels,headRefName")
	if err != nil {
		return nil, err
	}
	var items []ghItem
	if err := json.Unmarshal(b, &items); err != nil {
		return nil, fmt.Errorf("github trigger: gh pr list: %w", err)
	}
	var out []Event
	for _, it := range items {
		if len(t.Labels) > 0 && !hasAny(it.labels(), t.Labels) {
			continue
		}
		e := itemEvent(it, t)
		e.Kind, e.Key, e.Branch = KindPROpened, "pr:"+strconv.Itoa(it.Number), it.Head
		out = append(out, e)
	}
	return out, nil
}

func (g *GitHub) mentions(ctx context.Context, t workflow.Trigger) ([]Event, error) {
	text := strings.ToLower(t.Text)
	var out []Event
	for _, kind := range []string{"issue", "pr"} {
		b, err := g.gh(ctx, t, kind, "list", "--state", "open", "--search", "sort:updated-desc", "--limit", strconv.Itoa(mentionLimit), "--json", "number,title,url,body,author,comments")
		if err != nil {
			return nil, err
		}
		var items []ghItem
		if err := json.Unmarshal(b, &items); err != nil {
			return nil, fmt.Errorf("github trigger: gh %s list: %w", kind, err)
		}
		for _, it := range items {
			base := itemEvent(it, t)
			base.Kind = KindMention
			if strings.Contains(strings.ToLower(it.Body), text) {
				e := base
				e.Key = kind + ":" + strconv.Itoa(it.Number) + ":body"
				out = append(out, e)
			}
			for ci, c := range it.Comments {
				if !strings.Contains(strings.ToLower(c.Body), text) {
					continue
				}
				e := base
				cid := c.ID
				if cid == "" {
					cid = c.URL
				}
				if cid == "" {
					cid = strconv.Itoa(ci)
				}
				e.Key = kind + ":" + strconv.Itoa(it.Number) + ":comment:" + cid
				e.Body, e.Author = c.Body, c.Author.Login
				if c.URL != "" {
					e.URL = c.URL
				}
				out = append(out, e)
			}
		}
	}
	return out, nil
}

func (g *GitHub) runs(ctx context.Context, t workflow.Trigger) ([]Event, error) {
	args := []string{"run", "list", "--limit", strconv.Itoa(pollLimit), "--json", "databaseId,displayTitle,headBranch,status,conclusion,url,workflowName"}
	if t.Branch != "" {
		args = append(args, "--branch", t.Branch)
	}
	b, err := g.gh(ctx, t, args...)
	if err != nil {
		return nil, err
	}
	var runs []ghRun
	if err := json.Unmarshal(b, &runs); err != nil {
		return nil, fmt.Errorf("github trigger: gh run list: %w", err)
	}
	var out []Event
	for _, r := range runs {
		if r.Conclusion != "failure" || r.ID == 0 {
			continue
		}
		title := r.Workflow
		if r.Title != "" {
			title += ": " + r.Title
		}
		out = append(out, Event{Kind: KindCIFailed, Key: "run:" + strconv.FormatInt(r.ID, 10), Source: "github",
			Repo: t.Repo, Title: title, URL: r.URL, Branch: r.Branch})
	}
	return out, nil
}

func itemEvent(it ghItem, t workflow.Trigger) Event {
	return Event{Source: "github", Repo: t.Repo, Number: it.Number, Title: it.Title, URL: it.URL,
		Body: it.Body, Author: it.Author.Login, Labels: it.labels()}
}

// matching are the labels of have that are in want.
func matching(have, want []string) []string {
	var out []string
	for _, h := range have {
		if hasAny([]string{h}, want) {
			out = append(out, h)
		}
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return clip(s, 200)
}
