package ai

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// proofFakeBrowser is a shell script that acts like a headless browser:
// it writes a tiny PNG to the --screenshot= path and records its args.
func proofFakeBrowser(t *testing.T) (bin, argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake browser is a shell script")
	}
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args.txt")
	bin = filepath.Join(dir, "fake-chrome")
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do echo \"$a\" >> '" + argsFile + "'; case \"$a\" in --screenshot=*) printf '\\211PNG\\r\\n\\032\\nfake' > \"${a#--screenshot=}\";; esac; done\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, argsFile
}

// Spec (CMP-52): a localhost page or a workspace file is local (no
// card); another web address asks (class run); other schemes, paths
// outside the workspace and secret files are refused before any card.
func TestProofTarget(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "index.html"), []byte("<p>hi</p>"), 0o644)
	os.WriteFile(filepath.Join(root, ".env"), []byte("A=1"), 0o644)
	w := Workspace{Root: root}
	for _, c := range []struct {
		in    string
		local bool
		bad   bool
	}{
		{"http://localhost:5173/", true, false},
		{"http://127.0.0.1:8080/login", true, false},
		{"http://[::1]:3000", true, false},
		{"https://example.com/", false, false},
		{"index.html", true, false},
		{"file:///etc/passwd", false, true},
		{"javascript:alert(1)", false, true},
		{"../outside.html", false, true},
		{".env", false, true},
		{"missing.html", false, true},
		{"", false, true},
	} {
		target, local, err := proofTarget(w, c.in)
		if (err != nil) != c.bad {
			t.Fatalf("%q: err %v, want bad=%v", c.in, err, c.bad)
		}
		if err == nil && local != c.local {
			t.Fatalf("%q: local %v", c.in, local)
		}
		if c.in == "index.html" && !strings.HasPrefix(target, "file://") {
			t.Fatalf("workspace file target %q", target)
		}
	}
	tool := ProofShotTool(w, func() string { return "" })
	if m, _ := tool.Gate(map[string]any{"url": "http://localhost:3000"}); m {
		t.Fatal("a localhost page asks")
	}
	if m, cl := tool.Gate(map[string]any{"url": "https://example.com"}); !m || cl != ClassRun {
		t.Fatalf("a web address: mutating %v class %q", m, cl)
	}
	if tool.Validate(map[string]any{"url": "file:///etc/passwd"}) == nil {
		t.Fatal("file:// not refused")
	}
}

// Spec (CMP-52): the browser runs headless with its own throw-away
// profile and the asked size; the PNG lands in .termocode/proof with a
// side file (address, size, note); the model gets the picture when it
// takes images; ListProofs lists it newest first; ClearProofs removes
// only the proof files.
func TestProofShotRun(t *testing.T) {
	bin, argsFile := proofFakeBrowser(t)
	root := t.TempDir()
	w := Workspace{Root: root}
	old := proofNow
	proofNow = func() time.Time { return time.Date(2026, 11, 7, 10, 30, 0, 0, time.UTC) }
	t.Cleanup(func() { proofNow = old })

	tool := ProofShotTool(w, func() string { return bin })
	ctx, take := WithImageSink(context.Background())
	out, err := tool.Run(ctx, map[string]any{"url": "http://localhost:5173/settings", "width": 390, "height": 844, "note": "phone layout of the settings page"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, ProofDir) || !strings.Contains(out, "390×844") {
		t.Fatalf("result %q", out)
	}
	b, _ := os.ReadFile(argsFile)
	args := string(b)
	for _, want := range []string{"--headless", "--window-size=390,844", "--user-data-dir=", "--screenshot=", "http://localhost:5173/settings"} {
		if !strings.Contains(args, want) {
			t.Fatalf("browser args miss %q:\n%s", want, args)
		}
	}
	if imgs := take(); len(imgs) != 1 || imgs[0].MediaType != "image/png" {
		t.Fatalf("images %d", len(imgs))
	}

	proofNow = func() time.Time { return time.Date(2026, 11, 7, 10, 31, 0, 0, time.UTC) }
	if _, err := tool.Run(context.Background(), map[string]any{"url": "http://localhost:5173/"}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, ProofDir, "notes.txt"), []byte("keep"), 0o644)
	list, err := ListProofs(root)
	if err != nil || len(list) != 2 {
		t.Fatalf("list %d %v", len(list), err)
	}
	if list[0].Width != 1280 || list[1].Width != 390 || list[1].Note != "phone layout of the settings page" || list[1].URL != "http://localhost:5173/settings" {
		t.Fatalf("list %+v", list)
	}
	var meta Proof
	jb, _ := os.ReadFile(strings.TrimSuffix(list[1].Path, ".png") + ".json")
	if json.Unmarshal(jb, &meta) != nil || meta.Height != 844 {
		t.Fatalf("side file %s", jb)
	}
	n, err := ClearProofs(root)
	if err != nil || n != 2 {
		t.Fatalf("clear %d %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(root, ProofDir, "notes.txt")); err != nil {
		t.Fatal("clear removed a file that is not a proof")
	}
}

// Spec (CMP-52): with no browser the call fails with how to fix it, and
// a browser that writes no picture is an error, not a fake proof.
func TestProofShotNoBrowser(t *testing.T) {
	root := t.TempDir()
	tool := ProofShotTool(Workspace{Root: root}, func() string { return "" })
	_, err := tool.Run(context.Background(), map[string]any{"url": "http://localhost:1/"})
	if err == nil || !strings.Contains(err.Error(), "TERMOCODE_BROWSER") {
		t.Fatalf("err %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	silent := filepath.Join(t.TempDir(), "silent")
	os.WriteFile(silent, []byte("#!/bin/sh\necho crashed\nexit 0\n"), 0o755)
	tool = ProofShotTool(Workspace{Root: root}, func() string { return silent })
	if _, err := tool.Run(context.Background(), map[string]any{"url": "http://localhost:1/"}); err == nil {
		t.Fatal("no picture but no error")
	}
	if list, _ := ListProofs(root); len(list) != 0 {
		t.Fatalf("a proof without a picture: %+v", list)
	}
}

// Spec (CMP-52): TERMOCODE_BROWSER wins; otherwise the first known
// browser on PATH.
func TestFindProofBrowser(t *testing.T) {
	old := proofLookPath
	t.Cleanup(func() { proofLookPath = old })
	proofLookPath = func(name string) (string, error) {
		if name == "chromium" {
			return "/usr/bin/chromium", nil
		}
		return "", os.ErrNotExist
	}
	t.Setenv("TERMOCODE_BROWSER", "")
	if got := FindProofBrowser(); got != "/usr/bin/chromium" {
		t.Fatalf("found %q", got)
	}
	t.Setenv("TERMOCODE_BROWSER", "/opt/my-chrome")
	if got := FindProofBrowser(); got != "/opt/my-chrome" {
		t.Fatalf("env %q", got)
	}
}
