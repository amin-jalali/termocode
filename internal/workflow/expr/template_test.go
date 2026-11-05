package expr

import (
	"reflect"
	"strings"
	"testing"
)

func TestRenderText(t *testing.T) {
	cases := []struct{ tmpl, want string }{
		{`plain text`, `plain text`},
		{``, ``},
		{`fix: {{item.title}}`, `fix: Crash on save`},
		{`{{item.id}}/{{vars.base}}`, `LIN-228/main`},
		{`n={{ vars.n }}`, `n=3`},
		{`{{item.prio + 0.5}}`, `2.5`},
		{`[{{item.nope}}]`, `[]`}, // null is empty text
		{`{{item.labels}}`, `["bug","ui"]`},
		{`{{run.dry}}`, `true`},
		{`{{"}}"}} and {{'{{'}}`, `}} and {{`}, // braces inside strings
		{`a }} b`, `a }} b`},                   // a lone close is text
		{`{{ item.id matches "^LIN" }}`, `true`},
	}
	for _, c := range cases {
		got, err := Render(c.tmpl, testScope(), nil)
		if err != nil {
			t.Errorf("Render(%q): %v", c.tmpl, err)
			continue
		}
		if got != c.want {
			t.Errorf("Render(%q) = %q, want %q", c.tmpl, got, c.want)
		}
	}
}

func TestRenderValueKeepsJSONType(t *testing.T) {
	cases := []struct {
		tmpl string
		want any
	}{
		{`{{item.prio}}`, float64(2)},
		{`{{item.labels}}`, []any{"bug", "ui"}},
		{`{{item.plan.skip}}`, false},
		{`{{item.nope}}`, nil},
		{`{{ item.plan }}`, map[string]any{"skip": false, "rank": float64(1)}},
		{`#{{item.prio}}`, "#2"}, // not the whole string: text
		{` {{item.prio}}`, " 2"}, // outer space counts as text
		{`{{item.prio}}{{item.prio}}`, "22"},
		{`no template`, "no template"},
	}
	for _, c := range cases {
		got, err := RenderValue(c.tmpl, testScope(), nil)
		if err != nil {
			t.Errorf("RenderValue(%q): %v", c.tmpl, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("RenderValue(%q) = %#v, want %#v", c.tmpl, got, c.want)
		}
	}
}

func TestRenderJSONWalksArgs(t *testing.T) {
	args := map[string]any{
		"issueId": "{{item.id}}",
		"count":   "{{item.prio}}",
		"labels":  "{{item.labels}}",
		"body":    "Draft PR for {{item.title}}",
		"fixed":   float64(7),
		"nested":  []any{"{{run.dry}}", map[string]any{"k": "{{vars.base}}"}},
		"{{key}}": "keys are never templates",
	}
	got, err := RenderJSON(args, testScope(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"issueId": "LIN-228",
		"count":   float64(2),
		"labels":  []any{"bug", "ui"},
		"body":    "Draft PR for Crash on save",
		"fixed":   float64(7),
		"nested":  []any{true, map[string]any{"k": "main"}},
		"{{key}}": "keys are never templates",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RenderJSON = %#v\nwant %#v", got, want)
	}
	if args["issueId"] != "{{item.id}}" {
		t.Fatal("RenderJSON changed its input")
	}
}

func TestTemplateErrors(t *testing.T) {
	cases := []struct{ tmpl, wantErr string }{
		{`{{item.id`, "not closed"},
		{`a {{ "x }}`, "not closed"},
		{`{{}}`, "empty"},
		{`{{ foo }}`, "unknown name"},
		{`x {{1 < "a"}}`, "cannot compare"},
		{`{{` + strings.Repeat("1+", MaxExprLen) + `1}}`, "too long"},
		{strings.Repeat("x", MaxTemplateLen+1), "template is too long"},
	}
	for _, c := range cases {
		_, err := Render(c.tmpl, testScope(), nil)
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("Render(%.40q) err = %v, want %q", c.tmpl, err, c.wantErr)
		}
		if len(err.Error()) > 300 {
			t.Errorf("Render(%.40q) error is %d chars", c.tmpl, len(err.Error()))
		}
	}
}

func TestCheckFindsErrorsWithoutAScope(t *testing.T) {
	if err := Check(`item.a == 1 and len(item.b) > 0`); err != nil {
		t.Fatalf("Check good: %v", err)
	}
	if err := Check(`item.a ==`); err == nil {
		t.Fatal("Check bad: no error")
	}
	if err := CheckTemplate(`hi {{item.a}} and {{len(item.b)}}`); err != nil {
		t.Fatalf("CheckTemplate good: %v", err)
	}
	for _, bad := range []string{`hi {{item.a`, `{{ env.HOME }}`} {
		if err := CheckTemplate(bad); err == nil {
			t.Errorf("CheckTemplate(%q): no error", bad)
		}
	}
	if !HasTemplate(`a {{b}}`) || HasTemplate(`a { b }`) {
		t.Fatal("HasTemplate is wrong")
	}
}
