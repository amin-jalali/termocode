package migrate

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// snipUser makes a VS Code User folder with the given snippet files.
func snipUser(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".config", "Code", "User")
	writeAt(t, dir, "settings.json", "{}")
	for name, data := range files {
		writeAt(t, dir, "snippets/"+name, data)
	}
	return dir
}

func snippetAt(d Document, scope, trigger string) (Snippet, bool) {
	for _, s := range d.Snippets {
		if s.Scope == scope && s.Trigger == trigger {
			return s, true
		}
	}
	return Snippet{}, false
}

func TestReadVSCodeSnippetsPerLanguageAndGlobal(t *testing.T) {
	dir := snipUser(t, map[string]string{
		"go.json": `{
  // comments and trailing commas are fine
  "Error wrap": {"prefix": "errw", "body": ["if err != nil {", "\treturn err", "}"], "description": "wrap",},
  "Two": {"prefix": ["tw", "two"], "body": "x"},
}`,
		"shellscript.json": `{"Shebang": {"prefix": "sb", "body": "#!/bin/sh"}}`,
		"all.code-snippets": `{
  "Todo": {"prefix": "todo", "body": "// TODO"},
  "Some": {"scope": "javascript, typescript", "prefix": "cl", "body": "console.log($1)"}
}`,
		"package.json": `{"name": "ignored"}`,
		"notes.txt":    "ignored",
	})
	d, err := ReadVSCode(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Skipped) != 0 {
		t.Fatalf("skipped = %+v", d.Skipped)
	}
	s, ok := snippetAt(d, "go", "errw")
	if !ok || s.Name != "Error wrap" || s.Description != "wrap" || s.Body != "if err != nil {\n\treturn err\n}" || s.File != "snippets/go.json" {
		t.Fatalf("errw = %+v", s)
	}
	for _, c := range [][2]string{{"go", "tw"}, {"go", "two"}, {"sh", "sb"}, {"*", "todo"}, {"javascript", "cl"}, {"typescript", "cl"}} {
		if _, ok := snippetAt(d, c[0], c[1]); !ok {
			t.Errorf("missing %v in %+v", c, d.Snippets)
		}
	}
	if len(d.Snippets) != 7 {
		t.Errorf("snippets = %+v", d.Snippets)
	}
}

func TestReadVSCodeSnippetsRowsThatCannotImport(t *testing.T) {
	dir := snipUser(t, map[string]string{
		"go.json": `{
  "Dotted": {"prefix": "console.log", "body": "x"},
  "NoPrefix": {"body": "x"},
  "NoBody": {"prefix": "nb", "body": ""},
  "NotSnippet": 5,
  "A": {"prefix": "same", "body": "a"},
  "B": {"prefix": "same", "body": "b"},
  "Dash": {"prefix": ["ok_1", "bad-one"], "body": "x"}
}`,
		"broken.json": `{"x": `,
	})
	d, _ := ReadVSCode(dir)
	for _, want := range []string{"console.log", "no prefix", "no body", "not a snippet", "same trigger", "bad-one", "not valid JSON"} {
		if !hasSkipped(d, RowSnippet, want) {
			t.Errorf("no skipped row mentioning %q: %+v", want, d.Skipped)
		}
	}
	if s, ok := snippetAt(d, "go", "same"); !ok || s.Name != "A" {
		t.Errorf("first duplicate must win: %+v", s)
	}
	if _, ok := snippetAt(d, "go", "ok_1"); !ok || len(d.Snippets) != 2 {
		t.Errorf("snippets = %+v", d.Snippets)
	}
	for _, s := range d.Skipped {
		if s.Kind != RowSnippet || s.Reason == "" {
			t.Errorf("bad skipped %+v", s)
		}
	}
}

func TestReadVSCodeSnippetsStayInsideTheFolder(t *testing.T) {
	dir := snipUser(t, map[string]string{"go.json": `{"A": {"prefix": "a", "body": "a"}}`})
	outside := t.TempDir()
	writeAt(t, outside, "secret.json", `{"S": {"prefix": "secret", "body": "stolen"}}`)
	if err := os.Symlink(filepath.Join(outside, "secret.json"), filepath.Join(dir, "snippets", "evil.json")); err != nil {
		t.Skip("symlinks not available")
	}
	d, _ := ReadVSCode(dir)
	if _, ok := snippetAt(d, "evil", "secret"); ok || len(d.Snippets) != 1 {
		t.Fatalf("link followed: %+v", d.Snippets)
	}
	if !hasSkipped(d, RowSnippet, "evil.json") {
		t.Errorf("link not reported: %+v", d.Skipped)
	}
	// The whole snippets folder as a link that leaves the folder.
	dir2 := filepath.Join(t.TempDir(), "User")
	writeAt(t, dir2, "settings.json", "{}")
	writeAt(t, outside, "snip/go.json", `{"S": {"prefix": "secret", "body": "stolen"}}`)
	if err := os.Symlink(filepath.Join(outside, "snip"), filepath.Join(dir2, "snippets")); err != nil {
		t.Skip("symlinks not available")
	}
	d, _ = ReadVSCode(dir2)
	if len(d.Snippets) != 0 || !hasSkipped(d, RowSnippet, "outside") {
		t.Errorf("folder link: %+v %+v", d.Snippets, d.Skipped)
	}
}

func TestReadVSCodeSnippetsSizeLimits(t *testing.T) {
	big := `{"A": {"prefix": "a", "body": "` + strings.Repeat("x", maxConfigFile) + `"}}`
	huge := strings.Repeat("y", maxSnippetBody+1)
	dir := snipUser(t, map[string]string{
		"big.json": big,
		"go.json":  `{"Long": {"prefix": "long", "body": "` + huge + `"}, "Ok": {"prefix": "ok", "body": "z"}}`,
	})
	d, _ := ReadVSCode(dir)
	if !hasSkipped(d, RowSnippet, "larger than 1 MB") || !hasSkipped(d, RowSnippet, "64 KB") {
		t.Errorf("skipped = %+v", d.Skipped)
	}
	if len(d.Snippets) != 1 || d.Snippets[0].Trigger != "ok" {
		t.Errorf("snippets = %+v", d.Snippets)
	}
	// Too many snippets: the rest are cut with one row.
	var sb strings.Builder
	sb.WriteString("{")
	for i := 0; i < maxSnippets+5; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`"n` + itoa(i) + `": {"prefix": "p` + itoa(i) + `", "body": "x"}`)
	}
	sb.WriteString("}")
	d, _ = ReadVSCode(snipUser(t, map[string]string{"lots.json": sb.String()}))
	if len(d.Snippets) != maxSnippets || !hasSkipped(d, RowSnippet, "more than") {
		t.Errorf("%d snippets, skipped %+v", len(d.Snippets), d.Skipped)
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

func TestDetectVSCodeWithOnlySnippets(t *testing.T) {
	home := t.TempDir()
	writeAt(t, home, ".config/Code/User/snippets/go.json", `{}`)
	writeAt(t, home, "Library/Application Support/Code/User/snippets/go.json", `{}`)
	for _, goos := range []string{"linux", "darwin"} {
		found := false
		for _, s := range Detect(home, goos, "") {
			found = found || s.ID == "vscode"
		}
		if !found {
			t.Errorf("%s: VS Code with only snippets not detected", goos)
		}
	}
}
