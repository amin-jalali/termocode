package ext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/theme"
)

// FuzzMarkup reads any extension line. The plain text is never longer
// than the line, only known styles are kept, and rendering never panics.
func FuzzMarkup(f *testing.F) {
	for _, s := range []string{
		"plain",
		"{{error}}bad{{/}} ok",
		"{{bold}}a{{muted}}b{{/}}c{{/}}d",
		"{{nope}}x{{/}}{{/}}{{",
		"{{bold,muted}}x",
		strings.Repeat("{{bold}}", 50) + "deep",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		// A tab may turn into up to four spaces.
		if p := PlainText(line); len(p) > len(line)+3*strings.Count(line, "\t") {
			t.Fatalf("plain text %q longer than %q", p, line)
		}
		for _, sp := range ParseMarkup(line) {
			for _, st := range sp.Styles {
				if !IsToken(st) {
					t.Fatalf("unknown style %q kept", st)
				}
			}
		}
		_ = Render(line, theme.Color256(7), nil)
	})
}

// FuzzExtensionJSON reads any bytes as the registry, a poll answer and
// an extension.json. Nothing may panic, and a process extension that is
// found always has a command.
func FuzzExtensionJSON(f *testing.F) {
	for _, s := range []string{
		`{"commands":{},"panels":[{"id":"x.p","title":"P","icon":"","ext":"x"}],"status":null,"extensions":[{"name":"x","version":"","ok":true,"error":""}]}`,
		`{"panel":"a.b","lines":["x","{{muted}}y{{/}}"],"status":[{"id":"s","text":"3"}]}`,
		`{"status":{}}`,
		`{"name":"demo","run":{"command":"python3","args":["main.py"]},"activationEvents":["onStartup","onCommand:x"]}`,
		`{"run":{"command":"  "}}`,
		`{"run":null}`,
		`[]`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParseRegistry(string(b))
		_, _ = ParsePoll(string(b))
		dir := t.TempDir()
		x := filepath.Join(dir, "demo")
		if err := os.MkdirAll(x, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(x, ManifestFile), b, 0o600); err != nil {
			t.Fatal(err)
		}
		exts, _ := Discover(dir)
		for _, e := range exts {
			if e.IsProcess() && strings.TrimSpace(e.Manifest.Run.Command) == "" {
				t.Fatalf("process extension without a command: %+v", e.Manifest)
			}
			_ = e.Manifest.StartsAtStartup()
			_ = e.Manifest.ActivatesOn("onCommand:x")
		}
	})
}
