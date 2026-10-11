package testkit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSlowGitClone puts a `git` first on PATH whose `clone` logs its
// arguments, waits `wait` and fails (no network); any other git command
// runs the real git. It returns the log file.
func fakeSlowGitClone(t testing.TB, wait string) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(bin, "clone.log")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = clone ]; then\n" +
		"  echo \"$@\" >> '" + log + "'\n" +
		"  sleep " + wait + "\n" +
		"  echo 'fatal: unable to access: Could not resolve host' >&2\n" +
		"  exit 128\n" +
		"fi\n" +
		"exec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// Spec: Given a first run (no plugins downloaded yet) and a network that
// hangs for 4 s and then fails (fake git),
// when the app starts,
// then nvim setup is not held up by the downloads (ready in well under the
// 4 s), the user can open a file and type at once, every clone goes into
// TERMOCODE_PLUGINS_DIR, and when the downloads fail a toast says so.
func TestPluginCloneNeverBlocksStartup(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	var log, plugins string
	start := time.Now()
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: textFiles(),
		Setup: func(t testing.TB, dir string) {
			log = fakeSlowGitClone(t, "4")
			plugins = os.Getenv("TERMOCODE_PLUGINS_DIR")
			// First run: no plugin checkouts at all.
			if err := os.RemoveAll(plugins); err != nil {
				t.Fatal(err)
			}
		}})
	if took := time.Since(start); took > 2500*time.Millisecond {
		t.Errorf("nvim setup took %v: it waited for the plugin downloads", took)
	}
	open(d, "a.txt")
	d.Type("xy")
	if got := bufLines(d); got != "xyalpha\nbravo\ncharlie" {
		t.Errorf("typing during the downloads: buffer = %q", got)
	}

	// The downloads fail after 4 s: the user is told.
	if !d.WaitForText("Plugin download failed", 8*time.Second) {
		t.Errorf("no toast after the plugin downloads failed:\n%s", screen(d))
	}
	b, _ := os.ReadFile(log)
	clones := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(clones) < 3 {
		t.Errorf("git clone ran %d times, want 3 (multi-cursor, dap, bracket pairs): %q", len(clones), b)
	}
	for _, c := range clones {
		f := strings.Fields(c)
		if len(f) == 0 || !strings.HasPrefix(f[len(f)-1], plugins+string(os.PathSeparator)) {
			t.Errorf("clone %q does not go into TERMOCODE_PLUGINS_DIR %s", c, plugins)
		}
	}
}

// Spec: Given a first run with no bracket-pair plugin downloaded and a
// network that delivers it after 1 s (fake git),
// when the app starts,
// then nvim is ready before the download ends, and once it ends the plugin
// is set up in the running editor (on the runtimepath, no error recorded)
// without a restart and without a failure toast.
func TestPluginCloneSetsUpPluginWhenDone(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	var plugins string
	start := time.Now()
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: textFiles(),
		Setup: func(t testing.TB, dir string) {
			plugins = os.Getenv("TERMOCODE_PLUGINS_DIR")
			_ = os.RemoveAll(filepath.Join(plugins, "rainbow-delimiters.nvim"))
			bin := filepath.Join(t.TempDir(), "bin")
			if err := os.MkdirAll(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			// clone: wait, then create the checkout at the last argument.
			script := "#!/bin/sh\n" +
				"for a; do dest=$a; done\n" +
				"sleep 1\n" +
				"mkdir -p \"$dest/lua/rainbow-delimiters\"\n" +
				"echo 'return {}' > \"$dest/lua/rainbow-delimiters/setup.lua\"\n"
			if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		}})
	if took := time.Since(start); took > 900*time.Millisecond {
		t.Errorf("nvim setup took %v: it waited for the plugin download", took)
	}
	dir := filepath.Join(plugins, "rainbow-delimiters.nvim")
	onRtp := func() bool {
		return lua(d, `return (vim.tbl_contains(vim.opt.runtimepath:get(), ...) and not vim.g.termocode_bracket_pair_error) and 'yes' or 'no'`, dir) == "yes"
	}
	if !d.WaitFor(onRtp, 5*time.Second) {
		t.Errorf("plugin not set up after the download: rtp has it = %v, error = %q",
			onRtp(), lua(d, `return vim.g.termocode_bracket_pair_error or ''`))
	}
	if strings.Contains(screen(d), "Plugin download failed") {
		t.Errorf("failure toast after a good download:\n%s", screen(d))
	}
}
