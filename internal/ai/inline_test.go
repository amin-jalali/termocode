package ai

import (
	"reflect"
	"strings"
	"testing"
)

// Port of mobocode test/inline_completion_test.dart.

func TestParseInlineResponse(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		ok      bool
		text    string
		imports []string
	}{
		{"plain", `print("hi");`, true, `print("hi");`, nil},
		{"fence with lang", "```dart\nfinal x = 1;\n```", true, "final x = 1;", nil},
		{"fence no lang", "```\nfoo()\n```", true, "foo()", nil},
		{"missing closing fence", "```dart\nfoo()", true, "foo()", nil},
		{"import line", "IMPORT: import 'dart:math';\nsqrt(2);", true, "sqrt(2);", []string{"import 'dart:math';"}},
		{"import in fence", "```dart\nIMPORT: import 'dart:math';\nsqrt(2);\n```", true, "sqrt(2);", []string{"import 'dart:math';"}},
		{"several imports", "IMPORT: import \"fmt\"\nIMPORT: import \"os\"\nfmt.Println(os.Args)", true, "fmt.Println(os.Args)", []string{`import "fmt"`, `import "os"`}},
		{"no space after IMPORT:", "IMPORT:import \"fmt\"\nfmt.Println()", true, "fmt.Println()", []string{`import "fmt"`}},
		{"dedupe imports", "IMPORT: import \"fmt\"\nIMPORT: import \"fmt\"\nfmt.Println()", true, "fmt.Println()", []string{`import "fmt"`}},
		{"empty", "", false, "", nil},
		{"whitespace", "   \n\t  ", false, "", nil},
		{"import only", "IMPORT: import 'dart:math';", false, "", nil},
		{"empty fence", "```\n```", false, "", nil},
		{"multi-line", "foo();\nbar();\nbaz();", true, "foo();\nbar();\nbaz();", nil},
		{"multi-line fenced with import", "```dart\nIMPORT: import 'dart:math';\nfinal a = 1;\nfinal b = 2;\n```", true, "final a = 1;\nfinal b = 2;", []string{"import 'dart:math';"}},
	}
	for _, c := range cases {
		got, ok := ParseInlineResponse(c.raw)
		if ok != c.ok {
			t.Errorf("%s: ok=%v want %v", c.name, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if strings.TrimSpace(got.InsertText) != c.text {
			t.Errorf("%s: text=%q want %q", c.name, got.InsertText, c.text)
		}
		if !reflect.DeepEqual(got.ImportLines, c.imports) {
			t.Errorf("%s: imports=%v want %v", c.name, got.ImportLines, c.imports)
		}
	}
}

// apply runs the exact transform the editor performs on Tab.
func apply(before, after, completion string) string {
	r := ResolveInlineCompletion(before, after, completion)
	return before[:len(before)-r.DeleteBefore] + r.Insert
}

func TestResolveInlineCompletion(t *testing.T) {
	if got := apply("  print", "", `fmt.Println("Hello, World!")`); got != `  fmt.Println("Hello, World!")` {
		t.Errorf("rewrite print: %q", got)
	}
	if got := apply("  print salam", "", `fmt.Println("salam")`); got != `  fmt.Println("salam")` {
		t.Errorf("rewrite with space: %q", got)
	}
	if got := apply("fmt.Pri", "", `Println("x")`); got != `fmt.Println("x")` {
		t.Errorf("continuation suffix: %q", got)
	}
	if got := apply("fmt.Pri", "", `fmt.Println("x")`); got != `fmt.Println("x")` {
		t.Errorf("continuation whole token: %q", got)
	}
	if r := ResolveInlineCompletion("print", ")", `fmt.Println("x"))`); r.Insert != `fmt.Println("x")` {
		t.Errorf("trailing overlap: %q", r.Insert)
	}
	if r := ResolveInlineCompletion("  ", "", "x := 1"); r.DeleteBefore != 0 || r.Insert != "x := 1" {
		t.Errorf("blank line: %+v", r)
	}
	if r := ResolveInlineCompletion("\t\tprint salam", "", `fmt.Println("salam")`); r.DeleteBefore != len("print salam") {
		t.Errorf("rewrite keeps indent: %+v", r)
	}
	// Multi-byte text before the caret never splits a rune.
	if got := apply("s := \"héllo", "", "\"héllo wörld\""); got != "s := \"héllo wörld\"" {
		t.Errorf("utf8 continuation: %q", got)
	}
}

func TestCapContextAndPrompt(t *testing.T) {
	before := strings.Repeat("line\n", 3000) + "foo"
	after := strings.Repeat("x\n", 3000)
	b, a := CapContext(before, after, 100, 50)
	if len(b) > 100 || !strings.HasSuffix(b, "foo") || len(a) > 50 {
		t.Fatalf("cap failed: %d %d", len(b), len(a))
	}
	msgs := BuildInlinePrompt("go", "/x/main.go", "fmt.Pri", "\n}")
	if len(msgs) != 2 || msgs[0].Role != RoleSystem || !strings.Contains(msgs[1].Content, "fmt.Pri<CURSOR>\n}") || !strings.Contains(msgs[1].Content, "main.go (language: go)") {
		t.Fatalf("prompt: %+v", msgs)
	}
}

func TestImportInsertion(t *testing.T) {
	goSrc := []string{"// c", "package main", "", `import "os"`, "", "func main() {}"}
	at, add := ImportInsertion(goSrc, "go", []string{`import "fmt"`, `import "os"`})
	if at != 2 || !reflect.DeepEqual(add, []string{"", `import "fmt"`}) {
		t.Errorf("go: at=%d add=%q", at, add)
	}
	block := []string{"package main", "import (", `	"fmt"`, ")"}
	if _, add := ImportInsertion(block, "go", []string{`import "fmt"`}); add != nil {
		t.Errorf("go block dedupe: %q", add)
	}
	py := []string{"#!/usr/bin/env python", "x = 1"}
	if at, add := ImportInsertion(py, "python", []string{"import os"}); at != 1 || len(add) != 1 {
		t.Errorf("python: %d %q", at, add)
	}
}
