package app

// Response chaining in the UI (API-1): a request that reads
// {{login.response...}} while login has no answer yet asks first
// ("Send login first?"); yes sends login, then the request itself with
// the fresh values.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

const confirmKindHTTPChainDep confirmKindEnum = 4512

// httpChainStep is a request that waits for the answer of dep.
type httpChainStep struct {
	file string
	vars []httpclient.Var
	req  httpclient.Request
	env  string
	dep  string // the request name it waits for
}

// httpMissingDeps are the requests of file that req reads an answer
// from but that have no answer yet (only names defined in file).
func (m *Model) httpMissingDeps(file string, req httpclient.Request) []string {
	refs := httpclient.ResponseRefs(req)
	if len(refs) == 0 || file == "" {
		return nil
	}
	src := m.httpS().chainSource(file)
	f, ok := m.httpReadFile(file)
	if !ok {
		return nil
	}
	var out []string
	for _, n := range refs {
		if n == req.Name {
			continue
		}
		if _, ok := src(n); ok {
			continue
		}
		if httpRequestNamed(f, n) != nil {
			out = append(out, n)
		}
	}
	return out
}

func httpRequestNamed(f httpclient.File, name string) *httpclient.Request {
	for i := range f.Requests {
		if f.Requests[i].Name == name {
			return &f.Requests[i]
		}
	}
	return nil
}

// httpAskChainDep shows the "Send login first?" card.
func (m *Model) httpAskChainDep(step httpChainStep, deps []string) {
	step.dep = deps[0]
	m.httpS().chainWait = &step
	msg := req2label(step.req) + " reads the answer of " + strings.Join(deps, ", ") + ", which has not run yet."
	if len(deps) > 1 {
		msg += " They are sent one by one, then this request."
	}
	m.confirm = confirm.New("Send "+deps[0]+" first?", msg,
		[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "first", Title: "Send " + deps[0] + " first", Style: confirm.StylePrimary}, {ID: "without", Title: "Send without it"}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindHTTPChainDep
}

func req2label(r httpclient.Request) string {
	if l := r.Label(); l != "" {
		return l
	}
	return "This request"
}

// httpChainDepConfirm answers the card.
func (m *Model) httpChainDepConfirm(id string) tea.Cmd {
	s := m.httpS()
	w := s.chainWait
	s.chainWait = nil
	if w == nil {
		return nil
	}
	switch id {
	case "first":
		f, ok := m.httpReadFile(w.file)
		dep := (*httpclient.Request)(nil)
		if ok {
			dep = httpRequestNamed(f, w.dep)
		}
		if dep == nil {
			s.chainNext = nil
			return m.pushToastDetail(toast.Warn, "No request named "+w.dep, "")
		}
		s.chainNext = append(s.chainNext, *w)
		return m.httpSendRequest(w.file, f.Vars, *dep, w.env)
	case "without":
		return m.httpSendResolved(w.file, w.vars, w.req, w.env, true)
	}
	s.chainNext = nil
	return nil
}

// httpChainContinue sends the waiting request once its dependency run
// is in; a failed dependency drops the queue with a toast.
func (m *Model) httpChainContinue(run *httpRun) tea.Cmd {
	s := m.httpS()
	if len(s.chainNext) == 0 {
		return nil
	}
	top := s.chainNext[len(s.chainNext)-1]
	if run.file != top.file || run.rr.Template.Name != top.dep {
		return nil
	}
	s.chainNext = s.chainNext[:len(s.chainNext)-1]
	if run.resp == nil || run.resp.Err != "" {
		s.chainNext = nil
		return m.pushToastDetail(toast.Warn, top.dep+" failed", req2label(top.req)+" was not sent")
	}
	return m.httpSendRequest(top.file, top.vars, top.req, top.env)
}
