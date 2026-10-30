package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// webMdKinds are the node kinds the page knows.
var webMdKinds = map[string]bool{"doc": true, "p": true, "h": true, "em": true, "strong": true, "code": true,
	"codeblock": true, "ul": true, "ol": true, "li": true, "blockquote": true, "link": true, "hr": true,
	"br": true, "table": true, "th": true, "td": true, "text": true}

// webMdCheck walks an AST: only allowed kinds, links http(s) only, no
// HTML tag or control character in any string, depth capped.
func webMdCheck(t testing.TB, n webproto.MdNode, depth int) {
	t.Helper()
	if !webMdKinds[n.K] {
		t.Fatalf("kind %q is not allowed", n.K)
	}
	if depth > webproto.MdMaxDepth+2 {
		t.Fatalf("depth %d", depth)
	}
	if n.K == "link" && !(strings.HasPrefix(n.Href, "http://") || strings.HasPrefix(n.Href, "https://")) {
		t.Fatalf("link href %q", n.Href)
	}
	if n.K != "link" && n.Href != "" {
		t.Fatalf("%s has an href", n.K)
	}
	for _, s := range []string{n.Text, n.Href, n.Lang} {
		for _, r := range s {
			if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f || (r >= 0x80 && r <= 0x9f) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
				t.Fatalf("control %U in %q", r, s)
			}
		}
	}
	for _, c := range n.C {
		webMdCheck(t, c, depth+1)
	}
	for _, row := range n.Rows {
		for _, c := range row {
			webMdCheck(t, c, depth+1)
		}
	}
}

// Spec (WEB-13): the markdown AST goldens: every input in
// testdata/web/md/*.md converts to its .json (UPDATE_GOLDEN=1 rewrites).
func TestMarkdownASTGoldens(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "web", "md", "*.md"))
	if len(files) < 2 {
		t.Fatalf("goldens: %v", files)
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		n := webMarkdownParse(string(src))
		webMdCheck(t, *n, 0)
		got, _ := json.MarshalIndent(n, "", "  ")
		got = append(got, '\n')
		golden := strings.TrimSuffix(f, ".md") + ".json"
		if os.Getenv("UPDATE_GOLDEN") == "1" {
			_ = os.WriteFile(golden, got, 0o644)
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("%v (UPDATE_GOLDEN=1 writes it)", err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s:\n%s", golden, got)
		}
	}
}

// Spec (WEB-13, T44): raw HTML never becomes a node or text, a link that
// is not absolute http(s) is its words only, an image is its alt text,
// entities stay text, bidi overrides and controls are gone.
func TestMarkdownDropsRawHTMLAndBadLinks(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "web", "md", "hostile.md"))
	if err != nil {
		t.Fatal(err)
	}
	n := webMarkdownParse(string(src))
	b, _ := json.Marshal(n)
	js := string(b)
	for _, never := range []string{"<script", "onclick", "<b", "javascript:", "data:", "../etc", "ftp://", "u:p@", "pixel.png", `"href":"mailto`, "\\u001b", "\\u0007", "\u202e"} {
		if strings.Contains(js, never) {
			t.Errorf("AST has %q: %s", never, js)
		}
	}
	for _, want := range []string{`"href":"https://ok.example/path"`, `"text":"tracker"`, `img src=x`, `"text":"click data rel ftp user"`, "reversed"} {
		if !strings.Contains(js, want) {
			t.Errorf("AST misses %q: %s", want, js)
		}
	}
	// Deep nesting is flattened, not refused.
	deep := strings.Repeat("> ", 200) + "deep"
	webMdCheck(t, *webMarkdownParse(deep), 0)
}

// FuzzMarkdownAST: any text converts without a panic to an AST of the
// allowed kinds with http(s) links only.
func FuzzMarkdownAST(f *testing.F) {
	for _, s := range []string{"# h\n\n*a* **b** `c`", "<script>x</script>", "[x](javascript:y)", "| a |\n|---|\n| b |", "1. a\n   > b\n     ```\n     c"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 8<<10 {
			return
		}
		webMdCheck(t, *webMarkdownParse(s), 0)
	})
}
