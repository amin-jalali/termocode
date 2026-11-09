package ai

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/search"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// ── Tools on a workspace host (RC-5 4e) ──────────────────────────────────
//
// A container workspace (ADR 0018) keeps every tool name, schema, class
// and approval rule of the local one; only the Run changes: files go
// through the Host FS, git and commands through the Host Exec (`docker
// exec -i -w <root> …`). Nothing reads or writes the local disk at the
// same path, and nothing is installed in the container.

// resolveOnHost is Resolve for a host workspace: a check by name only
// (the host's links cannot be followed from here), so ".." can never
// climb out of the root.
func (w Workspace) resolveOnHost(p string) (string, error) {
	abs, err := workspace.Resolve(w.Root, p)
	if errors.Is(err, workspace.ErrOutsideRoot) {
		return "", ErrOutsideWorkspace
	}
	return abs, err
}

// hostCallTimeout caps one file call on the host.
const hostCallTimeout = 30 * time.Second

// hostGlobDirs caps the folders the glob tool lists on a host: each one
// is a command there.
const hostGlobDirs = 3000

// hostSkipDirs are the folders the glob tool never enters on a host (the
// local glob skips them too).
var hostSkipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "target": true, "dist": true,
	"build": true, "__pycache__": true, "venv": true, ".venv": true,
}

// onHost swaps the Run of every file / git / run tool for one that works
// through w's host.
func onHost(w Workspace, tools []Tool) []Tool {
	runs := hostRuns(w)
	out := make([]Tool, 0, len(tools))
	for _, t := range tools {
		if r, ok := runs[t.Name]; ok {
			t.Run = r
		}
		out = append(out, t)
	}
	return out
}

// hostGit runs git args in the workspace root on the host and gives its
// output for the model (exit status as text, like runCmd).
func (w Workspace) hostGit(ctx context.Context, args ...string) (string, error) {
	return w.hostRun(ctx, 30*time.Second, append([]string{"git"}, args...)...)
}

// hostRun runs argv in the root on the host with the non-interactive
// variables set (OMX-10) and stdout and stderr together.
func (w Workspace) hostRun(ctx context.Context, timeout time.Duration, argv ...string) (string, error) {
	if w.Exec == nil {
		return "", errors.New("the workspace host cannot run commands")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	full := append([]string{"env"}, nonInteractive...)
	full = append(full, argv...)
	out, stderr, code, err := w.Exec.Capture(ctx, w.Root, full...)
	if ctx.Err() == context.DeadlineExceeded {
		return capOut(out+stderr, maxOutBytes) + "\n[timed out]", nil
	}
	if err != nil {
		return "", err
	}
	s := capOut(out+stderr, maxOutBytes)
	if code != 0 {
		return fmt.Sprintf("%s\n[exit status %d]", s, code), nil
	}
	if strings.TrimSpace(s) == "" {
		return "[no output, exit 0]", nil
	}
	return s, nil
}

// hostGitOK runs git and fails with git's own words on a non-zero exit
// (the write tools say "staged x" only when it worked).
func (w Workspace) hostGitOK(ctx context.Context, done string, args ...string) (string, error) {
	if w.Exec == nil {
		return "", errors.New("the workspace host cannot run commands")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	full := append(append([]string{"env"}, nonInteractive...), append([]string{"git"}, args...)...)
	_, stderr, code, err := w.Exec.Capture(ctx, w.Root, full...)
	if err != nil {
		return "", err
	}
	if code != 0 {
		msg := strings.TrimSpace(stderr)
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		if msg == "" {
			msg = fmt.Sprintf("git exited with code %d", code)
		}
		return "", errors.New(msg)
	}
	return done, nil
}

func hostCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, hostCallTimeout)
}

// hostRuns are the Run funcs of the host tools, by tool name. search
// needs none: it walks w.FS itself (search.Options.FS).
func hostRuns(w Workspace) map[string]func(context.Context, map[string]any) (string, error) {
	p := func(a map[string]any) string { return ArgString(a, "path") }
	return map[string]func(context.Context, map[string]any) (string, error){
		"read_file": func(ctx context.Context, a map[string]any) (string, error) {
			abs, err := w.Resolve(p(a))
			if err != nil {
				return "", err
			}
			if SecretFile(abs) {
				return "", errSecretFile(w.rel(abs))
			}
			ctx, cancel := hostCtx(ctx)
			defer cancel()
			b, err := w.FS.ReadFile(ctx, abs)
			if err != nil {
				return "", err
			}
			return capOut(string(b), maxReadBytes), nil
		},
		"list_dir": func(ctx context.Context, a map[string]any) (string, error) {
			abs, err := w.Resolve(p(a))
			if err != nil {
				return "", err
			}
			ctx, cancel := hostCtx(ctx)
			defer cancel()
			ents, err := w.FS.ReadDir(ctx, abs)
			if err != nil {
				return "", err
			}
			slices.SortFunc(ents, func(x, y fs.DirEntry) int { return strings.Compare(x.Name(), y.Name()) })
			var b strings.Builder
			for _, e := range ents {
				name := e.Name()
				if e.IsDir() {
					name += "/"
				}
				b.WriteString(name + "\n")
			}
			return capOut(b.String(), maxOutBytes), nil
		},
		"glob": func(ctx context.Context, a map[string]any) (string, error) {
			globs := search.SplitGlobs(ArgString(a, "pattern"))
			if len(globs) == 0 {
				return "", errors.New("pattern is empty")
			}
			abs, err := w.Resolve(p(a))
			if err != nil {
				return "", err
			}
			files, total, err := w.hostGlob(ctx, abs, globs, 200)
			if err != nil {
				return "", err
			}
			if len(files) == 0 {
				return "no files", nil
			}
			var b strings.Builder
			for _, f := range files {
				b.WriteString(f + "\n")
			}
			if total > len(files) {
				fmt.Fprintf(&b, "(showing %d of %d; narrow the pattern)\n", len(files), total)
			}
			return capOut(b.String(), maxOutBytes), nil
		},
		"git_status": func(ctx context.Context, _ map[string]any) (string, error) {
			out, err := w.hostGit(ctx, "status", "--porcelain=v1")
			if err != nil {
				return "", err
			}
			if out == "[no output, exit 0]" {
				return "clean", nil
			}
			return out, nil
		},
		"git_diff": func(ctx context.Context, a map[string]any) (string, error) {
			args := []string{"diff", "--no-color", "--no-ext-diff"}
			if f := p(a); f != "" {
				if _, err := w.Resolve(f); err != nil {
					return "", err
				}
				args = append(args, "HEAD", "--", f)
			} else {
				args = append(args, "HEAD")
			}
			out, err := w.hostRun(ctx, 30*time.Second, append([]string{"git"}, args...)...)
			if err != nil {
				return "", err
			}
			if out == "[no output, exit 0]" {
				return "no changes", nil
			}
			return out, nil
		},
		"git_branch": func(ctx context.Context, _ map[string]any) (string, error) {
			return w.hostGit(ctx, "branch", "--list")
		},
		"write_file": func(ctx context.Context, a map[string]any) (string, error) {
			abs, err := w.Resolve(p(a))
			if err != nil {
				return "", err
			}
			ctx, cancel := hostCtx(ctx)
			defer cancel()
			if err := w.FS.WriteFile(ctx, abs, []byte(ArgString(a, "content")), 0o644); err != nil {
				return "", err
			}
			return "wrote " + w.rel(abs), nil
		},
		"edit_file": func(ctx context.Context, a map[string]any) (string, error) {
			abs, err := w.Resolve(p(a))
			if err != nil {
				return "", err
			}
			if SecretFile(abs) {
				return "", errSecretFile(w.rel(abs))
			}
			ctx, cancel := hostCtx(ctx)
			defer cancel()
			b, err := w.FS.ReadFile(ctx, abs)
			if err != nil {
				return "", err
			}
			text, err := ApplyEdit(string(b), ArgString(a, "old_text"), ArgString(a, "new_text"), ArgBool(a, "replace_all"))
			if err != nil {
				return "", err
			}
			if err := w.FS.WriteFile(ctx, abs, []byte(text), 0o644); err != nil {
				return "", err
			}
			return "edited " + w.rel(abs), nil
		},
		"create_file": func(ctx context.Context, a map[string]any) (string, error) {
			abs, err := w.Resolve(p(a))
			if err != nil {
				return "", err
			}
			ctx, cancel := hostCtx(ctx)
			defer cancel()
			if _, err := w.FS.Stat(ctx, abs); err == nil {
				return "", errors.New("file already exists")
			}
			if err := w.FS.WriteFile(ctx, abs, []byte(ArgString(a, "content")), 0o644); err != nil {
				return "", err
			}
			return "created " + w.rel(abs), nil
		},
		"create_dir": func(ctx context.Context, a map[string]any) (string, error) {
			abs, err := w.Resolve(p(a))
			if err != nil {
				return "", err
			}
			ctx, cancel := hostCtx(ctx)
			defer cancel()
			return "created " + w.rel(abs), w.FS.Mkdir(ctx, abs, 0o755)
		},
		"delete": func(ctx context.Context, a map[string]any) (string, error) {
			abs, err := w.Resolve(p(a))
			if err != nil {
				return "", err
			}
			if root, _ := w.Resolve(""); abs == root {
				return "", errors.New("refusing to delete the workspace root")
			}
			ctx, cancel := hostCtx(ctx)
			defer cancel()
			return "deleted " + w.rel(abs), w.FS.Remove(ctx, abs)
		},
		"rename": func(ctx context.Context, a map[string]any) (string, error) {
			from, err := w.Resolve(ArgString(a, "from"))
			if err != nil {
				return "", err
			}
			to, err := w.Resolve(ArgString(a, "to"))
			if err != nil {
				return "", err
			}
			ctx, cancel := hostCtx(ctx)
			defer cancel()
			return "renamed to " + w.rel(to), w.FS.Rename(ctx, from, to)
		},
		"git_stage": func(ctx context.Context, a map[string]any) (string, error) {
			if _, err := w.Resolve(p(a)); err != nil {
				return "", err
			}
			return w.hostGitOK(ctx, "staged "+p(a), "add", "--", p(a))
		},
		"git_commit": func(ctx context.Context, a map[string]any) (string, error) {
			args := []string{"commit"}
			if ArgBool(a, "signoff") {
				args = append(args, "--signoff")
			}
			if !ArgBool(a, "amend") {
				return w.hostGitOK(ctx, "committed", append(args, "-m", ArgString(a, "message"))...)
			}
			args = append(args, "--amend")
			if strings.TrimSpace(ArgString(a, "message")) == "" {
				return w.hostGitOK(ctx, "amended the last commit", append(args, "--no-edit")...)
			}
			return w.hostGitOK(ctx, "amended the last commit", append(args, "-m", ArgString(a, "message"))...)
		},
		"git_checkout": func(ctx context.Context, a map[string]any) (string, error) {
			br := ArgString(a, "branch")
			if strings.HasPrefix(br, "-") {
				return "", errors.New("a branch name cannot start with -")
			}
			if ArgBool(a, "create") {
				return w.hostGitOK(ctx, "created and switched to "+br, "checkout", "-b", br)
			}
			return w.hostGitOK(ctx, "switched to "+br, "checkout", br)
		},
		"git_push": func(ctx context.Context, _ map[string]any) (string, error) {
			return w.hostGitOK(ctx, "pushed", "push")
		},
		"git_pull": func(ctx context.Context, _ map[string]any) (string, error) {
			return w.hostGitOK(ctx, "pulled", "pull")
		},
		"run_command": func(ctx context.Context, a map[string]any) (string, error) {
			t := w.RunTimeout
			if t <= 0 {
				t = 2 * time.Minute
			}
			if StartsWebPanel(ArgString(a, "command")) {
				return "", ErrNoWebPanel
			}
			// The command text goes to the container's sh as one
			// argument; no local shell reads it.
			return w.hostRun(ctx, t, "sh", "-c", ArgString(a, "command"))
		},
	}
}

// hostGlob walks dir on the host and gives the paths (relative to the
// root, sorted) that match globs, at most max of them, and how many
// matched in all.
func (w Workspace) hostGlob(ctx context.Context, dir string, globs []string, max int) ([]string, int, error) {
	var include, exclude []string
	for _, g := range globs {
		if e, ok := strings.CutPrefix(g, "!"); ok {
			exclude = append(exclude, e)
		} else {
			include = append(include, g)
		}
	}
	matches := func(rel string) bool {
		for _, g := range exclude {
			if search.MatchGlob(g, rel) {
				return false
			}
		}
		if len(include) == 0 {
			return true
		}
		for _, g := range include {
			if search.MatchGlob(g, rel) {
				return true
			}
		}
		return false
	}
	root := path.Clean(w.Root)
	var found []string
	queue := []string{path.Clean(dir)}
	for n := 0; len(queue) > 0 && n < hostGlobDirs; n++ {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		d := queue[0]
		queue = queue[1:]
		cctx, cancel := hostCtx(ctx)
		ents, err := w.FS.ReadDir(cctx, d)
		cancel()
		if err != nil {
			if n == 0 {
				return nil, 0, err
			}
			continue
		}
		for _, e := range ents {
			name := e.Name()
			p := path.Join(d, name)
			if e.IsDir() {
				if strings.HasPrefix(name, ".") || hostSkipDirs[name] {
					continue
				}
				queue = append(queue, p)
				continue
			}
			rel := strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
			if matches(rel) {
				found = append(found, rel)
			}
		}
	}
	slices.Sort(found)
	total := len(found)
	if len(found) > max {
		found = found[:max]
	}
	return found, total, nil
}

// statFile is os.Stat, or the host's Stat for a host workspace.
func (w Workspace) statFile(ctx context.Context, abs string) (fs.FileInfo, error) {
	if w.FS == nil {
		return os.Stat(abs)
	}
	ctx, cancel := hostCtx(ctx)
	defer cancel()
	return w.FS.Stat(ctx, abs)
}

// readFile is os.ReadFile, or the host's ReadFile for a host workspace.
func (w Workspace) readFile(ctx context.Context, abs string) ([]byte, error) {
	if w.FS == nil {
		return os.ReadFile(abs)
	}
	ctx, cancel := hostCtx(ctx)
	defer cancel()
	return w.FS.ReadFile(ctx, abs)
}
