package app

import (
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/workspace"
	"github.com/amin-jalali/termocode/internal/workspace/localhost"
)

// setProcWorkspace sets the process workspace for one test.
func setProcWorkspace(t *testing.T, ws workspace.Workspace) {
	t.Helper()
	old := processWS.Load()
	SetWorkspace(ws)
	t.Cleanup(func() { processWS.Store(old) })
}

// oldSessionKey is how per-workspace session files were named before
// workspace IDs: a hash of the clean, symlink-free path.
func oldSessionKey(path string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:8])
}

// A session file written by an older termocode (keyed by the folder
// path) still loads now that sessions are keyed by Workspace.ID.
func TestSessionOldPathKeyedFileLoadsByWorkspaceID(t *testing.T) {
	cfg := withConfigDir(t)
	dir := t.TempDir()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	file := filepath.Join(dir, "main.go")
	old := `{
  "workspace": "` + dir + `",
  "open_files": ["` + file + `"],
  "active_path": "` + file + `",
  "expanded_dirs": null,
  "cursors": {"` + file + `": [12, 3]}
}`
	p := filepath.Join(cfg, "termocode", "sessions", oldSessionKey(dir)+".json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	ws, err := localhost.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := sessionKeyFor(ws); got != oldSessionKey(dir) {
		t.Fatalf("sessionKeyFor(%s) = %s, want the old key %s", ws.ID, got, oldSessionKey(dir))
	}
	setProcWorkspace(t, ws)
	t.Chdir(t.TempDir()) // the process folder no longer decides

	s := loadSession()
	if len(s.OpenFiles) != 1 || s.OpenFiles[0] != file || s.ActivePath != file || s.Cursors[file] != [2]int{12, 3} {
		t.Fatalf("old session not loaded: %+v", s)
	}
}

// A container workspace is keyed by its ID, not by its path: /app in
// two containers (or a local /app) must not share a session.
func TestSessionContainerWorkspaceKeyedByID(t *testing.T) {
	cfg := withConfigDir(t)
	ws := workspace.Workspace{ID: "container:prod/api-1:/app", Kind: workspace.KindContainer, Root: "/app"}
	setProcWorkspace(t, ws)
	saveSession(SessionState{OpenFiles: []string{"/app/main.go"}, ActivePath: "/app/main.go"})

	want := filepath.Join(cfg, "termocode", "sessions", workspace.Key(ws.ID)+".json")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("session not at %s: %v", want, err)
	}
	if _, err := os.Stat(filepath.Join(cfg, "termocode", "sessions", oldSessionKey("/app")+".json")); err == nil {
		t.Fatal("container session written under the local path key of /app")
	}
	s, ok := readSessionFile(want)
	if !ok || s.Workspace != ws.ID {
		t.Fatalf("stored workspace = %q, want the ID", s.Workspace)
	}
}

// Hot-exit backups of a local workspace stay where they were.
func TestHotExitRootKeyedByWorkspaceID(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/s")
	got, err := hotExitRootFor(workspace.LocalID("/w/proj"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/s/termocode/backup", oldSessionKey("/w/proj")); got != want {
		t.Fatalf("hotExitRootFor = %s, want %s", got, want)
	}
}

// The Model reads its root from m.ws, not from the process folder.
func TestModelRootComesFromWorkspace(t *testing.T) {
	dir := t.TempDir()
	ws, err := localhost.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	m := Model{ws: ws}
	if got, err := m.rootDir(); err != nil || got != dir {
		t.Fatalf("rootDir = %q, %v; want %q", got, err, dir)
	}
	// A Model not built by New falls back to the process workspace.
	setProcWorkspace(t, ws)
	if got, _ := (Model{}).rootDir(); got != dir {
		t.Fatalf("zero Model rootDir = %q, want %q", got, dir)
	}
}

// getwdAllowed lists the files of internal/app that may still read the
// process folder, with the reason. Everything else uses the workspace
// (m.rootDir / procRootDir), so a container workspace works later.
var getwdAllowed = map[string]string{}

// os.Getwd must not come back in internal/app (ADR 0018).
func TestNoGetwdInApp(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		if _, ok := getwdAllowed[f]; ok {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Getwd" {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "os" {
				t.Errorf("%s: os.Getwd; use m.rootDir() or procRootDir() (ADR 0018)", fset.Position(sel.Pos()))
			}
			return true
		})
	}
}
