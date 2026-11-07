package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/jsonc"
)

// Parse reads and checks one workflow file. path is used in messages
// and, when it ends in .jsonc, the file's name must match it ("" skips
// that check). It does no I/O.
//
// The error is a *ParseError listing every problem with its line;
// warnings (unknown keys and other things that do not stop a run) come
// with a good file. No message holds a value that may be a secret.
func Parse(path string, data []byte) (*File, []Problem, error) {
	c := &checker{src: data}
	f := c.parse(path, data)
	if len(c.errs) > 0 {
		return nil, c.warns, &ParseError{File: path, Problems: c.errs}
	}
	return f, c.warns, nil
}

var (
	idRe       = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)
	serverRe   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`) // an mcp.json server name
	timeRe     = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	itemPathRe = regexp.MustCompile(`^\$((\.[A-Za-z_][A-Za-z0-9_-]*)|(\[\*\])|(\[[0-9]{1,6}\]))*$`)
)

type checker struct {
	src   []byte
	errs  []Problem
	warns []Problem
	steps int // all steps seen
	ids   map[string]int
}

func (c *checker) line(off int) int {
	if off <= 0 || c.src == nil {
		return 1
	}
	if off > len(c.src) {
		off = len(c.src)
	}
	return bytes.Count(c.src[:off], []byte("\n")) + 1
}

func (c *checker) errAt(off int, where, format string, a ...any) {
	c.errs = append(c.errs, c.problem(off, where, format, a...))
}

func (c *checker) warnAt(off int, where, format string, a ...any) {
	c.warns = append(c.warns, c.problem(off, where, format, a...))
}

// problem builds one message. A key that looks like a token can be part
// of where; it is hidden like a value (a backstop to quote).
func (c *checker) problem(off int, where, format string, a ...any) Problem {
	return Problem{Line: c.line(off), Where: hideTokens(where), Msg: hideTokens(fmt.Sprintf(format, a...))}
}

func (c *checker) parse(p string, data []byte) *File {
	if len(data) > MaxFileBytes {
		c.errs = append(c.errs, Problem{Msg: fmt.Sprintf("the file is %d KB; the most is %d KB", (len(data)+1023)/1024, MaxFileBytes>>10)})
		return nil
	}
	if !utf8.Valid(data) {
		c.errs = append(c.errs, Problem{Msg: "the file is not UTF-8 text"})
		return nil
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	c.src = data
	root, err := parseTree(jsonc.Blank(data))
	if err != nil {
		var se *syntaxErr
		if errors.As(err, &se) {
			c.errAt(se.off, "", "%s", se.msg)
		} else {
			c.errs = append(c.errs, Problem{Msg: "not valid JSON"})
		}
		return nil
	}
	if root.kind != kObject {
		c.errAt(root.off, "", "the file must be one { … } object")
		return nil
	}
	c.scanSecrets(root, "", secretNo)

	o := c.object(root, "")
	f := &File{Hash: HashBytes(data), ResumeOnStart: true}

	vn := o.get("version")
	switch {
	case vn == nil:
		c.errAt(root.off, "", `"version" is missing; add "version": 1`)
		o.forget() // nothing else is trusted
		return nil
	case vn.kind != kNumber:
		c.errAt(vn.off, "version", "must be the number 1")
		return nil
	default:
		v, err := strconv.Atoi(vn.num.String())
		switch {
		case err != nil || v < 1:
			c.errAt(vn.off, "version", "must be the number 1")
			return nil
		case v > Version:
			c.errAt(vn.off, "version", "this file was made by a newer termocode (version %d); this one reads version %d. Update termocode.", v, Version)
			return nil
		}
		f.Version = v
	}

	f.Name = o.str("name")
	switch {
	case f.Name == "":
		if o.get("name") == nil {
			c.errAt(root.off, "", `"name" is missing`)
		}
	case !idRe.MatchString(f.Name):
		c.errAt(o.off("name"), "name", "use a-z, 0-9, _ and -, starting with a letter, at most 40 characters")
	default:
		if want := NameFromPath(p); want != "" && want != f.Name {
			c.errAt(o.off("name"), "name", "is %s but the file is %s; they must match", quote(f.Name), quote(want+Ext))
		}
	}
	f.Title = o.str("title")
	f.Description = o.str("description")
	f.Schedule = c.schedule(o)
	f.Triggers = c.triggers(o)
	f.Vars = c.vars(o)
	f.Limits = c.limits(o)
	if r := c.retries(o, "retries"); r != nil {
		f.Retries = *r
	}
	f.OnError = o.enum("on_error", OnErrorContinue, OnErrorContinue, OnErrorStop)
	if n := o.get("resume_on_start"); n != nil {
		f.ResumeOnStart = o.boolean("resume_on_start")
	}
	c.ids = map[string]int{}
	sn := o.get("steps")
	switch {
	case sn == nil:
		c.errAt(root.off, "", `"steps" is missing; a workflow needs at least one step`)
	case sn.kind != kArray:
		c.errAt(sn.off, "steps", "must be a list of steps")
	case len(sn.vals) == 0:
		c.errAt(sn.off, "steps", "is empty; a workflow needs at least one step")
	default:
		f.Steps = c.stepList(sn, "steps", 0, f.Limits.MaxItems)
	}
	if c.steps > MaxSteps {
		c.errAt(sn.off, "steps", "%d steps in all; the most is %d", c.steps, MaxSteps)
	}
	f.Notify = c.notify(o, f)
	c.checkRefs(f)
	o.finish(topKeys)
	return f
}

var topKeys = []string{"$schema", "version", "name", "title", "description", "schedule", "on", "vars", "limits", "retries", "on_error", "notify", "resume_on_start", "steps"}

func (c *checker) schedule(o *obj) *Schedule {
	n := o.get("schedule")
	if n == nil {
		return nil
	}
	so := c.object(n, "schedule")
	if so == nil {
		return nil
	}
	s := &Schedule{
		Preset: so.enum("preset", "", SchedulePresets...),
		Time:   so.str("time"),
		Cron:   so.str("cron"),
		TZ:     so.str("tz"),
	}
	s.Precheck = so.argv("precheck")
	switch {
	case s.Preset == "" && s.Cron == "":
		c.errAt(n.off, "schedule", `needs "preset" (hourly, daily, weekdays, weekly, monthly) or "cron"`)
	case s.Preset != "" && s.Cron != "":
		c.errAt(n.off, "schedule", `has both "preset" and "cron"; keep one`)
	}
	if s.Time != "" && !timeRe.MatchString(s.Time) {
		c.errAt(so.off("time"), "schedule.time", `must be HH:MM, like "09:00"`)
	}
	if s.Cron != "" && len(strings.Fields(s.Cron)) != 5 {
		c.errAt(so.off("cron"), "schedule.cron", `must have 5 fields: minute hour day month weekday, like "0 9 * * 1-5"`)
	}
	if s.TZ != "" && strings.ContainsAny(s.TZ, " \t\\") {
		c.errAt(so.off("tz"), "schedule.tz", `must be a zone name like "Europe/Berlin"`)
	}
	so.finish([]string{"preset", "time", "cron", "tz", "precheck"})
	return s
}

var triggerKeys = []string{"github", "webhook", "from", "repo", "labels", "text", "branch", "ask", "worktree", "base"}

// triggers reads "on": a list of GitHub and webhook triggers.
func (c *checker) triggers(o *obj) []Trigger {
	n := o.get("on")
	if n == nil {
		return nil
	}
	if n.kind != kArray {
		c.errAt(n.off, "on", "must be a list of triggers, like [{\"github\": \"issue_labeled\", \"labels\": [\"agent\"]}]")
		return nil
	}
	if len(n.vals) > MaxTriggers {
		c.errAt(n.off, "on", "has %d triggers; the most is %d", len(n.vals), MaxTriggers)
		return nil
	}
	var out []Trigger
	hooks := map[string]bool{}
	for i, v := range n.vals {
		where := fmt.Sprintf("on[%d]", i)
		to := c.object(v, where)
		if to == nil {
			continue
		}
		t := Trigger{Line: c.line(v.off), Ask: true}
		t.GitHub = to.enum("github", "", GitHubEvents...)
		t.Webhook = to.str("webhook")
		t.From = to.enum("from", "", WebhookFroms...)
		t.Repo = to.str("repo")
		t.Labels = to.strList("labels")
		t.Text = to.str("text")
		t.Branch = to.str("branch")
		if to.get("ask") != nil {
			t.Ask = to.boolean("ask")
		}
		t.Worktree = to.boolean("worktree")
		t.Base = to.str("base")
		gh, wh := to.get("github") != nil, to.get("webhook") != nil
		switch {
		case !gh && !wh:
			c.errAt(v.off, where, `needs "github" (%s) or "webhook" (a name)`, strings.Join(GitHubEvents, ", "))
		case gh && wh:
			c.errAt(v.off, where, `has both "github" and "webhook"; keep one`)
		case wh:
			if !idRe.MatchString(t.Webhook) {
				c.errAt(to.off("webhook"), where+".webhook", "use a-z, 0-9, _ and -, starting with a letter, at most 40 characters")
			} else if hooks[t.Webhook] {
				c.errAt(to.off("webhook"), where+".webhook", "%s is used twice in this file", quote(t.Webhook))
			}
			hooks[t.Webhook] = true
			if t.From == "" {
				t.From = "generic"
			}
			for _, k := range []string{"repo", "labels", "text", "branch"} {
				if to.get(k) != nil {
					c.warnAt(to.off(k), where+"."+k, "is for github triggers; a webhook ignores it")
				}
			}
		default:
			if to.get("from") != nil {
				c.warnAt(to.off("from"), where+".from", "is for webhook triggers; a github trigger ignores it")
			}
			if t.Repo != "" && !triggerRepoRe.MatchString(t.Repo) {
				c.errAt(to.off("repo"), where+".repo", `must be "owner/name"`)
			}
			if t.GitHub == "issue_labeled" && len(t.Labels) == 0 {
				c.errAt(v.off, where, `issue_labeled needs "labels": the labels that start the workflow`)
			}
			if t.GitHub == "mention" && t.Text == "" {
				t.Text = "@termocode"
			}
		}
		if strings.HasPrefix(t.Base, "-") {
			c.errAt(to.off("base"), where+".base", "must be a branch, tag or commit, not an option")
		}
		if t.Base != "" && !t.Worktree {
			c.warnAt(to.off("base"), where+".base", `is used only with "worktree": true`)
		}
		to.finish(triggerKeys)
		out = append(out, t)
	}
	return out
}

var triggerRepoRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}/[A-Za-z0-9._-]{1,100}$`)

func (c *checker) vars(o *obj) map[string]any {
	n := o.get("vars")
	if n == nil {
		return nil
	}
	vo := c.object(n, "vars")
	if vo == nil {
		return nil
	}
	out := map[string]any{}
	for i, k := range n.keys {
		v := n.vals[i]
		where := "vars." + k
		if !idRe.MatchString(k) {
			c.errAt(n.koff[i], "vars", "the name %s: use a-z, 0-9, _ and -, starting with a letter, at most 40 characters", quote(k))
			continue
		}
		switch v.kind {
		case kString, kNumber, kBool:
			out[k] = v.plain()
		default:
			c.errAt(v.off, where, "must be a string, a number or true/false")
		}
	}
	vo.forget()
	return out
}

func (c *checker) limits(o *obj) Limits {
	l := Limits{MaxItems: DefaultMaxItems}
	n := o.get("limits")
	if n == nil {
		return l
	}
	lo := c.object(n, "limits")
	if lo == nil {
		return l
	}
	l.BudgetUSD = lo.num("budget_usd", 0, 1e6)
	l.ItemBudgetUSD = lo.num("item_budget_usd", 0, 1e6)
	l.Parallel = lo.intIn("parallel", 1, 16, 0)
	l.Timeout = lo.dur("timeout")
	l.ItemTimeout = lo.dur("item_timeout")
	l.Wait = lo.dur("wait")
	l.MaxItems = lo.intIn("max_items", 1, MaxItemsCap, DefaultMaxItems)
	lo.finish([]string{"budget_usd", "item_budget_usd", "parallel", "timeout", "item_timeout", "wait", "max_items"})
	return l
}

func (c *checker) retries(o *obj, key string) *Retries {
	n := o.get(key)
	if n == nil {
		return nil
	}
	ro := c.object(n, o.at(key))
	if ro == nil {
		return nil
	}
	r := &Retries{Max: ro.intIn("max", 0, 10, 0), Backoff: ro.dur("backoff")}
	ro.finish([]string{"max", "backoff"})
	return r
}

func (c *checker) notify(o *obj, f *File) []string {
	list := o.strList("notify")
	for i, s := range list {
		where := fmt.Sprintf("notify[%d]", i)
		switch {
		case s == "os", s == "web":
		case strings.HasPrefix(s, "message:"):
			id := strings.TrimPrefix(s, "message:")
			st := f.StepByID(id)
			if st == nil || st.Write == nil || st.Write.To != "message" {
				c.errAt(o.elemOff("notify", i), where, "%s is not a message write step", quote(id))
			} else if strings.Contains(st.Write.Channel, "{{") {
				c.errAt(o.elemOff("notify", i), where, `the summary goes to %s after the run, so its "channel" cannot hold {{…}}; write the channel id`, quote(id))
			}
		default:
			c.errAt(o.elemOff("notify", i), where, `use "os", "web" or "message:<step id>"`)
		}
	}
	return list
}

// checkRefs: steps.<id> in an each's items, and a handoff, must name a
// step.
func (c *checker) checkRefs(f *File) {
	Walk(f.Steps, func(s *Step) bool {
		if s.Handoff != "" && s.Handoff != s.ID {
			if _, known := c.ids[s.Handoff]; !known {
				c.errs = append(c.errs, Problem{Line: s.Line, Where: "step " + quote(s.ID) + ".handoff", Msg: fmt.Sprintf("no step has the id %s", quote(s.Handoff))})
			}
		}
		if s.Each == nil {
			return true
		}
		if id, ok := strings.CutPrefix(s.Each.Items, "steps."); ok {
			id, _, _ = strings.Cut(id, ".")
			if _, known := c.ids[id]; !known {
				c.errs = append(c.errs, Problem{Line: s.Line, Where: "step " + quote(s.ID) + ".items", Msg: fmt.Sprintf("no step has the id %s", quote(id))})
			}
		}
		return true
	})
}

// ── steps ───────────────────────────────────────────────────────────────

var commonStepKeys = []string{"id", "type", "title", "when", "else", "retries", "timeout", "on_error", "handoff"}

func (c *checker) stepList(n *node, where string, eachDepth, maxItems int) []Step {
	out := make([]Step, 0, len(n.vals))
	sources := 0
	for i, sn := range n.vals {
		w := fmt.Sprintf("%s[%d]", where, i)
		s, ok := c.step(sn, w, eachDepth, maxItems)
		if !ok {
			continue
		}
		if s.Type == TypeSource {
			sources++
			if sources == 2 {
				c.errAt(sn.off, w, "a second source in one list; use one source per workflow or per each")
			}
		}
		out = append(out, s)
	}
	return out
}

func (c *checker) step(n *node, where string, eachDepth, maxItems int) (Step, bool) {
	c.steps++
	o := c.object(n, where)
	if o == nil {
		return Step{}, false
	}
	s := Step{Line: c.line(n.off)}
	s.ID = o.str("id")
	if s.ID != "" {
		if idRe.MatchString(s.ID) {
			where = fmt.Sprintf("%s (%s)", where, s.ID)
			o.where = where
		}
		switch {
		case !idRe.MatchString(s.ID):
			c.errAt(o.off("id"), where+".id", "use a-z, 0-9, _ and -, starting with a letter, at most 40 characters")
		case c.ids[s.ID] > 0:
			c.errAt(o.off("id"), where+".id", "%s is already the id of the step on line %d", quote(s.ID), c.ids[s.ID])
		default:
			c.ids[s.ID] = s.Line
		}
	}
	tn := o.get("type")
	if tn == nil {
		c.errAt(n.off, where, `"type" is missing; use one of: %s`, strings.Join(StepTypes, ", "))
		o.forget()
		return Step{}, false
	}
	s.Type = o.str("type")
	if !contains(StepTypes, s.Type) {
		if tn.kind == kString {
			c.errAt(tn.off, where+".type", "%s is not a step type; use one of: %s%s", quote(s.Type), strings.Join(StepTypes, ", "), suggest(s.Type, StepTypes))
		}
		o.forget()
		return Step{}, false
	}
	s.Title = o.str("title")
	s.When = o.expr("when")
	s.Retries = c.retries(o, "retries")
	s.Timeout = o.dur("timeout")
	s.OnError = o.enum("on_error", "", OnErrorContinue, OnErrorStop)
	s.Handoff = o.str("handoff")
	switch {
	case s.Handoff == "":
	case s.Type != TypeAI && s.Type != TypeAgent && s.Type != TypePipeline:
		c.errAt(o.off("handoff"), where+".handoff", `"handoff" is only for ai, agent and pipeline steps`)
	case s.Handoff == s.ID:
		c.errAt(o.off("handoff"), where+".handoff", "a step cannot hand off to itself")
	}
	if en := o.get("else"); en != nil {
		if en.kind != kArray {
			c.errAt(en.off, where+".else", "must be a list of steps")
		} else {
			s.Else = c.stepList(en, where+".else", eachDepth, maxItems)
		}
	}
	var keys []string
	switch s.Type {
	case TypeSource:
		s.Source, keys = c.source(o, maxItems)
	case TypePlan:
		s.Plan = &PlanSpec{Profile: o.str("profile"), Model: o.str("model")}
		s.Plan.Prompt, s.Plan.PromptFile = o.prompt()
		s.Plan.Output = o.strMap("output")
		keys = []string{"profile", "model", "prompt", "prompt_file", "output"}
	case TypeFilter:
		s.Filter = &FilterSpec{Sort: o.expr("sort"), Take: o.intIn("take", 0, MaxItemsCap, 0), Dedupe: o.expr("dedupe")}
		if s.When == "" && s.Filter.Dedupe == "" && s.Filter.Take == 0 {
			c.errAt(n.off, where, `a filter needs "when" (the keep test), "dedupe" or "take"`)
		}
		keys = []string{"sort", "take", "dedupe"}
	case TypeEach:
		s.Each, keys = c.each(o, n, where, eachDepth, maxItems)
	case TypeAI:
		a := &AISpec{Profile: o.str("profile"), Model: o.str("model"), ApprovePlan: o.boolean("approve_plan"), BudgetUSD: o.num("budget_usd", 0, 1e6)}
		a.Prompt, a.PromptFile = o.prompt()
		a.Tools = c.tools(o)
		if a.Profile != "" && a.Model != "" {
			c.errAt(n.off, where, `has both "profile" and "model"; keep one`)
		}
		if a.Profile == "" && o.get("prompt") == nil && o.get("prompt_file") == nil {
			c.errAt(n.off, where, `needs "prompt" or "prompt_file" (or a "profile")`)
		}
		s.AI = a
		keys = []string{"profile", "model", "prompt", "prompt_file", "tools", "approve_plan", "budget_usd"}
	case TypeAgent:
		a := &AgentSpec{Agent: o.enum("agent", "", Agents...), Sandbox: o.enum("sandbox", "", Sandboxes...), Model: o.str("model"), Effort: o.str("effort")}
		a.Prompt, a.PromptFile = o.prompt()
		if o.get("agent") == nil {
			c.errAt(n.off, where, `"agent" is missing; use one of: %s`, strings.Join(Agents, ", "))
		}
		if o.get("prompt") == nil && o.get("prompt_file") == nil {
			c.errAt(n.off, where, `needs "prompt" or "prompt_file"`)
		}
		s.Agent = a
		keys = []string{"agent", "prompt", "prompt_file", "sandbox", "model", "effort"}
	case TypePipeline:
		p := &PipelineSpec{Profile: o.str("profile")}
		p.Prompt, p.PromptFile = o.prompt()
		if p.Profile == "" {
			o.missing("profile")
		}
		s.Pipeline = p
		keys = []string{"profile", "prompt", "prompt_file"}
	case TypeTests:
		t := &TestsSpec{Task: o.str("task"), Argv: o.argv("argv")}
		if t.Task != "" && len(t.Argv) > 0 {
			c.errAt(n.off, where, `has both "task" and "argv"; keep one`)
		}
		s.Tests = t
		keys = []string{"task", "argv"}
	case TypeShell:
		sh := &ShellSpec{Argv: o.argv("argv"), Cwd: o.relPath("cwd"), Env: o.strMap("env")}
		if len(sh.Argv) == 0 && o.get("argv") == nil {
			o.missing("argv")
		}
		s.Shell = sh
		keys = []string{"argv", "cwd", "env"}
	case TypeWrite:
		s.Write, keys = c.write(o, n, where)
	case TypeLoop:
		s.Loop, keys = c.loop(o, n, where, eachDepth, maxItems)
	case TypeBranch:
		s.Branch, keys = c.branch(o, n, where, eachDepth, maxItems)
	case TypeWait:
		s.Wait, keys = c.wait(o, n, where)
	}
	o.finish(append(append([]string(nil), commonStepKeys...), keys...))
	return s, true
}

func (c *checker) source(o *obj, maxItems int) (*SourceSpec, []string) {
	s := &SourceSpec{From: o.enum("from", "", SourceKinds...), Items: o.str("items"), Key: o.str("key")}
	s.MaxItems = o.intIn("max_items", 1, MaxItemsCap, maxItems)
	if o.get("from") == nil {
		c.errAt(o.n.off, o.where, `"from" is missing; use one of: %s`, strings.Join(SourceKinds, ", "))
	}
	if s.Items != "" && !itemPathRe.MatchString(s.Items) {
		c.errAt(o.off("items"), o.at("items"), `must be a path like "$.issues" or "$.data[*].node"`)
	}
	keys := []string{"from", "items", "key", "max_items"}
	switch s.From {
	case "mcp":
		s.Server, s.Tool, s.Args = o.req("server"), o.req("tool"), o.anyMap("args")
		keys = append(keys, "server", "tool", "args")
	case "github":
		s.What = o.enum("what", "", "issues", "pulls", "checks")
		if o.get("what") == nil {
			o.missing("what")
		}
		s.Args = o.anyMap("args")
		keys = append(keys, "what", "args")
	case "shell":
		s.Argv = o.argv("argv")
		if len(s.Argv) == 0 && o.get("argv") == nil {
			o.missing("argv")
		}
		s.Parse = o.enum("parse", "json", "json", "jsonl", "lines")
		s.Cwd = o.relPath("cwd")
		s.Env = o.strMap("env")
		keys = append(keys, "argv", "parse", "cwd", "env")
	case "files":
		s.Glob, s.ChangedSince = o.req("glob"), o.str("changed_since")
		if s.Glob != "" && (path.IsAbs(s.Glob) || hasDotDot(s.Glob)) {
			c.errAt(o.off("glob"), o.at("glob"), "must stay inside the workspace (no leading / and no ..)")
		}
		keys = append(keys, "glob", "changed_since")
	case "http":
		s.Request = o.relPath("request")
		if s.Request == "" && o.get("request") == nil {
			o.missing("request")
		}
		s.Headers = o.strMap("headers")
		keys = append(keys, "request", "headers")
	case "db":
		s.Connection, s.SQL = o.req("connection"), o.req("sql")
		if s.SQL != "" && !selectOnly(s.SQL) {
			c.errAt(o.off("sql"), o.at("sql"), "a source may only read: start with SELECT (or WITH … SELECT) and run one statement")
		}
		keys = append(keys, "connection", "sql")
	}
	return s, keys
}

func (c *checker) each(o *obj, n *node, where string, depth, maxItems int) (*EachSpec, []string) {
	e := &EachSpec{Items: o.expr("items"), Parallel: o.intIn("parallel", 1, 16, 0), Worktree: o.boolean("worktree"),
		Base: o.str("base"), Cleanup: o.enum("cleanup", "keep", "keep", "discard")}
	if e.Items == "" && o.get("items") == nil {
		o.missing("items")
	}
	if depth+1 > MaxEachDepth {
		c.errAt(n.off, where, "each is nested %d deep; the most is %d", depth+1, MaxEachDepth)
	}
	sn := o.get("steps")
	switch {
	case sn == nil:
		o.missing("steps")
	case sn.kind != kArray || len(sn.vals) == 0:
		c.errAt(sn.off, where+".steps", "must be a list with at least one step")
	default:
		if depth+1 <= MaxEachDepth {
			e.Steps = c.stepList(sn, where+".steps", depth+1, maxItems)
		}
	}
	return e, []string{"items", "parallel", "worktree", "base", "cleanup", "steps"}
}

// nested checks the depth of a loop or a branch (each, loop and branch
// count together).
func (c *checker) nested(n *node, where, what string, depth int) bool {
	if depth+1 > MaxEachDepth {
		c.errAt(n.off, where, "%s is nested %d deep; the most is %d", what, depth+1, MaxEachDepth)
		return false
	}
	return true
}

// subSteps reads the "steps" list of o (a loop, or one branch).
func (c *checker) subSteps(o *obj, where string, depth, maxItems int, deepOK bool) []Step {
	sn := o.get("steps")
	switch {
	case sn == nil:
		o.missing("steps")
	case sn.kind != kArray || len(sn.vals) == 0:
		c.errAt(sn.off, where+".steps", "must be a list with at least one step")
	case deepOK:
		return c.stepList(sn, where+".steps", depth+1, maxItems)
	}
	return nil
}

func (c *checker) loop(o *obj, n *node, where string, depth, maxItems int) (*LoopSpec, []string) {
	l := &LoopSpec{Max: o.intIn("max", 1, MaxLoopRounds, 0), Until: o.expr("until"), BudgetUSD: o.num("budget_usd", 0, 1e6)}
	if o.get("max") == nil {
		o.missing("max")
	}
	ok := c.nested(n, where, "loop", depth)
	l.Steps = c.subSteps(o, where, depth, maxItems, ok)
	return l, []string{"max", "until", "budget_usd", "steps"}
}

func (c *checker) branch(o *obj, n *node, where string, depth, maxItems int) (*BranchSpec, []string) {
	b := &BranchSpec{Join: o.enum("join", JoinAll, JoinAll, JoinAny)}
	ok := c.nested(n, where, "branch", depth)
	bn := o.get("branches")
	switch {
	case bn == nil:
		o.missing("branches")
	case bn.kind != kArray:
		c.errAt(bn.off, where+".branches", `must be a list of { "name": …, "steps": [ … ] }`)
	default:
		if len(bn.vals) < MinBranches || len(bn.vals) > MaxBranches {
			c.errAt(bn.off, where+".branches", "needs %d to %d branches; it has %d", MinBranches, MaxBranches, len(bn.vals))
		}
		names := map[string]bool{}
		for i, v := range bn.vals {
			w := fmt.Sprintf("%s.branches[%d]", where, i)
			bo := c.object(v, w)
			if bo == nil {
				continue
			}
			br := Branch{Name: bo.req("name")}
			switch {
			case br.Name == "":
			case !idRe.MatchString(br.Name):
				c.errAt(bo.off("name"), w+".name", "use a-z, 0-9, _ and -, starting with a letter, at most 40 characters")
			case names[br.Name]:
				c.errAt(bo.off("name"), w+".name", "%s is already the name of a branch in this step", quote(br.Name))
			default:
				names[br.Name] = true
			}
			br.Steps = c.subSteps(bo, w, depth, maxItems, ok)
			bo.finish([]string{"name", "steps"})
			b.Branches = append(b.Branches, br)
		}
	}
	return b, []string{"join", "branches"}
}

func (c *checker) wait(o *obj, n *node, where string) (*WaitSpec, []string) {
	w := &WaitSpec{For: o.enum("for", "", WaitFors...), PR: o.req("pr"), Every: o.dur("every")}
	if o.get("for") == nil {
		c.errAt(n.off, where, `"for" is missing; use one of: %s`, strings.Join(WaitFors, ", "))
	}
	switch {
	case w.Every == 0:
		w.Every = DefaultWaitEvery
	case w.Every < MinWaitEvery:
		c.errAt(o.off("every"), o.at("every"), "must be at least %s", MinWaitEvery)
		w.Every = DefaultWaitEvery
	}
	return w, []string{"for", "pr", "every"}
}

func (c *checker) tools(o *obj) *ToolRules {
	n := o.get("tools")
	if n == nil {
		return nil
	}
	to := c.object(n, o.at("tools"))
	if to == nil {
		return nil
	}
	t := &ToolRules{Preset: to.str("preset"), Deny: to.strList("deny")}
	if to.get("allow") != nil {
		c.errAt(to.off("allow"), to.at("allow"), `a workflow can only remove tools; use "deny"`)
	}
	to.finish([]string{"preset", "deny", "allow"})
	return t
}

func (c *checker) write(o *obj, n *node, where string) (*WriteSpec, []string) {
	w := &WriteSpec{To: o.enum("to", "", WriteTargets...), Gate: o.enum("gate", GateAsk, GateAsk, GateAllow, GateDry)}
	if o.get("to") == nil {
		c.errAt(n.off, where, `"to" is missing; use one of: %s`, strings.Join(WriteTargets, ", "))
	}
	keys := []string{"to", "gate"}
	body := func() {
		w.Body, w.BodyFile = o.text("body"), o.relPath("body_file")
		if w.Body != "" && w.BodyFile != "" {
			c.errAt(n.off, where, `has both "body" and "body_file"; keep one`)
		}
		keys = append(keys, "body", "body_file")
	}
	switch w.To {
	case "pr":
		w.Title, w.Base, w.Draft, w.WaitCI = o.req("title"), o.str("base"), o.boolean("draft"), o.boolean("wait_ci")
		body()
		keys = append(keys, "title", "base", "draft", "wait_ci")
	case "commit":
		w.Message = o.text("message")
		if w.Message == "" {
			o.missing("message")
		}
		keys = append(keys, "message")
	case "mcp", "status":
		w.Server, w.Tool, w.Args = o.req("server"), o.req("tool"), o.anyMap("args")
		if un := o.get("undo"); un != nil {
			if uo := c.object(un, o.at("undo")); uo != nil {
				w.Undo = &UndoSpec{Tool: uo.req("tool"), Args: uo.anyMap("args")}
				uo.finish([]string{"tool", "args"})
			}
		}
		keys = append(keys, "server", "tool", "args", "undo")
	case "github.comment":
		w.PR = o.req("pr")
		keys = append(keys, "pr")
		body()
		if w.Body == "" && w.BodyFile == "" {
			c.errAt(n.off, where, `needs "body" or "body_file"`)
		}
	case "file":
		w.Path, w.Content = o.relPath("path"), o.text("content")
		if w.Path == "" && o.get("path") == nil {
			o.missing("path")
		}
		w.Mode = o.enum("mode", "create", "create", "overwrite", "append")
		keys = append(keys, "path", "content", "mode")
	case "http":
		w.Request = o.relPath("request")
		if w.Request == "" && o.get("request") == nil {
			o.missing("request")
		}
		w.Headers, w.Body = o.strMap("headers"), o.text("body")
		keys = append(keys, "request", "headers", "body")
	case "message":
		w.Via, w.Text = o.req("via"), o.text("text")
		w.Channel, w.Tool = o.str("channel"), o.str("tool")
		keys = append(keys, "via", "text", "channel", "tool")
		via := strings.TrimSpace(w.Via)
		switch {
		case via == "" || strings.Contains(via, "${") || strings.Contains(via, "://"): // a webhook: checked when it is sent
		case via == "os" || via == "web":
			if w.Channel != "" || w.Tool != "" {
				c.errAt(n.off, where, `"channel" and "tool" are only for a message via an MCP server`)
			}
		case !serverRe.MatchString(via):
			c.errAt(o.off("via"), o.at("via"), `use "os", "web", ${secret:NAME} (a webhook) or an MCP server name`)
		case strings.TrimSpace(w.Channel) == "":
			c.errAt(n.off, where, `a message via the MCP server %q needs "channel"`, via)
		}
	}
	return w, keys
}

// selectOnly reports whether sql is one read-only statement.
func selectOnly(sql string) bool {
	s := strings.TrimSpace(sql)
	s = strings.TrimSuffix(s, ";")
	if strings.Contains(s, ";") {
		return false
	}
	first := strings.ToLower(strings.Fields(s + " x")[0])
	return first == "select" || first == "with"
}

func hasDotDot(p string) bool {
	for _, part := range strings.Split(strings.ReplaceAll(p, "\\", "/"), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ── obj: one object's fields, with the keys read so far ─────────────────

type obj struct {
	c     *checker
	n     *node
	where string
	used  map[string]bool
}

// object wraps n; when n is not an object it reports that and returns nil.
func (c *checker) object(n *node, where string) *obj {
	if n.kind != kObject {
		c.errAt(n.off, where, "must be an object { … }, not %s", n.kind)
		return nil
	}
	return &obj{c: c, n: n, where: where, used: map[string]bool{}}
}

func (o *obj) at(key string) string {
	if o.where == "" {
		return key
	}
	return o.where + "." + key
}

func (o *obj) index(key string) int {
	for i, k := range o.n.keys {
		if k == key {
			return i
		}
	}
	return -1
}

// get returns the value of key (nil when absent) and marks it read.
func (o *obj) get(key string) *node {
	if o == nil {
		return nil
	}
	o.used[key] = true
	if i := o.index(key); i >= 0 {
		return o.n.vals[i]
	}
	return nil
}

// off is the offset of key's value (the object's when absent).
func (o *obj) off(key string) int {
	if i := o.index(key); i >= 0 {
		return o.n.vals[i].off
	}
	return o.n.off
}

func (o *obj) elemOff(key string, i int) int {
	if n := o.get(key); n != nil && n.kind == kArray && i < len(n.vals) {
		return n.vals[i].off
	}
	return o.off(key)
}

// forget marks every key read (no unknown-key warnings).
func (o *obj) forget() {
	for _, k := range o.n.keys {
		o.used[k] = true
	}
}

func (o *obj) missing(key string) {
	o.c.errAt(o.n.off, o.where, "%q is missing", key)
}

// finish warns about every key that is not in known.
func (o *obj) finish(known []string) {
	for i, k := range o.n.keys {
		if o.used[k] || contains(known, k) {
			continue
		}
		o.c.warnAt(o.n.koff[i], o.where, "unknown key %s is ignored%s", quote(k), suggest(k, known))
	}
}

func (o *obj) str(key string) string {
	n := o.get(key)
	if n == nil {
		return ""
	}
	if n.kind != kString {
		o.c.errAt(n.off, o.at(key), "must be a string, not %s", n.kind)
		return ""
	}
	return n.str
}

// req is str that must be present and not empty.
func (o *obj) req(key string) string {
	s := o.str(key)
	if n := o.get(key); n == nil {
		o.missing(key)
	} else if n.kind == kString && strings.TrimSpace(s) == "" {
		o.c.errAt(n.off, o.at(key), "is empty")
	}
	return s
}

// text is str capped at MaxPromptBytes.
func (o *obj) text(key string) string {
	s := o.str(key)
	if len(s) > MaxPromptBytes {
		o.c.errAt(o.off(key), o.at(key), "is %d KB; the most is %d KB (put it in a file)", (len(s)+1023)/1024, MaxPromptBytes>>10)
		return ""
	}
	return s
}

// expr is str capped at MaxExprBytes (the expr package parses it).
func (o *obj) expr(key string) string {
	s := o.str(key)
	if len(s) > MaxExprBytes {
		o.c.errAt(o.off(key), o.at(key), "the expression is %d bytes; the most is %d", len(s), MaxExprBytes)
		return ""
	}
	if n := o.get(key); n != nil && n.kind == kString && strings.TrimSpace(s) == "" {
		o.c.errAt(n.off, o.at(key), "is empty")
	}
	return s
}

// prompt reads prompt / prompt_file (at most one).
func (o *obj) prompt() (string, string) {
	p, f := o.text("prompt"), o.relPath("prompt_file")
	if p != "" && f != "" {
		o.c.errAt(o.n.off, o.where, `has both "prompt" and "prompt_file"; keep one`)
	}
	return p, f
}

// relPath is a path that must stay inside the repository.
func (o *obj) relPath(key string) string {
	s := o.str(key)
	if s == "" {
		if n := o.get(key); n != nil && n.kind == kString {
			o.c.errAt(n.off, o.at(key), "is empty")
		}
		return ""
	}
	if path.IsAbs(s) || strings.HasPrefix(s, "~") || hasDotDot(s) || (len(s) > 1 && s[1] == ':') {
		o.c.errAt(o.off(key), o.at(key), "must be a path inside the workspace (no leading /, ~ or ..)")
		return ""
	}
	return s
}

func (o *obj) boolean(key string) bool {
	n := o.get(key)
	if n == nil {
		return false
	}
	if n.kind != kBool {
		o.c.errAt(n.off, o.at(key), "must be true or false, not %s", n.kind)
		return false
	}
	return n.b
}

func (o *obj) number(key string) (float64, *node, bool) {
	n := o.get(key)
	if n == nil {
		return 0, nil, false
	}
	if n.kind != kNumber {
		o.c.errAt(n.off, o.at(key), "must be a number, not %s", n.kind)
		return 0, n, false
	}
	f, err := n.num.Float64()
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		o.c.errAt(n.off, o.at(key), "is not a usable number")
		return 0, n, false
	}
	return f, n, true
}

func (o *obj) num(key string, lo, hi float64) float64 {
	f, n, ok := o.number(key)
	if !ok {
		return 0
	}
	if f < lo || f > hi {
		o.c.errAt(n.off, o.at(key), "must be from %s to %s", strconv.FormatFloat(lo, 'f', -1, 64), strconv.FormatFloat(hi, 'f', -1, 64))
		return 0
	}
	return f
}

func (o *obj) intIn(key string, lo, hi, def int) int {
	f, n, ok := o.number(key)
	if !ok {
		return def
	}
	if f != math.Trunc(f) || f < float64(lo) || f > float64(hi) {
		o.c.errAt(n.off, o.at(key), "must be a whole number from %d to %d", lo, hi)
		return def
	}
	return int(f)
}

func (o *obj) dur(key string) time.Duration {
	n := o.get(key)
	if n == nil {
		return 0
	}
	s := o.str(key)
	if n.kind != kString {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		o.c.errAt(n.off, o.at(key), `must be a time like "90s", "20m" or "2h"`)
		return 0
	}
	return d
}

// enum is a string from choices; def when absent.
func (o *obj) enum(key, def string, choices ...string) string {
	n := o.get(key)
	if n == nil {
		return def
	}
	s := o.str(key)
	if n.kind != kString {
		return def
	}
	if !contains(choices, s) {
		o.c.errAt(n.off, o.at(key), "%s is not allowed; use one of: %s%s", quote(s), strings.Join(choices, ", "), suggest(s, choices))
		return def
	}
	return s
}

func (o *obj) strList(key string) []string {
	n := o.get(key)
	if n == nil {
		return nil
	}
	if n.kind != kArray {
		o.c.errAt(n.off, o.at(key), "must be a list of strings, not %s", n.kind)
		return nil
	}
	out := make([]string, 0, len(n.vals))
	for i, v := range n.vals {
		if v.kind != kString {
			o.c.errAt(v.off, fmt.Sprintf("%s[%d]", o.at(key), i), "must be a string, not %s", v.kind)
			continue
		}
		out = append(out, v.str)
	}
	return out
}

// argv is a command line as a list. Templates are refused: item text
// must never become a command argument.
func (o *obj) argv(key string) []string {
	n := o.get(key)
	list := o.strList(key)
	if n != nil && n.kind == kArray && len(n.vals) == 0 {
		o.c.errAt(n.off, o.at(key), "is empty; give the program and its arguments")
	}
	for i, a := range list {
		if strings.Contains(a, "{{") {
			o.c.errAt(n.vals[i].off, fmt.Sprintf("%s[%d]", o.at(key), i), "{{…}} templates are not allowed in a command line (item text must never become an argument); pass data in env or a file")
			return nil
		}
	}
	if len(list) > 0 && strings.TrimSpace(list[0]) == "" {
		o.c.errAt(n.vals[0].off, o.at(key)+"[0]", "the program name is empty")
	}
	return list
}

func (o *obj) strMap(key string) map[string]string {
	n := o.get(key)
	if n == nil {
		return nil
	}
	if n.kind != kObject {
		o.c.errAt(n.off, o.at(key), "must be an object of strings, not %s", n.kind)
		return nil
	}
	out := make(map[string]string, len(n.keys))
	for i, k := range n.keys {
		v := n.vals[i]
		if v.kind != kString {
			o.c.errAt(v.off, o.at(key)+"."+k, "must be a string, not %s", v.kind)
			continue
		}
		out[k] = v.str
	}
	return out
}

func (o *obj) anyMap(key string) map[string]any {
	n := o.get(key)
	if n == nil {
		return nil
	}
	if n.kind != kObject {
		o.c.errAt(n.off, o.at(key), "must be an object, not %s", n.kind)
		return nil
	}
	m, _ := n.plain().(map[string]any)
	return m
}

// ── messages ────────────────────────────────────────────────────────────

// quote is %q of a value from the file for a message, cut to 40
// characters. A value that may be a secret is never shown.
func quote(s string) string {
	if looksSecret(s) || strings.Contains(s, "${") {
		return "(value hidden)"
	}
	if utf8.RuneCountInString(s) > 40 {
		r := []rune(s)
		s = string(r[:39]) + "…"
	}
	return strconv.Quote(s)
}

// suggest is ` (did you mean "x"?)` for a close known word, else "".
func suggest(s string, known []string) string {
	if s == "" || len(s) > 40 || looksSecret(s) {
		return ""
	}
	limit := 2 // the most edits a suggestion may be away
	if len(s) <= 4 {
		limit = 1
	}
	best, bestD := "", limit+1
	for _, k := range known {
		if d := editDistance(strings.ToLower(s), k); d < bestD {
			best, bestD = k, d
		}
	}
	if best == "" || best == s {
		return ""
	}
	return fmt.Sprintf(" (did you mean %q?)", best)
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
