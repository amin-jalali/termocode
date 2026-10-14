package ai

import (
	"strings"
	"testing"
)

// ctxAt builds an InlineContext from a buffer with "|" at the caret.
func ctxAt(ft, buf string) InlineContext {
	i := strings.Index(buf, "|")
	before, after := buf[:i], buf[i+1:]
	lb := before
	if j := strings.LastIndexByte(before, '\n'); j >= 0 {
		lb = before[j+1:]
	}
	la := after
	if j := strings.IndexByte(after, '\n'); j >= 0 {
		la = after[:j]
	}
	return InlineContext{Filetype: ft, Before: before, After: after, LineBefore: lb, LineAfter: la}
}

func TestShouldAutoTrigger(t *testing.T) {
	cases := []struct {
		name string
		ft   string
		buf  string
		want bool
	}{
		{"empty buffer", "go", "|", false},
		{"end of line", "go", "func main() {\n\tfmt.Pri|\n}", true},
		{"after dot", "go", "func main() {\n\tfmt.|\n}", true},
		{"mid-word", "go", "func main() {\n\tfmt.Pr|intln(x)\n}", false},
		{"code to the right", "go", "func main() {\n\tx := |foo(bar)\n}", false},
		{"only closers to the right", "go", "func main() {\n\tfmt.Println(|)\n}", true},
		{"blank line after closing brace", "go", "func a() {\n}\n|", false},
		{"blank top-level line", "go", "package main\n\n|", false},
		{"blank line in body after opener", "go", "func a() {\n\t|\n}", true},
		{"blank line in body after statement", "go", "func a() {\n\tx := 1\n\t|\n}", true},
		{"blank line after nested closing brace", "go", "func a() {\n\tif x {\n\t\ty()\n\t}\n\t|\n}", false},
		{"second blank line", "go", "func a() {\n\tx := 1\n\n\t|\n}", false},
		{"line comment", "go", "func a() {\n\t// compute the|\n}", false},
		{"trailing comment", "go", "func a() {\n\tx := 1 // the|\n}", false},
		{"url in string is not a comment", "go", "func a() {\n\tu := \"http://|\"\n}", true},
		{"python comment", "python", "def f():\n    # todo|", false},
		{"python body line", "python", "def f():\n    x = compu|", true},
		{"lua end closes scope", "lua", "local function f()\nend\n|", false},
	}
	for _, c := range cases {
		got, why := ShouldAutoTrigger(ctxAt(c.ft, c.buf))
		if got != c.want {
			t.Errorf("%s: got %v (%s) want %v", c.name, got, why, c.want)
		}
	}
}

func TestNeedsLongerPause(t *testing.T) {
	if !NeedsLongerPause(ctxAt("go", "func a() {\n\t|\n}")) {
		t.Error("blank line should wait for a longer pause")
	}
	if NeedsLongerPause(ctxAt("go", "func a() {\n\tfmt.|\n}")) {
		t.Error("typing on a line should not wait longer")
	}
}

// Recorded fake model answers (the kind of output the user complained
// about) and what may reach the screen.
func TestFilterInlineCompletion(t *testing.T) {
	cases := []struct {
		name   string
		ft     string
		buf    string
		model  string
		manual bool
		want   string // "" = dropped
	}{
		{"plain continuation kept", "go", "func a() {\n\tfmt.Pri|\n}", `Println(x)`, false, `Println(x)`},
		{"rewrite kept", "go", "func a() {\n\tprint|\n}", `fmt.Println("hi")`, false, `fmt.Println("hi")`},
		{"new function after statement is cut", "go", "func main() {\n\tx := 1\n\t|\n}",
			"fmt.Println(x)\n}\n\nfunc helper() {\n\treturn\n}", false, "fmt.Println(x)"},
		{"new function on blank line dropped", "go", "func main() {\n\tx := 1\n\t|\n}",
			"func helper() int {\n\treturn 1\n}", false, ""},
		{"new function on blank line kept when asked", "go", "func main() {\n\tx := 1\n\t|\n}",
			"func helper() int {\n\t\treturn 1\n\t}", true, "func helper() int {\n\t\treturn 1\n\t}"},
		{"method inside go body dropped", "go", "func main() {\n\t|\n}",
			"func (s *Server) Start() error {\n\treturn nil\n}", false, ""},
		{"python def dropped", "python", "class A:\n    def f(self):\n        return 1\n    |",
			"def g(self):\n        return 2", false, ""},
		{"user's own declaration kept, next one cut", "go", "func add(a, b int) int|\n",
			" {\n\treturn a + b\n}\n\nfunc sub(a, b int) int {\n\treturn a - b\n}", false, " {\n\treturn a + b\n}"},
		{"mid-line keeps one line", "go", "func a() {\n\tx := compute(|\n}",
			"a, b)\n\ty := x + 1\n\tz := y", false, "a, b)"},
		{"block opener keeps body", "go", "func a() error {\n\tif err != nil|\n}",
			" {\n\t\treturn err\n\t}", false, " {\n\t\treturn err\n\t}"},
		{"repeats the next line", "go", "func a() int {\n\t|\n\treturn x\n}", "return x", false, ""},
		{"repeats the previous line", "go", "func a() {\n\tfmt.Println(a)\n\t|\n}", "fmt.Println(a)", false, ""},
		{"only a closer", "go", "func a() {\n\tfoo(|\n}", ")", false, ""},
		{"trailing lines duplicate the suffix", "go", "func a() {\n\tx := 1\n\t|\n\treturn x\n}",
			"y := x * 2\n\treturn x\n}", false, "y := x * 2"},
		{"long body capped", "go", "func a() {\n\t|\n}",
			strings.Repeat("x++\n\t", 20) + "x++", false, strings.TrimSuffix(strings.Repeat("x++\n\t", InlineMaxLines), "\n\t")},
	}
	for _, c := range cases {
		got, ok := FilterInlineCompletion(ctxAt(c.ft, c.buf), c.model, c.manual)
		if c.want == "" {
			if ok {
				t.Errorf("%s: want dropped, got %q", c.name, got)
			}
			continue
		}
		if !ok || got != c.want {
			t.Errorf("%s: got %q ok=%v want %q", c.name, got, ok, c.want)
		}
	}
}

// The system prompt must steer away from invented code.
func TestInlinePromptPolicy(t *testing.T) {
	p := BuildInlinePrompt("go", "/x/main.go", "x", "")[0].Content
	for _, want := range []string{"Never start a new function", "already after <CURSOR>", "output nothing"} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt lacks %q", want)
		}
	}
	if strings.Contains(p, "finish the current statement or block") {
		t.Error("system prompt still invites whole blocks")
	}
}

func TestInlineCacheTypeThrough(t *testing.T) {
	var c InlineCache
	c.Remember(1, 3, "\tfmt.Pri", "ntln(x)")
	if rest, ok := c.Match(1, 3, "\tfmt.Prin"); !ok || rest != "tln(x)" {
		t.Fatalf("type-through: %q %v", rest, ok)
	}
	if _, ok := c.Match(1, 3, "\tfmt.Prinz"); ok {
		t.Fatal("typed a different char: must miss")
	}
	if _, ok := c.Match(1, 4, "\tfmt.Prin"); ok {
		t.Fatal("other row: must miss")
	}
	if _, ok := c.Match(1, 3, "\tfmt.Println(x)"); ok {
		t.Fatal("fully typed: nothing left to show")
	}
	c.Remember(1, 3, "\tfmt.Prin", "tln(x)")
	c.Reset()
	if _, ok := c.Match(1, 3, "\tfmt.Print"); ok {
		t.Fatal("reset cache must miss")
	}
}
