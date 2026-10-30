package webhl

import (
	"fmt"
	"strings"
	"testing"
)

func symText(path string, rows []string) string {
	syms, _, ok := Symbols(path, "", rows, 100)
	if !ok {
		return "no lexer"
	}
	var out []string
	for _, s := range syms {
		out = append(out, fmt.Sprintf("%s:%d:%d:%d:%d", s.Name, s.Kind, s.Line, s.Col, s.Depth))
	}
	return strings.Join(out, " ")
}

// Without a language server the outline is a guess from declarations:
// the name after func/def/class/…, its line and UTF-16 column, nested
// one level when indented; calls and variables are not symbols.
func TestSymbolsFromDeclarations(t *testing.T) {
	goSrc := []string{
		"package a",
		"",
		"type Box struct{ n int }",
		"",
		"func (b *Box) Size() int { return len(\"é\") }",
		"",
		"func main() {",
		"\tfmt.Println(1)",
		"}",
	}
	if got, want := symText("a.go", goSrc), "Box:5:3:6:0 Size:6:5:15:0 main:12:7:6:0"; got != want {
		t.Errorf("go: %s, want %s", got, want)
	}
	py := []string{
		"class Shop:",
		"    def buy(self):",
		"        print(1)",
		"",
		"def main():",
		"    pass",
	}
	if got, want := symText("shop.py", py), "Shop:5:1:7:0 buy:6:2:9:1 main:12:5:5:0"; got != want {
		t.Errorf("python: %s, want %s", got, want)
	}
	if got := symText("notes.unknownext", []string{"def x"}); got != "no lexer" {
		t.Errorf("unknown file: %s", got)
	}
	many := make([]string, 50)
	for i := range many {
		many[i] = fmt.Sprintf("def f%d(): pass", i)
	}
	syms, cut, _ := Symbols("m.py", "", many, 10)
	if len(syms) != 10 || !cut {
		t.Errorf("cap: %d symbols cut %v", len(syms), cut)
	}
}
