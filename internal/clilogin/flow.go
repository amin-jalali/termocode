package clilogin

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/lspinstall"
)

// Phase is a step of the sign-in path.
type Phase int

const (
	PhaseInstalling Phase = iota
	PhaseWaitingForBrowser
	PhaseLoadingModels
	PhaseReady
	PhaseFailed
)

func (p Phase) String() string {
	switch p {
	case PhaseInstalling:
		return "installing"
	case PhaseWaitingForBrowser:
		return "waiting for browser"
	case PhaseLoadingModels:
		return "loading models"
	case PhaseReady:
		return "ready"
	case PhaseFailed:
		return "failed"
	}
	return "unknown"
}

// State is what the card shows. Only the fields of its Phase are set.
type State struct {
	Phase Phase
	// Elapsed is the time since the phase started (Installing: the timer).
	Elapsed time.Duration
	Line    string // Installing: the installer's last progress line
	// WaitingForBrowser: the sign-in link ("" until the CLI prints it;
	// never log it), the one-time code to type on its page (Codex device
	// sign-in), and whether the CLI asks for a pasted code (Flow.Write).
	URL       string
	Code      string
	NeedsCode bool
	Models    []ai.ModelInfo // Ready: the CLI's models (ai.CLIModels)
	Err       *Error         // Failed
}

// Config is one sign-in path run. Zero values are the defaults.
type Config struct {
	CLI string // ai.CLIClaude or ai.CLICodex
	// Env is the environment of the status checks and the sign-in (nil:
	// os.Environ() with tools/bin first); clistep.CleanEnv is applied.
	// Codex uses its device sign-in when it looks like SSH without a
	// display (UseDeviceAuth).
	Env []string

	// Install installs the CLI (nil: lspinstall.Install).
	Install func(context.Context, lspinstall.Tool, lspinstall.Progress) (string, error)
	// Check is the CLI's status (nil: clistep.Check).
	Check func(context.Context, string, []string) clistep.Status

	Tick      time.Duration // the Installing timer (1 s)
	PollEvery time.Duration // the status poll during the sign-in (2 s)
	ExitGrace time.Duration // how long a signed-in CLI may take to exit (10 s)
	Timeout   time.Duration // the sign-in's limit (5 min)
}

// ToolName is the lspinstall tool of a CLI ("" for an unknown one).
func ToolName(cli string) string {
	switch cli {
	case ai.CLIClaude:
		return "claude-code"
	case ai.CLICodex:
		return "codex"
	}
	return ""
}

// Flow is one running sign-in path.
type Flow struct {
	ch     chan State
	cancel context.CancelFunc

	mu   sync.Mutex
	sess *Session
}

// Run starts the sign-in path of cfg.CLI: install when missing, sign in
// when signed out, then the models. ctx ending is a Cancel.
func Run(ctx context.Context, cfg Config) *Flow {
	if cfg.Install == nil {
		cfg.Install = lspinstall.Install
	}
	if cfg.Check == nil {
		cfg.Check = clistep.Check
	}
	if cfg.Tick <= 0 {
		cfg.Tick = time.Second
	}
	if cfg.PollEvery <= 0 {
		cfg.PollEvery = 2 * time.Second
	}
	if cfg.ExitGrace <= 0 {
		cfg.ExitGrace = 10 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithCancel(ctx)
	f := &Flow{ch: make(chan State, 16), cancel: cancel}
	crash.Go("the sign-in", func() { f.run(ctx, cfg) })
	return f
}

// Updates are the flow's states; the channel closes when the flow ends
// (after Ready or Failed, or at once after Cancel).
func (f *Flow) Updates() <-chan State { return f.ch }

// Write pastes a code into the running sign-in (when NeedsCode).
func (f *Flow) Write(code string) error {
	f.mu.Lock()
	s := f.sess
	f.mu.Unlock()
	if s == nil {
		return errors.New("no sign-in is running")
	}
	return s.Write(code)
}

// Cancel stops the flow: the installer and the CLI are killed.
func (f *Flow) Cancel() { f.cancel() }

// send delivers a phase change (dropped only when the flow is canceled
// and the card has no room left). Room first: the exit grace of a
// signed-in CLI may end ctx just before Ready.
func (f *Flow) send(ctx context.Context, st State) {
	select {
	case f.ch <- st:
		return
	default:
	}
	select {
	case f.ch <- st:
	case <-ctx.Done():
	}
}

// trySend delivers a timer tick unless the card is behind.
func (f *Flow) trySend(st State) {
	select {
	case f.ch <- st:
	default:
	}
}

func (f *Flow) fail(ctx context.Context, e *Error) {
	f.send(ctx, State{Phase: PhaseFailed, Err: e})
}

func (f *Flow) run(ctx context.Context, cfg Config) {
	keep := false // a signed-in CLI still exits; its grace ends the context
	defer func() {
		if !keep {
			f.cancel()
		}
		close(f.ch)
	}()
	title := ai.CLITitle(cfg.CLI)
	if title == "" {
		f.fail(ctx, &Error{Kind: FailOther, Msg: "Unknown sign-in."})
		return
	}
	st := cfg.Check(ctx, cfg.CLI, cfg.Env)
	if ctx.Err() != nil {
		return
	}
	if st == clistep.StatusMissing {
		if !f.install(ctx, cfg, title) {
			return
		}
		if st = cfg.Check(ctx, cfg.CLI, cfg.Env); ctx.Err() != nil {
			return
		}
		if st == clistep.StatusMissing {
			f.fail(ctx, &Error{Kind: FailInstall, Msg: "Could not install " + title + ". Try again, or use an API key.",
				Detail: "installed, but " + clistep.Bin(cfg.CLI) + " is not found in the tools folder"})
			return
		}
	}
	if st != clistep.StatusReady {
		var ok bool
		if ok, keep = f.login(ctx, cfg, title); !ok {
			return
		}
	}
	f.send(ctx, State{Phase: PhaseLoadingModels})
	f.send(ctx, State{Phase: PhaseReady, Models: ai.CLIModels(cfg.CLI)})
}

// install runs the installer with a ticking timer; false: failed (a
// Failed state is sent) or canceled.
func (f *Flow) install(ctx context.Context, cfg Config, title string) bool {
	tool, ok := lspinstall.Lookup(ToolName(cfg.CLI))
	if !ok {
		f.fail(ctx, &Error{Kind: FailInstall, Msg: "Could not install " + title + ". Try again, or use an API key."})
		return false
	}
	start := time.Now()
	type result struct {
		out string
		err error
	}
	lines, res := make(chan string, 16), make(chan result, 1)
	crash.Go("the sign-in", func() {
		out, err := cfg.Install(ctx, tool, func(l string) {
			select {
			case lines <- l:
			default:
			}
		})
		res <- result{out, err}
	})
	tick := time.NewTicker(cfg.Tick)
	defer tick.Stop()
	line := ""
	f.send(ctx, State{Phase: PhaseInstalling})
	for {
		select {
		case l := <-lines:
			line = l
			f.trySend(State{Phase: PhaseInstalling, Elapsed: time.Since(start), Line: line})
		case <-tick.C:
			f.trySend(State{Phase: PhaseInstalling, Elapsed: time.Since(start), Line: line})
		case r := <-res:
			if ctx.Err() != nil {
				return false
			}
			if r.err != nil {
				f.fail(ctx, installError(title, r.out, r.err))
				return false
			}
			return true
		}
	}
}

// installError is the plain error of a failed install.
func installError(title, out string, err error) *Error {
	var mt *lspinstall.MissingToolchainError
	var off *lspinstall.OfflineError
	var un *lspinstall.UnsupportedError
	switch {
	case errors.As(err, &mt):
		return errNoNode(title)
	case errors.As(err, &off):
		return errOffline()
	case errors.As(err, &un):
		return &Error{Kind: FailInstall, Msg: "Could not install " + title + " on this system. Use an API key instead.",
			Detail: ai.Redact(err.Error())}
	}
	return &Error{Kind: FailInstall, Msg: "Could not install " + title + ". Try again, or use an API key.",
		Detail: ai.Redact(strings.TrimSpace(lastLines(out, 8) + "\n" + err.Error()))}
}

// lastLines are the last n lines of s.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// login runs the CLI's sign-in until the status is ready. ok: signed
// in; keep: the CLI still runs and a grace goroutine ends the context.
func (f *Flow) login(ctx context.Context, cfg Config, title string) (ok, keep bool) {
	judge := cfg.Env
	if judge == nil {
		judge = os.Environ()
	}
	sess, err := Start(ctx, Options{CLI: cfg.CLI, Env: cfg.Env, DeviceAuth: UseDeviceAuth(judge)})
	if err != nil {
		f.fail(ctx, &Error{Kind: FailOther, Msg: "Could not start the " + title + " sign-in. Try again, or use an API key."})
		return false, false
	}
	f.mu.Lock()
	f.sess = sess
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.sess = nil
		f.mu.Unlock()
	}()

	start := time.Now()
	cur := State{Phase: PhaseWaitingForBrowser}
	f.send(ctx, cur)
	poll := time.NewTicker(cfg.PollEvery)
	defer poll.Stop()
	timeout := time.NewTimer(cfg.Timeout)
	defer timeout.Stop()
	for {
		select {
		case ev := <-sess.Events():
			switch ev.Kind {
			case EventURL:
				cur.URL = ev.URL
			case EventCode:
				cur.Code = ev.Code
			case EventNeedsCode:
				cur.NeedsCode = true
			case EventExit:
				if ctx.Err() != nil {
					return false, false
				}
				if cfg.Check(ctx, cfg.CLI, cfg.Env) == clistep.StatusReady {
					return true, false
				}
				var e *Error
				if errors.As(ev.Err, &e) && (e.Kind == FailNoNode || e.Kind == FailOffline) {
					f.fail(ctx, e)
				} else {
					f.fail(ctx, errNotFinished())
				}
				return false, false
			}
			cur.Elapsed = time.Since(start)
			f.send(ctx, cur)
		case <-poll.C:
			if cfg.Check(ctx, cfg.CLI, cfg.Env) == clistep.StatusReady {
				crash.Go("the sign-in", func() {
					select {
					case <-sess.Done():
					case <-time.After(cfg.ExitGrace):
						sess.Cancel()
					case <-ctx.Done():
					}
					f.cancel()
				})
				return true, true
			}
		case <-timeout.C:
			sess.Cancel()
			<-sess.Done()
			f.fail(ctx, errTimeout())
			return false, false
		case <-ctx.Done():
			sess.Cancel()
			return false, false
		}
	}
}
