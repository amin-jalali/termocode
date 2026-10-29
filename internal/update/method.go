package update

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// Method is how termocode was installed, which decides who updates it.
type Method int

const (
	// MethodSelf: a plain file in a folder the user can write (install.sh
	// or a hand copy). termocode replaces it itself.
	MethodSelf Method = iota
	// MethodBrew: Homebrew owns it.
	MethodBrew
	// MethodPackage: a system package (pacman / AUR) owns it.
	MethodPackage
	// MethodGo: `go install` put it in GOBIN / GOPATH/bin.
	MethodGo
	// MethodReadOnly: a plain file the user cannot replace.
	MethodReadOnly
)

// InstallScript is the one-line installer of the README.
const InstallScript = "curl -fsSL https://raw.githubusercontent.com/" + Repo + "/main/scripts/install.sh | sh"

// Install says where the running binary is and how to update it.
type Install struct {
	Method  Method
	Path    string // the binary (symlinks resolved)
	Package string // MethodPackage: the package name
	// Command is what the user runs to update, for every method but
	// MethodSelf.
	Command string
}

// Env is what Detect looks at besides the file system.
type Env struct {
	Getenv func(string) string
	// Owner names the system package that owns path (pacman -Qqo).
	Owner func(path string) (pkg string, ok bool)
}

// Detect finds out how the binary at exe was installed.
func Detect(exe string, env Env) Install {
	path := exe
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		path = real
	}
	in := Install{Path: path}
	switch {
	case isBrew(exe, path, env.Getenv):
		in.Method, in.Command = MethodBrew, "brew upgrade termocode"
	case isGoBin(filepath.Dir(path), env.Getenv):
		in.Method, in.Command = MethodGo, "go install github.com/"+Repo+"/cmd/termocode@latest"
	default:
		if pkg, ok := ownerOf(env, path); ok {
			in.Method, in.Package = MethodPackage, pkg
			in.Command = "yay -S " + pkg
			return in
		}
		if dirWritable(filepath.Dir(path)) {
			in.Method = MethodSelf
			return in
		}
		in.Method = MethodReadOnly
		in.Command = readOnlyCommand(filepath.Dir(path))
	}
	return in
}

func ownerOf(env Env, path string) (string, bool) {
	if env.Owner == nil {
		return "", false
	}
	return env.Owner(path)
}

// isBrew: the binary is in a Homebrew Cellar, or under HOMEBREW_PREFIX /
// HOMEBREW_CELLAR.
func isBrew(exe, real string, getenv func(string) string) bool {
	for _, p := range []string{exe, real} {
		if strings.Contains(filepath.ToSlash(p), "/Cellar/") {
			return true
		}
	}
	for _, k := range []string{"HOMEBREW_PREFIX", "HOMEBREW_CELLAR"} {
		if root := getenv(k); root != "" && within(real, root) {
			return true
		}
	}
	return false
}

// isGoBin: dir is GOBIN or the bin folder of a GOPATH entry (default
// ~/go).
func isGoBin(dir string, getenv func(string) string) bool {
	var dirs []string
	if b := getenv("GOBIN"); b != "" {
		dirs = append(dirs, b)
	}
	gopath := getenv("GOPATH")
	if gopath == "" {
		if home := getenv("HOME"); home != "" {
			gopath = filepath.Join(home, "go")
		}
	}
	for _, p := range filepath.SplitList(gopath) {
		if p != "" {
			dirs = append(dirs, filepath.Join(p, "bin"))
		}
	}
	for _, d := range dirs {
		if sameDir(dir, d) {
			return true
		}
	}
	return false
}

func sameDir(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

// within reports whether path is root or below it.
func within(path, root string) bool {
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// dirWritable tries to create (and removes) a file in dir: the rename
// that replaces the binary needs exactly that right.
func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".termocode-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// readOnlyCommand is the install.sh line that installs into dir's prefix
// (dir must be a "bin" folder), else the plain installer.
func readOnlyCommand(dir string) string {
	if filepath.Base(dir) == "bin" {
		return "curl -fsSL https://raw.githubusercontent.com/" + Repo + "/main/scripts/install.sh | PREFIX=" + filepath.Dir(dir) + " sh"
	}
	return InstallScript
}

// PacmanOwner asks pacman which package owns path (nothing when pacman is
// not installed or no package owns it). Only files under /usr and /opt
// are asked about: a package never puts termocode in a home folder.
func PacmanOwner(path string) (string, bool) {
	if !strings.HasPrefix(path, "/usr/") && !strings.HasPrefix(path, "/opt/") {
		return "", false
	}
	bin, err := exec.LookPath("pacman")
	if err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := spawn.CommandContext(ctx, bin, "-Qqo", path).Output()
	pkg := strings.TrimSpace(string(out))
	if err != nil || pkg == "" || strings.ContainsAny(pkg, " \n") {
		return "", false
	}
	return pkg, true
}
