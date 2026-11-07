package httpclient

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// The safe script language (API-8): one statement per comment line
// above a request. It has no file, network, process or loop access;
// it only reads the request's variables (pre) or the answer (post) and
// writes session variables.
//
//	# @pre set NAME = VALUE       before the send; VALUE may use {{…}}
//	# @pre unset NAME
//	# @post set NAME = SUBJECT    after the answer; SUBJECT is an assert
//	                              subject: status, time, header X, body,
//	                              body $.path
//	# @post unset NAME
//
// A set NAME is {{NAME}} in this request and in every later one of the
// session (it wins over file and environment variables).

// maxScriptValue caps one set value.
const maxScriptValue = 64 << 10

// maxScriptLines caps the statements of one request (pre + post).
const maxScriptLines = 50

// Script is one parsed statement.
type Script struct {
	Op    string // set, unset
	Name  string
	Value string // pre: template text; post: the subject text
	subj  Assert // post: the parsed subject (Op unused)
}

// ParseScript reads one statement; pre selects the pre-request form.
func ParseScript(s string, pre bool) (Script, error) {
	var sc Script
	s = strings.TrimSpace(s)
	word, rest, _ := strings.Cut(s, " ")
	rest = strings.TrimSpace(rest)
	switch word {
	case "unset":
		if !isPlainName(rest) {
			return sc, fmt.Errorf("unset needs one variable name")
		}
		return Script{Op: "unset", Name: rest}, nil
	case "set":
	case "":
		return sc, fmt.Errorf("empty script line")
	default:
		return sc, fmt.Errorf("unknown statement %q (use set or unset)", word)
	}
	name, val, ok := strings.Cut(rest, "=")
	name, val = strings.TrimSpace(name), strings.TrimSpace(val)
	if !ok || !isPlainName(name) || strings.HasPrefix(name, "$") {
		return sc, fmt.Errorf("write set NAME = VALUE")
	}
	if val == "" {
		return sc, fmt.Errorf("set %s needs a value", name)
	}
	if len(val) > maxScriptValue {
		return sc, fmt.Errorf("value longer than %d bytes", maxScriptValue)
	}
	sc = Script{Op: "set", Name: name, Value: val}
	if pre {
		return sc, nil
	}
	a, err := ParseAssert(val + " exists")
	if err != nil {
		return sc, fmt.Errorf("after the answer, set takes status, time, header NAME, body or body $.path: %v", err)
	}
	sc.subj = a
	return sc, nil
}

// Session holds the variables scripts set; safe for many goroutines.
type Session struct {
	mu   sync.Mutex
	vals map[string]string
}

// NewSession returns an empty session.
func NewSession() *Session { return &Session{vals: map[string]string{}} }

// Get returns a session variable.
func (s *Session) Get(name string) (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.vals[name]
	return v, ok
}

// Set stores a session variable.
func (s *Session) Set(name, v string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.vals[name] = v
	s.mu.Unlock()
}

// Delete removes a session variable.
func (s *Session) Delete(name string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.vals, name)
	s.mu.Unlock()
}

// Names lists the session variables, sorted (values stay inside).
func (s *Session) Names() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.vals))
	for k := range s.vals {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Clear removes every session variable.
func (s *Session) Clear() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.vals = map[string]string{}
	s.mu.Unlock()
}

// runPre runs the request's @pre lines in the send scope; it returns
// one failed result per line that did not parse.
func (sc *sendScope) runPre(lines []string) []AssertResult {
	var bad []AssertResult
	for i, l := range lines {
		if i >= maxScriptLines {
			bad = append(bad, AssertResult{Expr: "@pre …", Message: fmt.Sprintf("more than %d script lines", maxScriptLines)})
			break
		}
		st, err := ParseScript(l, true)
		if err != nil {
			bad = append(bad, AssertResult{Expr: "@pre " + l, Message: err.Error()})
			continue
		}
		if sc.pre == nil {
			sc.pre = map[string]*string{}
		}
		if st.Op == "unset" {
			sc.pre[st.Name] = nil
			sc.rv.Session.Delete(st.Name)
			continue
		}
		v := sc.expand(st.Value)
		sc.pre[st.Name] = &v
		sc.rv.Session.Set(st.Name, v)
	}
	return bad
}

// RunPost runs @post lines against resp and stores the values in sess.
// Each line gives one result: passed when the value was found.
func RunPost(lines []string, resp *Response, sess *Session) []AssertResult {
	if len(lines) == 0 {
		return nil
	}
	out := make([]AssertResult, 0, len(lines))
	var doc any
	docState := 0
	for i, l := range lines {
		r := AssertResult{Expr: "@post " + l}
		if i >= maxScriptLines {
			r.Message = fmt.Sprintf("more than %d script lines", maxScriptLines)
			out = append(out, r)
			break
		}
		st, err := ParseScript(l, false)
		switch {
		case err != nil:
			r.Message = err.Error()
		case st.Op == "unset":
			sess.Delete(st.Name)
			r.Pass = true
		case resp == nil || resp.Err != "":
			r.Message = "no response (the send failed)"
		default:
			if st.subj.path != nil && docState == 0 {
				docState = 2
				if json.Unmarshal(resp.Body, &doc) == nil {
					docState = 1
				}
			}
			v, ok, why := subjectValue(st.subj, resp, doc, docState == 1)
			if ok {
				sess.Set(st.Name, v)
				r.Pass = true
			} else {
				r.Message = why
			}
		}
		out = append(out, r)
	}
	return out
}

// subjectValue is the text an assert subject picks from resp.
func subjectValue(a Assert, resp *Response, doc any, isJSON bool) (string, bool, string) {
	switch a.Subject {
	case "status":
		return fmt.Sprint(resp.Status), true, ""
	case "time":
		return fmt.Sprint(resp.Elapsed.Milliseconds()), true, ""
	case "header":
		for i := len(resp.Headers) - 1; i >= 0; i-- {
			if strings.EqualFold(resp.Headers[i].Name, a.Arg) {
				return resp.Headers[i].Value, true, ""
			}
		}
		return "", false, "header " + a.Arg + " is missing"
	}
	if a.path == nil {
		return resp.Text(), true, ""
	}
	if !isJSON {
		return "", false, "the body is not JSON"
	}
	v, ok := walkPath(doc, a.path)
	if !ok {
		return "", false, "body " + a.Arg + " is missing"
	}
	return jsonText(v), true, ""
}

// RunTests is everything checked after one send, in order: the @pre
// lines that did not parse, the @assert lines and the @post lines.
func RunTests(rr Resolved, resp *Response, sess *Session) []AssertResult {
	out := append([]AssertResult(nil), rr.PreErrors...)
	out = append(out, RunAsserts(rr.Asserts, resp)...)
	return append(out, RunPost(rr.Post, resp, sess)...)
}

// HasTests reports whether a request has asserts or scripts.
func (r *Request) HasTests() bool {
	return len(r.Asserts)+len(r.Pre)+len(r.Post) > 0
}
