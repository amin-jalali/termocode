package workflow

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite testdata/errors.golden")

// Every file in testdata/good parses with no error and no warning.
func TestParseGoodFiles(t *testing.T) {
	files, _ := filepath.Glob("testdata/good/*.jsonc")
	if len(files) < 3 {
		t.Fatalf("found %d good files", len(files))
	}
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		f, warns, err := Parse(p, data)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		if len(warns) > 0 {
			t.Errorf("%s: warnings %v", p, warns)
		}
		if f.Name != NameFromPath(p) || f.Version != 1 || f.Hash != HashBytes(data) {
			t.Errorf("%s: name %q version %d hash %q", p, f.Name, f.Version, f.Hash)
		}
	}
}

// The plan's full example lands in the typed schema as written.
func TestParseExampleTypes(t *testing.T) {
	data, err := os.ReadFile("testdata/good/weekday-bugs.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	f, _, err := Parse("weekday-bugs.jsonc", data)
	if err != nil {
		t.Fatal(err)
	}
	if f.Schedule == nil || f.Schedule.Preset != "weekdays" || f.Schedule.Time != "09:00" || f.Schedule.TZ != "Europe/Berlin" ||
		strings.Join(f.Schedule.Precheck, " ") != "git fetch --dry-run" {
		t.Errorf("schedule %+v", f.Schedule)
	}
	if f.Vars["label"] != "auto-fix" || f.Vars["base"] != "main" {
		t.Errorf("vars %v", f.Vars)
	}
	want := Limits{BudgetUSD: 5, ItemBudgetUSD: 1.5, Parallel: 2, Timeout: 45 * time.Minute, MaxItems: 10}
	if f.Limits != want {
		t.Errorf("limits %+v, want %+v", f.Limits, want)
	}
	if f.Retries != (Retries{Max: 1, Backoff: 2 * time.Minute}) || f.OnError != OnErrorContinue || !f.ResumeOnStart {
		t.Errorf("retries %+v on_error %q resume %v", f.Retries, f.OnError, f.ResumeOnStart)
	}
	if len(f.Steps) != 5 {
		t.Fatalf("%d top steps", len(f.Steps))
	}
	src := f.Steps[0].Source
	if src == nil || src.From != "mcp" || src.Server != "linear" || src.Tool != "list_issues" || src.Items != "$.issues" ||
		src.Key != "{{item.id}}" || src.MaxItems != 10 || src.Args["label"] != "{{vars.label}}" {
		t.Errorf("source %+v", src)
	}
	if p := f.Steps[1].Plan; p == nil || p.Model != "quick" || p.PromptFile != "prompts/plan-bugs.md" {
		t.Errorf("plan %+v", p)
	}
	if fl := f.Steps[2]; fl.Filter == nil || fl.Filter.Take != 5 || fl.Filter.Sort != "item.plan.rank" || !strings.Contains(fl.When, "seen(item.key)") {
		t.Errorf("filter %+v %+v", fl, fl.Filter)
	}
	each := f.Steps[3].Each
	if each == nil || each.Items != "steps.ready.items" || each.Parallel != 2 || !each.Worktree || each.Base != "{{vars.base}}" ||
		each.Cleanup != "keep" || len(each.Steps) != 5 {
		t.Fatalf("each %+v", each)
	}
	if a := each.Steps[0].Agent; a == nil || a.Agent != "codex" || a.Sandbox != SandboxWorktree {
		t.Errorf("agent %+v", a)
	}
	test := each.Steps[1]
	if test.Tests == nil || test.Tests.Task != "test" || len(test.Else) != 1 || test.Else[0].Write.To != "mcp" {
		t.Errorf("tests %+v", test)
	}
	if pr := each.Steps[2].Write; pr == nil || pr.To != "pr" || !pr.Draft || pr.Gate != GateAllow || pr.BodyFile != "prompts/pr-body.md" {
		t.Errorf("pr %+v", pr)
	}
	if st := each.Steps[4].Write; st == nil || st.Gate != GateAsk || st.Undo == nil || st.Undo.Tool != "update_issue" || st.Undo.Args["state"] != "{{item.state}}" {
		t.Errorf("state %+v", st)
	}
	if m := f.Steps[4].Write; m == nil || m.To != "message" || m.Via != "os" || !strings.HasPrefix(m.Text, "weekday-bugs:") {
		t.Errorf("message %+v", m)
	}
	if s := f.StepByID("sorry"); s == nil || s.Line != 29 {
		t.Errorf("sorry step %+v", s)
	}
	n := 0
	Walk(f.Steps, func(*Step) bool { n++; return true })
	if n != 11 {
		t.Errorf("Walk saw %d steps, want 11", n)
	}
}

// Every bad file gives an error at the right line, and the message
// says what to do.
var badCases = []struct {
	name, src, want string
}{
	{"not json", `{"version": 1,, }`, "line 1: not valid JSON"},
	{"ends early", "{\n\"version\": 1,\n\"steps\": [", "line 3: the file ends too early"},
	{"not an object", `[1]`, "the file must be one { … } object"},
	{"text after", `{"version":1,"name":"a","steps":[{"type":"tests"}]} x`, "more text after the end"},
	{"duplicate key", `{"version":1,"version":1}`, `the key "version" appears twice`},
	{"no version", `{"name":"a","steps":[]}`, `"version" is missing`},
	{"version 2", "{\n  \"version\": 2,\n  \"name\": \"a\"\n}", "line 2: version: this file was made by a newer termocode (version 2)"},
	{"version text", `{"version":"1"}`, "version: must be the number 1"},
	{"version 0", `{"version":0}`, "version: must be the number 1"},
	{"no name", `{"version":1,"steps":[{"type":"tests"}]}`, `"name" is missing`},
	{"bad name", `{"version":1,"name":"Bad Name","steps":[{"type":"tests"}]}`, "name: use a-z"},
	{"name mismatch", `{"version":1,"name":"other","steps":[{"type":"tests"}]}`, `name: is "other" but the file is "bad.jsonc"`},
	{"no steps", `{"version":1,"name":"bad"}`, `"steps" is missing`},
	{"empty steps", `{"version":1,"name":"bad","steps":[]}`, "steps: is empty"},
	{"steps not list", `{"version":1,"name":"bad","steps":{}}`, "steps: must be a list"},
	{"step not object", `{"version":1,"name":"bad","steps":[1]}`, "steps[0]: must be an object { … }, not a number"},
	{"no type", `{"version":1,"name":"bad","steps":[{"id":"x"}]}`, `steps[0] (x): "type" is missing`},
	{"bad type", `{"version":1,"name":"bad","steps":[{"type":"sourse"}]}`, `"sourse" is not a step type; use one of: source, plan, filter, each, ai, agent, pipeline, tests, shell, score, write (did you mean "source"?)`},
	{"bad id", `{"version":1,"name":"bad","steps":[{"id":"X","type":"tests"}]}`, "steps[0].id: use a-z"},
	{"dup id", "{\"version\":1,\"name\":\"bad\",\"steps\":[\n{\"id\":\"t\",\"type\":\"tests\"},\n{\"id\":\"t\",\"type\":\"tests\"}]}", `line 3: steps[1] (t).id: "t" is already the id of the step on line 2`},
	{"two sources", `{"version":1,"name":"bad","steps":[{"type":"source","from":"files","glob":"a"},{"type":"source","from":"files","glob":"b"}]}`, "a second source in one list"},
	{"source no from", `{"version":1,"name":"bad","steps":[{"type":"source"}]}`, `"from" is missing`},
	{"source bad from", `{"version":1,"name":"bad","steps":[{"type":"source","from":"ftp"}]}`, `from: "ftp" is not allowed; use one of: mcp, github, shell, files, http, db`},
	{"mcp no server", `{"version":1,"name":"bad","steps":[{"type":"source","from":"mcp","tool":"t"}]}`, `"server" is missing`},
	{"bad items path", `{"version":1,"name":"bad","steps":[{"type":"source","from":"files","glob":"a","items":"issues"}]}`, `items: must be a path like "$.issues"`},
	{"max items cap", `{"version":1,"name":"bad","steps":[{"type":"source","from":"files","glob":"a","max_items":501}]}`, "max_items: must be a whole number from 1 to 500"},
	{"glob escapes", `{"version":1,"name":"bad","steps":[{"type":"source","from":"files","glob":"../x/*"}]}`, "glob: must stay inside the workspace"},
	{"sql write", `{"version":1,"name":"bad","steps":[{"type":"source","from":"db","connection":"c","sql":"delete from t"}]}`, "sql: a source may only read"},
	{"sql two", `{"version":1,"name":"bad","steps":[{"type":"source","from":"db","connection":"c","sql":"select 1; drop table t"}]}`, "sql: a source may only read"},
	{"github what", `{"version":1,"name":"bad","steps":[{"type":"source","from":"github","what":"repos"}]}`, `what: "repos" is not allowed; use one of: issues, pulls, checks`},
	{"shell parse", `{"version":1,"name":"bad","steps":[{"type":"source","from":"shell","argv":["x"],"parse":"xml"}]}`, `parse: "xml" is not allowed`},
	{"argv template", `{"version":1,"name":"bad","steps":[{"type":"shell","argv":["echo","{{item.title}}"]}]}`, "argv[1]: {{…}} templates are not allowed in a command line"},
	{"precheck template", `{"version":1,"name":"bad","schedule":{"preset":"daily","precheck":["x","{{vars.a}}"]},"steps":[{"type":"tests"}]}`, "schedule.precheck[1]: {{…}} templates are not allowed"},
	{"argv empty", `{"version":1,"name":"bad","steps":[{"type":"shell","argv":[]}]}`, "argv: is empty"},
	{"cwd escapes", `{"version":1,"name":"bad","steps":[{"type":"shell","argv":["x"],"cwd":"/etc"}]}`, "cwd: must be a path inside the workspace"},
	{"prompt both", `{"version":1,"name":"bad","steps":[{"type":"ai","prompt":"a","prompt_file":"b.md"}]}`, `has both "prompt" and "prompt_file"`},
	{"ai nothing", `{"version":1,"name":"bad","steps":[{"type":"ai","model":"quick"}]}`, `needs "prompt" or "prompt_file"`},
	{"ai allow", `{"version":1,"name":"bad","steps":[{"type":"ai","prompt":"x","tools":{"allow":["run_command"]}}]}`, `tools.allow: a workflow can only remove tools; use "deny"`},
	{"prompt file escapes", `{"version":1,"name":"bad","steps":[{"type":"ai","prompt_file":"../../x.md"}]}`, "prompt_file: must be a path inside the workspace"},
	{"agent bad", `{"version":1,"name":"bad","steps":[{"type":"agent","agent":"other","prompt":"x"}]}`, `agent: "other" is not allowed; use one of: codex, claude-code`},
	{"agent sandbox", `{"version":1,"name":"bad","steps":[{"type":"agent","agent":"codex","prompt":"x","sandbox":"none"}]}`, `sandbox: "none" is not allowed; use one of: worktree, read-only`},
	{"tests both", `{"version":1,"name":"bad","steps":[{"type":"tests","task":"t","argv":["go","test"]}]}`, `has both "task" and "argv"`},
	{"filter empty", `{"version":1,"name":"bad","steps":[{"type":"filter"}]}`, `a filter needs "when"`},
	{"each nested deep", `{"version":1,"name":"bad","steps":[{"type":"each","items":"x","steps":[{"type":"each","items":"x","steps":[{"type":"each","items":"x","steps":[{"type":"each","items":"x","steps":[{"type":"tests"}]}]}]}]}]}`, "each is nested 4 deep; the most is 3"},
	{"each no steps", `{"version":1,"name":"bad","steps":[{"type":"each","items":"x"}]}`, `"steps" is missing`},
	{"each unknown ref", `{"version":1,"name":"bad","steps":[{"id":"e","type":"each","items":"steps.nope.items","steps":[{"type":"tests"}]}]}`, `step "e".items: no step has the id "nope"`},
	{"write no to", `{"version":1,"name":"bad","steps":[{"type":"write"}]}`, `"to" is missing`},
	{"write bad gate", `{"version":1,"name":"bad","steps":[{"type":"write","to":"message","via":"os","gate":"always"}]}`, `gate: "always" is not allowed; use one of: ask, allow, dry`},
	{"message mcp channel", `{"version":1,"name":"bad","steps":[{"type":"write","to":"message","via":"slack","text":"x"}]}`, `a message via the MCP server "slack" needs "channel"`},
	{"message via name", `{"version":1,"name":"bad","steps":[{"type":"write","to":"message","via":"my server","channel":"c","text":"x"}]}`, `via: use "os", "web", ${secret:NAME} (a webhook) or an MCP server name`},
	{"message channel os", `{"version":1,"name":"bad","steps":[{"type":"write","to":"message","via":"os","channel":"c","text":"x"}]}`, `"channel" and "tool" are only for a message via an MCP server`},
	{"write pr title", `{"version":1,"name":"bad","steps":[{"type":"write","to":"pr"}]}`, `"title" is missing`},
	{"write comment body", `{"version":1,"name":"bad","steps":[{"type":"write","to":"github.comment","pr":"1"}]}`, `needs "body" or "body_file"`},
	{"write file mode", `{"version":1,"name":"bad","steps":[{"type":"write","to":"file","path":"a.md","mode":"truncate"}]}`, `mode: "truncate" is not allowed`},
	{"write file abs", `{"version":1,"name":"bad","steps":[{"type":"write","to":"file","path":"~/.bashrc"}]}`, "path: must be a path inside the workspace"},
	{"else not list", `{"version":1,"name":"bad","steps":[{"type":"tests","else":{}}]}`, "else: must be a list of steps"},
	{"bad when", `{"version":1,"name":"bad","steps":[{"type":"tests","when":"  "}]}`, "when: is empty"},
	{"long expr", `{"version":1,"name":"bad","steps":[{"type":"tests","when":"` + strings.Repeat("a", 1025) + `"}]}`, "when: the expression is 1025 bytes; the most is 1024"},
	{"long prompt", `{"version":1,"name":"bad","steps":[{"type":"ai","prompt":"` + strings.Repeat("a", 33<<10) + `"}]}`, "prompt: is 33 KB; the most is 32 KB"},
	{"bad duration", `{"version":1,"name":"bad","steps":[{"type":"tests","timeout":"soon"}]}`, `timeout: must be a time like "90s"`},
	{"bad on_error", `{"version":1,"name":"bad","on_error":"retry","steps":[{"type":"tests"}]}`, `on_error: "retry" is not allowed; use one of: continue, stop`},
	{"bad retries", `{"version":1,"name":"bad","retries":{"max":11},"steps":[{"type":"tests"}]}`, "retries.max: must be a whole number from 0 to 10"},
	{"limits parallel", `{"version":1,"name":"bad","limits":{"parallel":1.5},"steps":[{"type":"tests"}]}`, "limits.parallel: must be a whole number from 1 to 16"},
	{"limits budget", `{"version":1,"name":"bad","limits":{"budget_usd":-1},"steps":[{"type":"tests"}]}`, "limits.budget_usd: must be from 0 to 1000000"},
	{"schedule none", `{"version":1,"name":"bad","schedule":{},"steps":[{"type":"tests"}]}`, `schedule: needs "preset"`},
	{"schedule both", `{"version":1,"name":"bad","schedule":{"preset":"daily","cron":"0 9 * * *"},"steps":[{"type":"tests"}]}`, `has both "preset" and "cron"`},
	{"schedule time", `{"version":1,"name":"bad","schedule":{"preset":"daily","time":"9am"},"steps":[{"type":"tests"}]}`, "schedule.time: must be HH:MM"},
	{"schedule cron", `{"version":1,"name":"bad","schedule":{"cron":"0 9 *"},"steps":[{"type":"tests"}]}`, "schedule.cron: must have 5 fields"},
	{"vars object", `{"version":1,"name":"bad","vars":{"a":{}},"steps":[{"type":"tests"}]}`, "vars.a: must be a string, a number or true/false"},
	{"vars name", `{"version":1,"name":"bad","vars":{"A B":1},"steps":[{"type":"tests"}]}`, `vars: the name "A B": use a-z`},
	{"notify bad", `{"version":1,"name":"bad","notify":["mail"],"steps":[{"type":"tests"}]}`, `notify[0]: use "os", "web" or "message:<step id>"`},
	{"notify channel template", `{"version":1,"name":"bad","notify":["message:m"],"steps":[{"id":"m","type":"write","to":"message","via":"slack","channel":"{{vars.ch}}","text":"x"}]}`, `notify[0]: the summary goes to "m" after the run, so its "channel" cannot hold {{…}}; write the channel id`},
	{"notify step", `{"version":1,"name":"bad","notify":["message:t"],"steps":[{"id":"t","type":"tests"}]}`, `notify[0]: "t" is not a message write step`},
	{"too many steps", `{"version":1,"name":"bad","steps":[` + strings.TrimSuffix(strings.Repeat(`{"type":"tests"},`, 41), ",") + `]}`, "steps: 41 steps in all; the most is 40"},
	{"wrong kind", `{"version":1,"name":"bad","title":3,"steps":[{"type":"tests"}]}`, "title: must be a string, not a number"},
	{"too big", `{"version":1,"name":"bad","description":"` + strings.Repeat("a", MaxFileBytes) + `"}`, "the file is 257 KB; the most is 256 KB"},
	{"not utf8", "{\"version\":1,\"name\":\"\xff\"}", "the file is not UTF-8 text"},
	{"deep nesting", `{"version":1,"vars":` + strings.Repeat("[", 70) + strings.Repeat("]", 70) + `}`, "nested more than 64 levels deep"},

	// Secrets (plan §6.2). The value is never in the message.
	{"cmd ref", `{"version":1,"name":"bad","steps":[{"type":"shell","argv":["x"],"env":{"T":"${cmd:pass show x}"}}]}`, "steps[0].env.T: ${cmd:} is not allowed in a workflow file"},
	{"env ref", `{"version":1,"name":"bad","steps":[{"type":"source","from":"http","request":"a.http","headers":{"A":"${env:HOME}"}}]}`, "steps[0].headers.A: ${env:} and ${dotenv:} are not allowed"},
	{"secret in prompt", `{"version":1,"name":"bad","steps":[{"type":"ai","prompt":"use ${secret:TOKEN}"}]}`, "steps[0].prompt: ${secret:} is allowed only in headers, env and via"},
	{"secret in args", `{"version":1,"name":"bad","steps":[{"type":"write","to":"mcp","server":"s","tool":"t","args":{"headers":{"a":"${secret:X}"}}}]}`, "steps[0].args.headers.a: ${secret:} is allowed only in headers, env and via"},
	{"secret in title", `{"version":1,"name":"bad","steps":[{"type":"write","to":"pr","title":"${secret:X}"}]}`, "steps[0].title: ${secret:} is allowed only"},
	{"secret bad name", `{"version":1,"name":"bad","steps":[{"type":"shell","argv":["x"],"env":{"T":"${secret:a b}"}}]}`, "${secret:NAME} needs a secret name"},
	{"literal gh token", `{"version":1,"name":"bad","steps":[{"type":"source","from":"http","request":"a.http","headers":{"Authorization":"token ` + "ghp_" + "abcdefghijklmnopqrstuvwxyz0123" + `"}}]}`, "steps[0].headers.Authorization: looks like a key or token"},
	{"literal sk in body", `{"version":1,"name":"bad","steps":[{"type":"write","to":"message","via":"os","text":"key ` + "sk-" + "test0123456789abcdefghij" + `"}]}`, "steps[0].text: looks like a key or token"},
	{"literal bearer", `{"version":1,"name":"bad","vars":{"t":"Bearer ` + "abcdefghij" + "0123456789xyz" + `"},"steps":[{"type":"tests"}]}`, "vars.t: looks like a key or token"},
	{"literal in key", `{"version":1,"name":"bad","vars":{"` + "sk-" + "test0123456789abcdefghij" + `":1},"steps":[{"type":"tests"}]}`, "looks like a key or token"},
}

func TestParseBadFiles(t *testing.T) {
	for _, c := range badCases {
		_, _, err := Parse("bad.jsonc", []byte(c.src))
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("%s: err %v, want a *ParseError", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s:\n got  %s\n want %s", c.name, err, c.want)
		}
		if len(err.Error()) > MaxErrorChars {
			t.Errorf("%s: error is %d chars", c.name, len(err.Error()))
		}
		for _, leak := range []string{"ghp_abc", "sk-test", "pass show", "Bearer abc"} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("%s: the error echoes a secret: %s", c.name, err)
			}
		}
	}
}

// The messages are part of the UI: a golden keeps them stable and
// readable. go test -run TestParseErrorGolden -update rewrites it.
func TestParseErrorGolden(t *testing.T) {
	var b strings.Builder
	for _, c := range badCases {
		_, _, err := Parse("bad.jsonc", []byte(c.src))
		b.WriteString(c.name + ":\n  " + err.Error() + "\n")
	}
	const golden = "testdata/errors.golden"
	if *update {
		if err := os.WriteFile(golden, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if b.String() != string(want) {
		t.Errorf("messages changed; run go test -run TestParseErrorGolden -update and review the diff\n%s", b.String())
	}
}

// Unknown keys are warned with the line and a near key, never fatal
// (ADR 0005). Comments and trailing commas keep the line numbers right.
func TestParseUnknownKeysWarn(t *testing.T) {
	src := `// a comment
{
  /* a block
     comment */
  "version": 1,
  "name": "warn",
  "colour": "red",
  "steps": [
    { "type": "ai", "promt": "x", "prompt": "y" },
    { "type": "write", "to": "message", "via": "os", "gat": "ask", "retries": { "max": 1, "bakoff": "1m" } },
  ],
}`
	f, warns, err := Parse("warn.jsonc", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, w := range warns {
		got = append(got, w.String())
	}
	want := []string{
		`line 9: steps[0]: unknown key "promt" is ignored (did you mean "prompt"?)`,
		`line 10: steps[1].retries: unknown key "bakoff" is ignored (did you mean "backoff"?)`,
		`line 10: steps[1]: unknown key "gat" is ignored (did you mean "gate"?)`,
		`line 7: unknown key "colour" is ignored`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("warnings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if f.Steps[0].AI.Prompt != "y" || f.Steps[1].Write.Gate != GateAsk {
		t.Errorf("steps %+v %+v", f.Steps[0].AI, f.Steps[1].Write)
	}
}

func TestParseDefaults(t *testing.T) {
	f, _, err := Parse("", []byte(`{"version":1,"name":"d","steps":[{"type":"source","from":"files","glob":"*.md"},{"type":"write","to":"file","path":"a.md"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Limits.MaxItems != DefaultMaxItems || f.Steps[0].Source.MaxItems != DefaultMaxItems || f.OnError != OnErrorContinue || !f.ResumeOnStart {
		t.Errorf("defaults %+v", f)
	}
	if w := f.Steps[1].Write; w.Gate != GateAsk || w.Mode != "create" {
		t.Errorf("write defaults %+v", w)
	}
}

// Spec (WFX-18): a message via an MCP server takes a channel (a
// template) and an optional send tool.
func TestMessageViaServerParses(t *testing.T) {
	f, warns, err := Parse("m.jsonc", []byte(`{"version":1,"name":"m","vars":{"ch":"C0123"},"steps":[
	  {"id":"sum","type":"write","to":"message","via":"slack","channel":"{{vars.ch}}","tool":"slack_send_message","text":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings %+v", warns)
	}
	w := f.Steps[0].Write
	if w.Via != "slack" || w.Channel != "{{vars.ch}}" || w.Tool != "slack_send_message" {
		t.Fatalf("write %+v", w)
	}
}
