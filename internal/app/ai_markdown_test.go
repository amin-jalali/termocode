package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

const mdSample = "## Cause\n\n`Tick` marks it **leaving** but *keeps* it, see [docs](https://x.y).\n\n" +
	"1. first `a = b`, then\n2. second item that is long enough to wrap around the panel width\n" +
	"   - nested bullet\n\n```go\nfunc f() int { return 42 } // answer\n```\n\n> quoted tip\n\n---\n\n```\nplain\n```\n"

func TestChatMarkdownWidthsAndMarkers(t *testing.T) {
	for _, w := range []int{20, 32, 60} {
		lines := renderChatMarkdown(mdSample, w, aiIndent, 3, false)
		var plain []string
		for i, l := range lines {
			if got := lipgloss.Width(l.styled); got != w {
				t.Fatalf("w=%d line %d is %d cells: %q", w, i, got, l.styled)
			}
			plain = append(plain, stripANSI(l.styled))
		}
		all := strings.Join(plain, "\n")
		for _, raw := range []string{"**", "## ", "```", "[docs]"} {
			if strings.Contains(all, raw) {
				t.Errorf("w=%d: raw Markdown %q left in output:\n%s", w, raw, all)
			}
		}
		for _, want := range []string{"Cause", "leaving", "docs", "1.", "answer", "quoted tip"} {
			if !strings.Contains(all, want) {
				t.Errorf("w=%d: missing %q:\n%s", w, want, all)
			}
		}
	}
}

func TestChatMarkdownCodeButtons(t *testing.T) {
	lines := renderChatMarkdown(mdSample, 60, aiIndent, 3, false)
	ids := map[string]bool{}
	for _, l := range lines {
		for _, b := range l.buttons {
			ids[b.id] = true
			if b.x1 <= b.x0 || b.x1 > 60 {
				t.Errorf("bad button span %+v", b)
			}
		}
	}
	for _, id := range []string{"copy:3:0", "apply:3:0", "copy:3:1", "apply:3:1"} {
		if !ids[id] {
			t.Errorf("missing button %s (have %v)", id, ids)
		}
	}
	// An unfinished block while streaming has no buttons yet.
	streaming := renderChatMarkdown("```go\nfunc f(", 60, aiIndent, 0, true)
	for _, l := range streaming {
		if len(l.buttons) > 0 {
			t.Error("streaming code block must not show buttons")
		}
	}
	if code, ok := aiCodeBlock(mdSample, 1); !ok || code != "plain" {
		t.Errorf("second block = %q %v", code, ok)
	}
	if code, ok := aiCodeBlock(mdSample, 0); !ok || !strings.Contains(code, "return 42") {
		t.Errorf("first block = %q", code)
	}
}

func TestChatMarkdownCacheByWidth(t *testing.T) {
	s := &aiState{}
	a := s.renderAnswer(0, mdSample, 40, false, false)
	b := s.renderAnswer(0, mdSample, 40, false, false)
	if &a[0] != &b[0] {
		t.Error("same text + width should hit the cache")
	}
	c := s.renderAnswer(0, mdSample, 50, false, false)
	if lipgloss.Width(c[0].styled) != 50 {
		t.Error("a new width must re-render")
	}
}

func BenchmarkChatMarkdown(b *testing.B) {
	for i := 0; i < b.N; i++ {
		renderChatMarkdown(mdSample, 48, aiIndent, 0, false)
	}
}
