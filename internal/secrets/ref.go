package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// Reference kinds.
const (
	RefEnv    = "env"    // ${env:NAME}: the UI process environment
	RefDotenv = "dotenv" // ${dotenv:NAME}: the workspace .env
	RefCmd    = "cmd"    // ${cmd:line}: first line of `sh -c line`
)

// Ref is a parsed reference.
type Ref struct {
	Kind string
	Arg  string
}

func (r Ref) String() string { return "${" + r.Kind + ":" + r.Arg + "}" }

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseRef parses a whole value as a reference (surrounding spaces are
// ignored). A value that does not start with "${" gives ErrNotRef; one
// that does but is not a valid env, dotenv or cmd reference gives
// ErrBadRef.
func ParseRef(s string) (Ref, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "${") {
		return Ref{}, ErrNotRef
	}
	body, ok := strings.CutSuffix(s[2:], "}")
	if !ok {
		return Ref{}, fmt.Errorf("%w: missing }", ErrBadRef)
	}
	kind, arg, ok := strings.Cut(body, ":")
	if !ok {
		return Ref{}, fmt.Errorf("%w: want ${env:NAME}, ${dotenv:NAME} or ${cmd:line}", ErrBadRef)
	}
	switch kind {
	case RefEnv, RefDotenv:
		if !envNameRe.MatchString(arg) {
			return Ref{}, fmt.Errorf("%w: ${%s:} needs a variable name", ErrBadRef, kind)
		}
	case RefCmd:
		arg = strings.TrimSpace(arg)
		if arg == "" || strings.ContainsAny(arg, "\n\r\x00") {
			return Ref{}, fmt.Errorf("%w: ${cmd:} needs one command line", ErrBadRef)
		}
	default:
		return Ref{}, fmt.Errorf("%w: unknown kind %q", ErrBadRef, kind)
	}
	return Ref{Kind: kind, Arg: arg}, nil
}

// IsRef reports whether s is a valid reference.
func IsRef(s string) bool {
	_, err := ParseRef(s)
	return err == nil
}

// PromptHint is the hint every secret prompt shows: a value, or a
// reference that is stored as text and resolved on use.
const PromptHint = "value, or ${env:NAME} / ${cmd:…} / ${dotenv:NAME}"

// BadRefText is the message a prompt shows for a malformed reference. It
// is fixed words: the typed text may be half a secret.
const BadRefText = "Not a valid reference. Use ${env:NAME}, ${dotenv:NAME} or ${cmd:…} (one line), or type the value itself."

// TypedProblem checks what a user typed into a secret prompt. "" means
// accept it (a plain value or a valid reference); anything else is the
// message to show, and nothing may be saved.
func TypedProblem(s string) string {
	if _, err := ParseRef(s); err != nil && !errors.Is(err, ErrNotRef) {
		return BadRefText
	}
	return ""
}

// HasCmdRef reports whether s contains a ${cmd:...} reference anywhere.
// Project files (.termocode/*.json) must not: a command from a file that
// travels with the repository would run on the reader's machine.
func HasCmdRef(s string) bool {
	return cmdRefRe.MatchString(s)
}

var cmdRefRe = regexp.MustCompile(`\$\{\s*cmd\s*:`)

// ProjectCmdWarning is the loaders' text for a ${cmd:} in a project file.
const ProjectCmdWarning = "${cmd:} is not allowed in project files; keep it in your own configuration"

// First use of a ${cmd:} text: the app shows it once in a toast so a
// tampered index is noticed. The set lives for the whole process (Lock
// forgets cached outputs, not what the user has already been shown).
var cmdNotice struct {
	sync.Mutex
	seen   map[string]bool
	notify func(string)
}

// SetCmdNotifier sets the func called with a command text the first time
// it runs in this process (nil: none). It may run on any goroutine and
// must not block. It never gets the command's output.
func SetCmdNotifier(fn func(string)) {
	cmdNotice.Lock()
	defer cmdNotice.Unlock()
	cmdNotice.notify = fn
}

// ResetCmdNotices forgets which commands were shown (tests).
func ResetCmdNotices() {
	cmdNotice.Lock()
	defer cmdNotice.Unlock()
	cmdNotice.seen = nil
}

func noteCmd(text string) {
	cmdNotice.Lock()
	if cmdNotice.seen == nil {
		cmdNotice.seen = map[string]bool{}
	}
	first := !cmdNotice.seen[text]
	cmdNotice.seen[text] = true
	fn := cmdNotice.notify
	cmdNotice.Unlock()
	if first && fn != nil {
		fn(text)
	}
}

// DefaultCmdTimeout bounds one ${cmd:} run.
const DefaultCmdTimeout = 10 * time.Second

// Resolver turns references into values. ${cmd:} outputs are cached
// until ForgetCache (the store's Lock); ${dotenv:} values until the
// .env file changes; ${env:} is never cached.
type Resolver struct {
	Getenv    func(string) string // nil: os.Getenv
	Workspace string              // root for ${dotenv:}
	Redact    func(string) string // cleans ${cmd:} stderr in errors
	Timeout   time.Duration       // ${cmd:} limit (0: DefaultCmdTimeout)

	mu     sync.Mutex
	cmd    map[string]string
	dotenv *dotenvCache
}

type dotenvCache struct {
	path  string
	stamp fileStamp
	vals  map[string]string
}

// Resolve returns the value a reference points at. Errors name the
// reference, never a value.
func (r *Resolver) Resolve(ref Ref) (string, error) {
	switch ref.Kind {
	case RefEnv:
		get := r.Getenv
		if get == nil {
			get = os.Getenv
		}
		if v := get(ref.Arg); v != "" {
			return v, nil
		}
		return "", fmt.Errorf("%s: variable is empty", ref)
	case RefDotenv:
		if !envNameRe.MatchString(ref.Arg) {
			return "", fmt.Errorf("%w: %s", ErrBadRef, ref)
		}
		return r.resolveDotenv(ref)
	case RefCmd:
		return r.resolveCmd(ref)
	}
	return "", fmt.Errorf("%w: %s", ErrBadRef, ref)
}

// ForgetCache drops cached ${cmd:} and ${dotenv:} values.
func (r *Resolver) ForgetCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	clear(r.cmd)
	r.cmd = nil
	r.dotenv = nil
}

// resolveDotenv reads NAME from <workspace>/.env, never from a file
// outside the workspace (a link that leaves it is refused).
func (r *Resolver) resolveDotenv(ref Ref) (string, error) {
	if r.Workspace == "" {
		return "", fmt.Errorf("%s: no workspace open", ref)
	}
	ws, err := filepath.EvalSymlinks(r.Workspace)
	if err != nil {
		return "", fmt.Errorf("%s: workspace: %w", ref, err)
	}
	real, err := filepath.EvalSymlinks(filepath.Join(ws, ".env"))
	if err != nil {
		return "", fmt.Errorf("%s: no .env in the workspace", ref)
	}
	if rel, err := filepath.Rel(ws, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s: .env points outside the workspace", ref)
	}
	fi, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("%s: %w", ref, err)
	}
	st := fileStamp{fi.ModTime(), fi.Size()}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dotenv == nil || r.dotenv.path != real || r.dotenv.stamp != st {
		r.dotenv = &dotenvCache{path: real, stamp: st, vals: services.EnvFileValues(real)}
	}
	if v := r.dotenv.vals[ref.Arg]; v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%s: not set in .env", ref)
}

// resolveCmd runs `sh -c line` with stdin closed and a timeout, and
// keeps the first output line, trimmed.
func (r *Resolver) resolveCmd(ref Ref) (string, error) {
	r.mu.Lock()
	if v, ok := r.cmd[ref.Arg]; ok {
		r.mu.Unlock()
		return v, nil
	}
	r.mu.Unlock()

	noteCmd(ref.Arg)
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultCmdTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := spawn.CommandContext(ctx, "sh", "-c", ref.Arg)
	cmd.Stdin = nil // /dev/null
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("%s: timed out after %s", ref, timeout)
	}
	if err != nil {
		msg := firstLine(stderr.String())
		if r.Redact != nil {
			msg = r.Redact(msg)
		}
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		if msg != "" {
			return "", fmt.Errorf("%s failed: %v: %s", ref, err, msg)
		}
		return "", fmt.Errorf("%s failed: %v", ref, err)
	}
	v := firstLine(stdout.String())
	zero(stdout.Bytes())
	if v == "" {
		return "", fmt.Errorf("%s printed nothing", ref)
	}
	r.mu.Lock()
	if r.cmd == nil {
		r.cmd = map[string]string{}
	}
	r.cmd[ref.Arg] = v
	r.mu.Unlock()
	return v, nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}
