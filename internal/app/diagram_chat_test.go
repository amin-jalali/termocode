package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (CMP-70): a finished mermaid block in the chat is drawn as a
// diagram; while it still streams the source shows.
func TestChatDrawsDiagrams(t *testing.T) {
	text := "Plan:\n\n```mermaid\ngraph TD\nA[Build] --> B[Ship]\n```\n"
	var b strings.Builder
	for _, l := range renderChatMarkdown(text, 80, aiIndent, 0, false) {
		b.WriteString(ansi.Strip(l.styled) + "\n")
	}
	got := b.String()
	if !strings.Contains(got, "▼") || !strings.Contains(got, "mermaid · diagram") || strings.Contains(got, "A[Build]") {
		t.Fatalf("not drawn:\n%s", got)
	}
	b.Reset()
	for _, l := range renderChatMarkdown("```mermaid\ngraph TD\nA[Build] --> B[Ship]\n", 80, aiIndent, 0, true) {
		b.WriteString(ansi.Strip(l.styled) + "\n")
	}
	if !strings.Contains(b.String(), "A[Build]") {
		t.Fatalf("a streaming block should show its source:\n%s", b.String())
	}
}

// Spec (CMP-70): the page gets the drawn diagram next to the source of a
// diagram block, and nothing extra for other code.
func TestWebMarkdownDiagram(t *testing.T) {
	n := webMarkdownParse("```csv\na,b\n1,2\n```\n\n```go\nx := 1\n```\n")
	if len(n.C) != 2 {
		t.Fatalf("nodes: %+v", n.C)
	}
	if c := n.C[0]; c.K != webproto.MdCodeBlock || c.Lang != "csv" || !strings.Contains(c.Diagram, "┌───┬───┐") || strings.TrimSuffix(c.Text, "\n") != "a,b\n1,2" {
		t.Fatalf("csv block: %+v", c)
	}
	if n.C[1].Diagram != "" {
		t.Fatalf("go block got a diagram: %q", n.C[1].Diagram)
	}
}
