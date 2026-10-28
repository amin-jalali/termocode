//go:build unix

package lspinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The agent CLIs install with npm into tools/<name>, never globally and
// never with sudo, and stay out of the language-server lists.
func TestAgentCLITools(t *testing.T) {
	for _, c := range []struct{ name, spec, bin string }{
		{"claude-code", "@anthropic-ai/claude-code", "claude"},
		{"codex", "@openai/codex", "codex"},
	} {
		tl, ok := Lookup(c.name)
		if !ok {
			t.Fatalf("%s missing", c.name)
		}
		if tl.Category != CategoryAgentCLI || tl.InstallKind != KindNpm || tl.Spec != c.spec || tl.Bin != c.bin {
			t.Fatalf("%s = %+v", c.name, tl)
		}
		p, err := BuildPlan(tl, "/tools/"+c.name, "linux", "amd64")
		if err != nil {
			t.Fatal(err)
		}
		argv := strings.Join(p.Steps[0].Argv, " ")
		if !strings.HasPrefix(argv, "npm install --prefix /tools/"+c.name+" ") || !strings.Contains(argv, c.spec+"@latest") {
			t.Errorf("%s argv %q", c.name, argv)
		}
		for _, bad := range []string{" -g", "--global", "sudo"} {
			if strings.Contains(argv, bad) {
				t.Errorf("%s argv has %q: %q", c.name, bad, argv)
			}
		}
		if p.Exec[0] != "/tools/"+c.name+"/node_modules/.bin/"+c.bin {
			t.Errorf("%s exec %v", c.name, p.Exec)
		}
		if strings.Join(p.Requires, ",") != "npm,node" {
			t.Errorf("%s requires %v", c.name, p.Requires)
		}
		for _, cat := range []Category{CategoryLSP, CategoryDAP} {
			for _, x := range ByCategory(cat) {
				if x.Name == c.name {
					t.Errorf("%s listed as category %d", c.name, cat)
				}
			}
		}
	}
	if got := ByCategory(CategoryAgentCLI); len(got) != 2 {
		t.Fatalf("agent CLIs = %v", got)
	}
}

// fakeBin writes an executable shell script into dir.
func fakeBin(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// Install runs (a fake) npm with --prefix into tools/codex, writes the
// shim into tools/bin, and npm never sees a vendor API key.
func TestInstallAgentCLIWithFakeNpm(t *testing.T) {
	root, fake := t.TempDir(), t.TempDir()
	t.Setenv(ToolsDirEnv, root)
	t.Setenv("PATH", fake+":/usr/bin:/bin")
	t.Setenv("OPENAI_API_KEY", "sk-test-not-for-npm")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-not-for-npm")
	fakeBin(t, fake, "node", "exit 0\n")
	fakeBin(t, fake, "npm", `prefix=""
while [ $# -gt 0 ]; do
  if [ "$1" = "--prefix" ]; then prefix="$2"; shift; fi
  shift
done
echo "keys:${OPENAI_API_KEY:+openai}${ANTHROPIC_API_KEY:+anthropic}" > "$prefix/env.txt"
mkdir -p "$prefix/node_modules/.bin"
printf '#!/bin/sh\necho codex-cli 0.0.1\n' > "$prefix/node_modules/.bin/codex"
chmod +x "$prefix/node_modules/.bin/codex"
`)
	tl, _ := Lookup("codex")
	var lines []string
	if _, err := Install(context.Background(), tl, func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatalf("install: %v", err)
	}
	st := StatusOf(tl)
	if st.State != Managed || st.Path != filepath.Join(root, "bin", "codex") {
		t.Fatalf("status %+v", st)
	}
	b, _ := os.ReadFile(filepath.Join(root, "codex", "env.txt"))
	if got := strings.TrimSpace(string(b)); got != "keys:" {
		t.Fatalf("npm saw API keys: %q", got)
	}
	if len(lines) == 0 || !strings.Contains(lines[0], "npm install @openai/codex@latest") {
		t.Fatalf("progress %v", lines)
	}
}

// Without npm the install stops before it starts, with a typed error the
// card turns into "Codex needs Node.js".
func TestInstallAgentCLIWithoutNpm(t *testing.T) {
	t.Setenv(ToolsDirEnv, t.TempDir())
	t.Setenv("PATH", t.TempDir())
	tl, _ := Lookup("claude-code")
	_, err := Install(context.Background(), tl, nil)
	var mt *MissingToolchainError
	if !errors.As(err, &mt) || mt.Bin != "npm" {
		t.Fatalf("got %v", err)
	}
}
