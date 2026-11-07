package workflow

import (
	"strings"
	"testing"
)

// CMP-38: "on" lists the triggers of a workflow: github events (polled
// through gh) and inbound webhooks. Ask is on unless the file says
// otherwise; a mention looks for @termocode by default.
func TestParseTriggers(t *testing.T) {
	f, warns, err := Parse("t.jsonc", []byte(`{"version":1,"name":"t","on":[
	  {"github":"issue_labeled","labels":["agent"],"worktree":true,"base":"main"},
	  {"github":"mention"},
	  {"github":"ci_failed","branch":"main","ask":false},
	  {"webhook":"deploy","from":"slack"},
	  {"webhook":"any"}
	],"steps":[{"type":"source","from":"files","glob":"*.md"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings %+v", warns)
	}
	if len(f.Triggers) != 5 {
		t.Fatalf("triggers %+v", f.Triggers)
	}
	tr := f.Triggers
	if tr[0].GitHub != "issue_labeled" || tr[0].Labels[0] != "agent" || !tr[0].Worktree || tr[0].Base != "main" || !tr[0].Ask {
		t.Errorf("labeled %+v", tr[0])
	}
	if tr[1].Text != "@termocode" {
		t.Errorf("mention text %q", tr[1].Text)
	}
	if tr[2].Ask || tr[2].Branch != "main" {
		t.Errorf("ci %+v", tr[2])
	}
	if tr[3].Webhook != "deploy" || tr[3].From != "slack" || tr[4].From != "generic" {
		t.Errorf("hooks %+v %+v", tr[3], tr[4])
	}
	if tr[0].Line != 2 {
		t.Errorf("line %d", tr[0].Line)
	}
}

func TestParseTriggerErrors(t *testing.T) {
	for _, tc := range []struct{ on, want string }{
		{`{}`, `needs "github"`},
		{`{"github":"push"}`, `"push" is not allowed`},
		{`{"github":"pr_opened","webhook":"x"}`, `has both`},
		{`{"github":"issue_labeled"}`, `needs "labels"`},
		{`{"webhook":"Bad Name"}`, `use a-z`},
		{`{"github":"pr_opened","repo":"nope"}`, `owner/name`},
		{`{"webhook":"a"},{"webhook":"a"}`, `used twice`},
		{`{"webhook":"a","from":"teams"}`, `"teams" is not allowed`},
	} {
		_, _, err := Parse("t.jsonc", []byte(`{"version":1,"name":"t","on":[`+tc.on+`],"steps":[{"type":"source","from":"files","glob":"*.md"}]}`))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err %v, want %q", tc.on, err, tc.want)
		}
	}
	_, _, err := Parse("t.jsonc", []byte(`{"version":1,"name":"t","on":{"github":"mention"},"steps":[{"type":"source","from":"files","glob":"*.md"}]}`))
	if err == nil || !strings.Contains(err.Error(), "must be a list") {
		t.Errorf("object on: %v", err)
	}
}

// A field of the other kind is only a warning; the hook secret never
// sits in the file, so it has no key for it.
func TestParseTriggerWarnings(t *testing.T) {
	_, warns, err := Parse("t.jsonc", []byte(`{"version":1,"name":"t","on":[
	  {"webhook":"a","labels":["x"]},{"github":"pr_opened","from":"slack"},{"github":"pr_opened","base":"main"},{"webhook":"b","secret":"x"}
	],"steps":[{"type":"source","from":"files","glob":"*.md"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	got := ""
	for _, w := range warns {
		got += w.String() + "\n"
	}
	for _, want := range []string{"on[0].labels", "on[1].from", "on[2].base", `unknown key "secret"`} {
		if !strings.Contains(got, want) {
			t.Errorf("no warning %q in:\n%s", want, got)
		}
	}
	if WebhookSecretName("a") != "workflow-hook.a" {
		t.Error(WebhookSecretName("a"))
	}
}
