package lspinstall

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/amin-jalali/termocode/internal/fetch"
)

// Progress receives short human-readable status lines while an install
// runs ("go install …", "download 40%"). May be nil. It is called from the
// install goroutine.
type Progress func(line string)

// maxOutput caps the captured installer output kept for the failure
// preview (the tail is what matters).
const maxOutput = 64 * 1024

// tailBuffer keeps the last maxOutput bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Write(p)
	if over := b.buf.Len() - maxOutput; over > 0 {
		b.buf.Next(over)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// CheckRequires returns a *MissingToolchainError for the first host
// executable in p.Requires that isn't on PATH.
func CheckRequires(p Plan) error {
	for _, bin := range p.Requires {
		if _, err := lookPath(bin); err != nil {
			return &MissingToolchainError{Tool: p.Tool.Name, Bin: bin, Hint: HostHint(bin)}
		}
	}
	return nil
}

// Install installs t into tools/<name>/ and writes its shim into tools/bin.
// It returns the captured installer output (for a failure preview) and a
// classified error (*MissingToolchainError, *OfflineError,
// *UnsupportedError, or a plain error). Never panics on a missing
// toolchain.
func Install(ctx context.Context, t Tool, progress Progress) (string, error) {
	say := func(s string) {
		if progress != nil {
			progress(s)
		}
	}
	dir, err := ToolDir(t.Name)
	if err != nil {
		return "", err
	}
	binDir, err := BinDir()
	if err != nil {
		return "", err
	}
	plan, err := BuildPlan(t, dir, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	if err := CheckRequires(plan); err != nil {
		return "", err
	}

	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	out := &tailBuffer{}
	fail := func(err error) (string, error) {
		_ = os.RemoveAll(dir)
		return out.String(), ClassifyFailure(t.Name, out.String(), err)
	}

	execArgv := plan.Exec
	for i, step := range plan.Steps {
		say(fmt.Sprintf("[%d/%d] %s", i+1, len(plan.Steps), step.Label))
		fmt.Fprintf(out, "$ %s\n", step.Label)
		switch step.Kind {
		case StepRun:
			if err := runStep(ctx, step, dir, out); err != nil {
				return fail(err)
			}
		case StepRustupWhich:
			var which bytes.Buffer
			c := exec.CommandContext(ctx, step.Argv[0], step.Argv[1:]...)
			c.Stdout = &which
			c.Stderr = out
			if err := c.Run(); err != nil {
				return fail(err)
			}
			p := strings.TrimSpace(which.String())
			if p == "" {
				return fail(fmt.Errorf("rustup which %s returned nothing", t.Bin))
			}
			execArgv = []string{p}
		case StepDownload:
			if err := downloadStep(ctx, step, dir, say); err != nil {
				fmt.Fprintf(out, "%v\n", err)
				return fail(err)
			}
		}
	}

	if len(execArgv) == 0 {
		return fail(fmt.Errorf("%s: no executable to link", t.Name))
	}
	// Every absolute path the shim references must exist ("node" etc. are
	// resolved through PATH at run time).
	for _, a := range execArgv {
		if filepath.IsAbs(a) {
			if _, err := os.Stat(a); err != nil {
				return fail(fmt.Errorf("installed, but %s is missing: %w", a, err))
			}
		}
	}
	if err := WriteShim(binDir, t.Bin, t.Name, execArgv); err != nil {
		return fail(err)
	}
	_ = os.WriteFile(filepath.Join(dir, versionFile), []byte(t.Version+"\n"), 0o644)
	say("done")
	return out.String(), nil
}

func runStep(ctx context.Context, step Step, dir string, out *tailBuffer) error {
	c := exec.CommandContext(ctx, step.Argv[0], step.Argv[1:]...)
	c.Dir = dir
	c.Env = append(os.Environ(), step.Env...)
	c.Stdout = out
	c.Stderr = out
	return c.Run()
}

func downloadStep(ctx context.Context, step Step, dir string, say func(string)) error {
	tmp, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	lastPct := -1
	err = fetch.Download(ctx, step.URL, tmpPath, func(done, total int64) {
		if total <= 0 {
			return
		}
		pct := int(done * 100 / total)
		// Report in 20 % steps so the UI isn't flooded.
		if pct/20 != lastPct/20 {
			lastPct = pct
			say(fmt.Sprintf("downloading %d%%", pct))
		}
	})
	if err != nil {
		return err
	}
	say("extracting")
	switch step.Archive {
	case "zip":
		return fetch.ExtractZip(tmpPath, step.Dest, nil)
	case "tar.gz":
		return fetch.ExtractTarGz(tmpPath, step.Dest, nil)
	case "gz":
		return fetch.Gunzip(tmpPath, step.Dest, 0o755)
	default:
		b, err := os.ReadFile(tmpPath)
		if err != nil {
			return err
		}
		return os.WriteFile(step.Dest, b, 0o755)
	}
}

// shellQuote single-quotes s for /bin/sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// RenderShim returns the /bin/sh shim that execs argv with the caller's
// arguments appended. Pure.
func RenderShim(tool string, argv []string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# Managed by termocode (tool: " + tool + "). Remove via LSP: Manage Language Servers.\n")
	b.WriteString("exec")
	for _, a := range argv {
		b.WriteString(" ")
		b.WriteString(shellQuote(a))
	}
	b.WriteString(" \"$@\"\n")
	return b.String()
}

// WriteShim writes tools/bin/<bin> pointing at argv.
func WriteShim(binDir, bin, tool string, argv []string) error {
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(binDir, bin)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(RenderShim(tool, argv)), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Uninstall removes a managed tool (its shim and tools/<name>). System
// installs are never touched; rustup components stay installed.
func Uninstall(t Tool) error {
	binDir, err := BinDir()
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(binDir, t.Bin)); err != nil && !os.IsNotExist(err) {
		return err
	}
	dir, err := ToolDir(t.Name)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
