package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// `termocode ai -x "…"` (CMP-41): one AI turn with no UI, for scripts and
// CI. The answer goes to stdout as text, one JSON object or a JSON line
// per event; the exit code says how the turn ended. Nobody is there to
// answer a confirm card, so the default tools only read; a mutating
// tool runs only when its class is named with --allow, else it is
// declined and the model is told so.

const aiUsage = `termocode ai: run one AI turn without the UI (scripts, CI)

  termocode ai -x "<prompt>" [options]
  termocode ai exec [options] <prompt…>        (prompt "-" reads stdin)

Options:
  -o, --output text|json|stream-json   text (default): the answer, tool lines on stderr
                                       json: one result object at the end
                                       stream-json: one JSON line per event, the result last
  -m, --model <id>        the model (default: the configured chat model)
  -C, --dir <folder>      the workspace the tools work in (default: here)
  --tools read|all|none   read (default): only tools that read; all: every
                          workspace tool, mutating ones need --allow
  --allow <classes>       let these tool classes run without asking:
                          fileWrite, git, run (comma-separated; implies --tools all)
  --max-steps <n>         cap the tool steps (default: ai_max_steps or 25)
  --timeout <d>           stop after d (like 90s or 10m; default 10m)
  --rules                 add the project rules (AGENTS.md, CLAUDE.md, .termocode/rules.md)
  --stdin                 add what is piped in below the prompt

Exit codes: 0 done, 1 the model failed, 2 bad usage, 3 AI not configured,
4 timeout, 5 step limit reached (work not finished), 130 interrupted.
`

// Exit codes of `termocode ai`.
const (
	aiExitOK            = 0
	aiExitFailed        = 1
	aiExitUsage         = 2
	aiExitNotConfigured = 3
	aiExitTimeout       = 4
	aiExitStepLimit     = 5
	aiExitInterrupted   = 130
)

// aiExecStdinMax caps the piped input (--stdin, prompt "-").
const aiExecStdinMax = 256 << 10

// aiExecDetailMax caps a tool result in the stream-json and json output.
const aiExecDetailMax = 4 << 10

// aiExecAllowClasses are the classes --allow accepts: the classes of the
// workspace tools.
var aiExecAllowClasses = []string{ai.ClassFileWrite, ai.ClassGit, ai.ClassRun}

var errAINotConfigured = errors.New("AI is not configured: run \"AI: Configure Provider…\" in termocode, " +
	"or set ANTHROPIC_API_KEY / OPENAI_API_KEY (or TERMOCODE_AI_PROVIDER=ollama)")

// aiExecEnv is the resolved provider of a headless turn.
type aiExecEnv struct {
	Provider  ai.Provider
	Model     string // the model id the turn uses
	MaxSteps  int    // ai_max_steps (0: ai.DefaultMaxSteps)
	Reasoning string
}

// aiExecSetup resolves the provider like the app does (config.json, the
// secret store, env). A test seam.
var aiExecSetup = func(getenv func(string) string, model string) (aiExecEnv, error) {
	var settings ai.Settings
	if p, err := ai.ConfigPath(); err == nil {
		settings = ai.LoadSettings(p)
	}
	var creds ai.Credentials
	locked := false
	credPath, err := ai.CredentialsPath()
	if err == nil {
		if st, err := openMCPSecretStore(); err == nil {
			if st.Locked() {
				locked = true // env keys may still do; the store stays shut
			} else {
				ai.SetSecretStoreForPath(credPath, st)
			}
		}
		if !locked {
			creds, _ = ai.LoadCredentials(credPath)
		}
	}
	cfg := ai.Resolve(settings, creds, getenv)
	if cfg.Kind == ai.KindClaudeOAuth && creds.ClaudeOAuth != nil && credPath != "" {
		cfg.Token = (&ai.TokenSource{Path: credPath}).Token
	}
	if model != "" {
		cfg.Model = model
	}
	if !cfg.Configured() {
		if locked {
			return aiExecEnv{}, fmt.Errorf("%w (the secret store is locked: unlock it in termocode, or set the key in the environment)", errAINotConfigured)
		}
		return aiExecEnv{}, errAINotConfigured
	}
	return aiExecEnv{Provider: ai.NewProvider(cfg), Model: cfg.Model, MaxSteps: settings.MaxSteps, Reasoning: cfg.Reasoning}, nil
}

type aiExecOpts struct {
	prompt   string
	output   string
	model    string
	dir      string
	tools    string
	allow    map[string]bool
	maxSteps int
	timeout  time.Duration
	rules    bool
	stdin    bool
}

// aiExecTool is one tool call in the json result.
type aiExecTool struct {
	Tool     string `json:"tool"`
	Detail   string `json:"detail,omitempty"`
	Decision string `json:"decision,omitempty"`
}

// aiExecResult is the json result (and the last stream-json line).
type aiExecResult struct {
	Type       string       `json:"type"` // "result"
	OK         bool         `json:"ok"`
	ExitCode   int          `json:"exit_code"`
	Text       string       `json:"text"`
	Error      string       `json:"error,omitempty"`
	Model      string       `json:"model,omitempty"`
	Usage      *ai.Usage    `json:"usage,omitempty"`
	Tools      []aiExecTool `json:"tools,omitempty"`
	StepLimit  bool         `json:"step_limit,omitempty"`
	DurationMS int64        `json:"duration_ms"`
}

func runAI(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runAIIn(ctx, args, os.Stdin, os.Stdout, os.Stderr, os.Getenv)
}

func parseAIExec(args []string) (aiExecOpts, error) {
	o := aiExecOpts{output: "text", tools: "", allow: map[string]bool{}, timeout: 10 * time.Minute}
	if len(args) > 0 && args[0] == "exec" {
		args = args[1:]
	}
	var parts []string
	val := func(i *int, name string) (string, error) {
		a := args[*i]
		if k, v, ok := strings.Cut(a, "="); ok && strings.HasPrefix(k, "--") {
			return v, nil
		}
		if *i+1 >= len(args) {
			return "", fmt.Errorf("%s needs a value", name)
		}
		*i++
		return args[*i], nil
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, _, _ := strings.Cut(a, "=")
		var v string
		var err error
		switch name {
		case "-x", "--exec":
			if v, err = val(&i, name); err == nil {
				parts = append([]string{v}, parts...)
			}
		case "-o", "--output":
			if v, err = val(&i, name); err == nil {
				o.output = v
			}
		case "-m", "--model":
			if v, err = val(&i, name); err == nil {
				o.model = v
			}
		case "-C", "--dir":
			if v, err = val(&i, name); err == nil {
				o.dir = v
			}
		case "--tools":
			if v, err = val(&i, name); err == nil {
				o.tools = v
			}
		case "--allow":
			if v, err = val(&i, name); err == nil {
				for _, c := range strings.Split(v, ",") {
					c = strings.TrimSpace(c)
					if c == "" {
						continue
					}
					cls, ok := aiExecClass(c)
					if !ok {
						return o, fmt.Errorf("--allow: unknown class %q (one of %s)", c, strings.Join(aiExecAllowClasses, ", "))
					}
					o.allow[cls] = true
				}
			}
		case "--max-steps":
			if v, err = val(&i, name); err == nil {
				n, perr := strconv.Atoi(v)
				if perr != nil || n <= 0 {
					return o, fmt.Errorf("bad --max-steps %q", v)
				}
				o.maxSteps = n
			}
		case "--timeout":
			if v, err = val(&i, name); err == nil {
				d, perr := time.ParseDuration(v)
				if perr != nil || d <= 0 {
					return o, fmt.Errorf("bad --timeout %q (like 90s or 10m)", v)
				}
				o.timeout = d
			}
		case "--rules":
			o.rules = true
		case "--stdin":
			o.stdin = true
		default:
			if a != "-" && strings.HasPrefix(a, "-") {
				return o, fmt.Errorf("unknown option %s", a)
			}
			parts = append(parts, a)
		}
		if err != nil {
			return o, err
		}
	}
	switch o.output {
	case "text", "json", "stream-json":
	default:
		return o, fmt.Errorf("--output must be text, json or stream-json")
	}
	switch o.tools {
	case "":
		o.tools = "read"
		if len(o.allow) > 0 {
			o.tools = "all"
		}
	case "read", "none":
		if len(o.allow) > 0 {
			return o, fmt.Errorf("--allow needs --tools all")
		}
	case "all":
	default:
		return o, fmt.Errorf("--tools must be read, all or none")
	}
	o.prompt = strings.TrimSpace(strings.Join(parts, " "))
	if o.prompt == "" {
		return o, errors.New("no prompt")
	}
	return o, nil
}

// aiExecClass maps an --allow word to its class (any case).
func aiExecClass(s string) (string, bool) {
	for _, c := range aiExecAllowClasses {
		if strings.EqualFold(c, s) {
			return c, true
		}
	}
	return "", false
}

func runAIIn(ctx context.Context, args []string, in io.Reader, out, errw io.Writer, getenv func(string) string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		fmt.Fprint(out, aiUsage)
		return aiExitOK
	}
	o, err := parseAIExec(args)
	if err != nil {
		fmt.Fprintf(errw, "termocode ai: %v\n\n%s", err, aiUsage)
		return aiExitUsage
	}
	start := time.Now()
	res := aiExecResult{Type: "result"}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	finish := func(code int, msg string) int {
		res.ExitCode, res.OK = code, code == aiExitOK
		if msg != "" {
			res.Error = ai.Redact(msg)
		}
		res.DurationMS = time.Since(start).Milliseconds()
		switch o.output {
		case "json", "stream-json":
			_ = enc.Encode(res)
		default:
			if res.Text != "" && !strings.HasSuffix(res.Text, "\n") {
				fmt.Fprintln(out)
			}
		}
		if msg != "" {
			fmt.Fprintf(errw, "termocode ai: %s\n", res.Error)
		}
		return code
	}

	// The prompt and the piped input.
	if o.prompt == "-" || o.stdin {
		b, err := io.ReadAll(io.LimitReader(in, aiExecStdinMax))
		if err != nil {
			return finish(aiExitUsage, "read stdin: "+err.Error())
		}
		if o.prompt == "-" {
			o.prompt = strings.TrimSpace(string(b))
			if o.prompt == "" {
				return finish(aiExitUsage, "no prompt on stdin")
			}
		} else if s := strings.TrimRight(string(b), "\n"); s != "" {
			o.prompt += "\n\n<stdin>\n" + s + "\n</stdin>"
		}
	}

	root := o.dir
	if root == "" {
		root = "."
	}
	root, err = filepath.Abs(root)
	if err == nil {
		var fi os.FileInfo
		if fi, err = os.Stat(root); err == nil && !fi.IsDir() {
			err = errors.New("not a folder")
		}
	}
	if err != nil {
		return finish(aiExitUsage, fmt.Sprintf("--dir %s: %v", o.dir, err))
	}

	env, err := aiExecSetup(getenv, o.model)
	if err != nil {
		return finish(aiExitNotConfigured, err.Error())
	}
	res.Model = env.Model

	var tools []ai.Tool
	if o.tools != "none" {
		for _, t := range ai.WorkspaceTools(ai.Workspace{Root: root}) {
			if o.tools == "read" && (t.Mutating || !t.ReadOnly) {
				continue
			}
			tools = append(tools, t)
		}
	}
	native := env.Provider.SupportsTools()
	base := "You are termocode's AI assistant, run headless from a script or CI job: no person watches this turn " +
		"and nobody can answer a question, so finish the task on your own and end with the answer. " +
		"Reply in plain text (Markdown is fine). The workspace is " + root + "."
	if o.tools == "all" {
		if len(o.allow) == 0 {
			base += " Mutating tools are declined in this mode."
		} else {
			var cls []string
			for _, c := range aiExecAllowClasses {
				if o.allow[c] {
					cls = append(cls, c)
				}
			}
			base += " Only these mutating tool classes may run: " + strings.Join(cls, ", ") + "; others are declined."
		}
	}
	if o.rules {
		if r := ai.LoadProjectRules(root); r.Text != "" {
			base += "\n\nProject rules (" + strings.Join(r.Files, ", ") + "):\n" + r.Text
		}
	}
	steps := o.maxSteps
	if steps <= 0 {
		steps = env.MaxSteps
	}
	if steps <= 0 {
		steps = ai.DefaultMaxSteps
	}
	agent := &ai.Agent{
		Provider:    env.Provider,
		Tools:       tools,
		Model:       o.model,
		MaxSteps:    steps,
		Native:      native,
		AutoApprove: o.allow,
		// Headless: nobody to ask, so every call that would ask is declined.
		Confirm:   func(context.Context, ai.Tool, string) bool { return false },
		Reasoning: env.Reasoning,
	}
	history := []ai.Message{
		{Role: ai.RoleSystem, Content: ai.SystemPrompt(base, tools, native)},
		{Role: ai.RoleUser, Content: o.prompt},
	}

	tctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	line := func(v map[string]any) {
		if o.output == "stream-json" {
			_ = enc.Encode(v)
		}
	}
	line(map[string]any{"type": "start", "model": env.Model, "dir": root, "tools": o.tools})
	var text strings.Builder
	var failed string
	agent.Run(tctx, history, func(ev ai.Event) {
		switch ev.Kind {
		case ai.EventText:
			text.WriteString(ev.Text)
			if o.output == "text" {
				fmt.Fprint(out, ev.Text)
			}
			line(map[string]any{"type": "text", "text": ev.Text})
		case ai.EventToolStart:
			if o.output == "text" {
				fmt.Fprintf(errw, "» %s\n", ai.Redact(ev.Detail))
			}
			line(map[string]any{"type": "tool_start", "tool": ev.Tool, "label": ev.Label, "detail": ai.Redact(ev.Detail)})
		case ai.EventToolResult:
			d := aiExecCap(ai.Redact(ev.Detail))
			res.Tools = append(res.Tools, aiExecTool{Tool: ev.Tool, Detail: d, Decision: ev.Decision})
			if o.output == "text" && ev.Decision == "user denied" {
				fmt.Fprintf(errw, "  declined: %s (allow it with --allow)\n", ev.Tool)
			}
			line(map[string]any{"type": "tool_result", "tool": ev.Tool, "detail": d, "gate": ev.Gate, "decision": ev.Decision})
		case ai.EventRetry:
			line(map[string]any{"type": "retry", "attempt": ev.Attempt, "attempts": ev.Attempts, "detail": ev.Detail})
		case ai.EventFallback:
			line(map[string]any{"type": "fallback", "model": ev.Label, "detail": ev.Detail})
		case ai.EventDone:
			res.Usage = ev.Usage
		case ai.EventError:
			res.Usage = ev.Usage
			failed = ev.Detail
			if failed == "" {
				failed = "the AI turn failed"
			}
		}
	})
	res.Text = text.String()
	switch {
	case ctx.Err() != nil:
		return finish(aiExitInterrupted, "interrupted")
	case errors.Is(tctx.Err(), context.DeadlineExceeded):
		return finish(aiExitTimeout, "timed out after "+o.timeout.String())
	case failed != "":
		return finish(aiExitFailed, failed)
	case strings.HasSuffix(res.Text, ai.StepLimitText(steps)):
		res.StepLimit = true
		return finish(aiExitStepLimit, fmt.Sprintf("stopped at the step limit (%d tool steps); the work is not finished", steps))
	}
	return finish(aiExitOK, "")
}

func aiExecCap(s string) string {
	if len(s) <= aiExecDetailMax {
		return s
	}
	cut := aiExecDetailMax
	for cut > 0 && cut < len(s) && s[cut]&0xC0 == 0x80 {
		cut-- // not in the middle of a UTF-8 rune
	}
	return s[:cut] + fmt.Sprintf("… [%d more bytes]", len(s)-cut)
}
