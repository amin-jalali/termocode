package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"golang.org/x/term"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/lazyre"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/secrets"
)

// `termocode mcp add | list | get | remove` (DI-9): manage the servers
// in mcp.json without the UI. Secret values are read from stdin or a
// hidden prompt, go to the secret store, and are never printed; mcp.json
// gets ${secret:NAME}. The bare `termocode mcp` stays the serve bridge.

const mcpAdminUsage = `Usage:
  termocode mcp add [options] <name> -- <command> [args...]
  termocode mcp add [options] [<name>] --npm|--pip|--image <package> [-- args...]
  termocode mcp add [options] [<name>] <https://…/mcp>
  termocode mcp add [options] <json> | -        (an mcpServers block; - reads stdin)
  termocode mcp list [--json] [--dir d]
  termocode mcp get <name> [--dir d]
  termocode mcp remove <name> [--scope user|project] [--dir d]

Options of add:
  --scope user|project   user: your mcp.json (default); project: <dir>/.termocode/mcp.json
  --env NAME=VALUE       a plain environment value (never a token)
  --secret NAME          an environment value kept in the secret store; it is
                         read from stdin (one line each) or a hidden prompt
  --share user           one process serves every project (user scope only)
  --dir d                the project folder (default: the current folder)

A project server asks you to trust it in termocode before it first starts.
`

// mcpAdminCommands are the words that are not the serve bridge.
var mcpAdminCommands = map[string]bool{"add": true, "list": true, "get": true, "remove": true}

// runMCPTop is `termocode mcp …`: the admin commands, else the bridge.
func runMCPTop(args []string) int {
	cwd, _ := os.Getwd()
	if code, ok := runMCPAdmin(args, os.Stdin, os.Stdout, os.Stderr, cwd); ok {
		return code
	}
	return runMCP(args)
}

// runMCPAdmin runs an admin command; ok=false: args are not one.
func runMCPAdmin(args []string, in io.Reader, out, errw io.Writer, cwd string) (int, bool) {
	if len(args) == 0 || !mcpAdminCommands[args[0]] {
		return 0, false
	}
	a := &mcpAdmin{in: in, out: out, errw: errw, cwd: cwd}
	err := a.run(args[0], args[1:])
	if errors.Is(err, errAdminHelp) {
		fmt.Fprint(out, mcpAdminUsage)
		return mcpExitOK, true
	}
	if err != nil {
		fmt.Fprintf(errw, "termocode mcp %s: %s\n", args[0], ai.Redact(err.Error()))
		var ue usageError
		if errors.As(err, &ue) {
			fmt.Fprint(errw, "\n"+mcpAdminUsage)
			return mcpExitUsage, true
		}
		return mcpExitLost, true
	}
	return mcpExitOK, true
}

var errAdminHelp = errors.New("help")

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

type mcpAdmin struct {
	in        io.Reader
	lines     *bufio.Reader
	out, errw io.Writer
	cwd       string

	scope    string
	dir      string
	env      map[string]string
	secrets  []string
	kind     string
	pkg      string
	asJSON   bool
	share    string
	pos      []string
	rest     []string
	restSeen bool
}

func (a *mcpAdmin) parse(args []string) error {
	a.env = map[string]string{}
	next := func(i *int, flag string) (string, error) {
		if *i+1 >= len(args) {
			return "", usageError{flag + " needs a value"}
		}
		*i++
		return args[*i], nil
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		flag, val, hasVal := strings.Cut(arg, "=")
		if !strings.HasPrefix(arg, "--") || arg == "--" {
			hasVal = false
			flag = arg
		}
		get := func() (string, error) {
			if hasVal {
				return val, nil
			}
			return next(&i, flag)
		}
		var err error
		switch flag {
		case "--":
			a.rest, a.restSeen = args[i+1:], true
			return nil
		case "-h", "--help", "help":
			return errAdminHelp
		case "--scope":
			a.scope, err = get()
			if err == nil && a.scope != mcp.SourceUser && a.scope != mcp.SourceProject {
				err = usageError{"--scope is user or project"}
			}
		case "--dir":
			a.dir, err = get()
		case "--env":
			var kv string
			kv, err = get()
			// The value goes in the file: only the name ever appears in
			// an error.
			k, v, ok := strings.Cut(kv, "=")
			if err == nil && !ok {
				err = usageError{"--env needs NAME=VALUE"}
			}
			if err == nil {
				a.env[k] = v
			}
		case "--secret":
			var n string
			n, err = get()
			a.secrets = append(a.secrets, n)
		case "--npm", "--pip", "--image":
			if a.kind != "" {
				return usageError{"use one of --npm, --pip and --image"}
			}
			a.kind = strings.TrimPrefix(flag, "--")
			a.pkg, err = get()
		case "--share":
			a.share, err = get()
			if err == nil && a.share != mcp.ShareUser {
				err = usageError{"--share can only be user"}
			}
		case "--json":
			a.asJSON = true
		default:
			if strings.HasPrefix(arg, "--") {
				return usageError{fmt.Sprintf("unknown option %s", flag)}
			}
			a.pos = append(a.pos, arg)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *mcpAdmin) run(cmd string, args []string) error {
	if err := a.parse(args); err != nil {
		return err
	}
	switch cmd {
	case "add":
		return a.add()
	case "list":
		return a.list()
	case "get":
		return a.get()
	case "remove":
		return a.remove()
	}
	return usageError{"unknown command"}
}

func (a *mcpAdmin) projectRoot() string {
	if a.dir != "" {
		return a.dir
	}
	return a.cwd
}

func (a *mcpAdmin) paths() (user, project string, err error) {
	dir, err := ai.ConfigDir()
	if err != nil {
		return "", "", err
	}
	return mcp.UserPath(dir), mcp.ProjectPath(a.projectRoot()), nil
}

func (a *mcpAdmin) target() (string, error) {
	user, project, err := a.paths()
	if a.scope == mcp.SourceProject {
		if info, serr := os.Stat(a.projectRoot()); serr != nil || !info.IsDir() {
			return "", fmt.Errorf("%s is not a folder", a.projectRoot())
		}
		return project, err
	}
	return user, err
}

// ── add ────────────────────────────────────────────────────────────────

func (a *mcpAdmin) add() error {
	path, err := a.target()
	if err != nil {
		return err
	}
	if a.kind != "" || a.restSeen {
		return a.addLocal(path)
	}
	if len(a.env) > 0 || len(a.secrets) > 0 || a.share != "" {
		return usageError{"--env, --secret and --share are for a local server (-- <command>)"}
	}
	var text, name string
	switch len(a.pos) {
	case 1:
		text = a.pos[0]
	case 2:
		name, text = a.pos[0], a.pos[1]
	default:
		return usageError{"give a name and a command, a URL or an mcpServers block"}
	}
	fromArgv := text != "-"
	if text == "-" {
		b, err := io.ReadAll(io.LimitReader(a.in, 1<<20))
		if err != nil {
			return err
		}
		text = string(b)
	}
	list, problems, err := mcp.ParseInstall(text)
	if err != nil {
		return err
	}
	if name != "" {
		if len(list) != 1 || strings.HasPrefix(strings.TrimSpace(text), "{") {
			return usageError{"a name goes only with a URL"}
		}
		list[0].Name = name
	}
	moved := false
	var added []string
	for _, im := range list {
		if err := a.free(path, im.Name); err != nil {
			problems = append(problems, im.Name+": "+err.Error())
			continue
		}
		if len(im.Secrets) > 0 {
			moved = true
			if err := a.storeSecrets(im.Secrets); err != nil {
				return err
			}
		}
		if err := mcp.AddServer(path, im.Name, im.Entry); err != nil {
			problems = append(problems, im.Name+": "+err.Error())
			continue
		}
		added = append(added, im.Name)
	}
	for _, p := range problems {
		fmt.Fprintf(a.errw, "skipped %s\n", p)
	}
	if len(added) == 0 {
		return errors.New("nothing added")
	}
	a.done(path, added)
	if moved && fromArgv {
		fmt.Fprintln(a.out, "Tokens moved to the secret store. Next time use `termocode mcp add -` and paste on stdin, so they stay out of your shell history.")
	}
	return nil
}

func (a *mcpAdmin) addLocal(path string) error {
	if a.share != "" && a.scope == mcp.SourceProject {
		return usageError{"--share user is only for your own mcp.json"}
	}
	spec := mcp.LocalSpec{Kind: a.kind, Env: a.env, Secrets: a.secrets, Share: a.share}
	switch {
	case a.kind != "":
		spec.Command, spec.Args = a.pkg, a.rest
	case len(a.rest) > 0:
		spec.Command, spec.Args = a.rest[0], a.rest[1:]
	default:
		return usageError{"-- needs a command"}
	}
	switch len(a.pos) {
	case 0:
		spec.Name = mcp.SuggestName(spec.Command)
	case 1:
		spec.Name = a.pos[0]
	default:
		return usageError{"put the command after --"}
	}
	entry, err := spec.Entry()
	if err != nil {
		return err
	}
	if err := a.free(path, spec.Name); err != nil {
		return err
	}
	if len(spec.Secrets) > 0 {
		vals := map[string]string{}
		for _, env := range spec.Secrets {
			v, err := a.readSecret(env)
			if err != nil {
				return err
			}
			vals[mcp.LocalSecretName(spec.Name, env)] = v
		}
		if err := a.storeSecrets(vals); err != nil {
			return err
		}
	}
	if err := mcp.AddServer(path, spec.Name, entry); err != nil {
		return err
	}
	a.done(path, []string{spec.Name})
	return nil
}

// free fails when path already has name.
func (a *mcpAdmin) free(path, name string) error {
	src := mcp.SourceUser
	if a.scope == mcp.SourceProject {
		src = mcp.SourceProject
	}
	list, _ := mcp.LoadAll(pick(src == mcp.SourceUser, path), pick(src == mcp.SourceProject, path))
	for _, s := range list {
		if s.Name == name {
			return fmt.Errorf("%s already has a server named %s (remove it first)", path, name)
		}
	}
	return nil
}

func pick(ok bool, s string) string {
	if ok {
		return s
	}
	return ""
}

func (a *mcpAdmin) done(path string, names []string) {
	scope := "user"
	if a.scope == mcp.SourceProject {
		scope = "project"
	}
	fmt.Fprintf(a.out, "Added %s to %s (%s).\n", strings.Join(names, ", "), path, scope)
	if scope == "project" {
		fmt.Fprintln(a.out, "termocode asks you to trust a project server before it first starts.")
	} else {
		fmt.Fprintln(a.out, "It starts on the first chat message that needs it.")
	}
}

// readSecret reads one value: a hidden prompt on a terminal, else one
// line of stdin.
func (a *mcpAdmin) readSecret(env string) (string, error) {
	var v string
	if f, ok := a.in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprintf(a.errw, "Value for %s (hidden): ", env)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(a.errw)
		if err != nil {
			return "", err
		}
		v = string(b)
	} else {
		if a.lines == nil {
			a.lines = bufio.NewReader(a.in)
		}
		line, err := a.lines.ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("no value for %s on stdin (one line per --secret)", env)
		}
		v = line
	}
	v = strings.TrimRight(v, "\r\n")
	if strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("the value for %s is empty", env)
	}
	if msg := secrets.TypedProblem(v); msg != "" {
		return "", fmt.Errorf("%s: %s", env, msg)
	}
	ai.RegisterSecret(v)
	return v, nil
}

// openMCPSecretStore opens the store the app uses (secret_store of
// config.json). Test seam.
var openMCPSecretStore = func() (secrets.Store, error) {
	dir, err := ai.ConfigDir()
	if err != nil {
		return nil, err
	}
	path, err := ai.CredentialsPath()
	if err != nil {
		return nil, err
	}
	setting := "auto"
	if cp, err := ai.ConfigPath(); err == nil {
		var c struct {
			SecretStore string `json:"secret_store"`
		}
		if b, err := os.ReadFile(cp); err == nil && json.Unmarshal(b, &c) == nil && strings.TrimSpace(c.SecretStore) != "" {
			setting = strings.ToLower(strings.TrimSpace(c.SecretStore))
		}
	}
	return secrets.Open(secrets.Options{Dir: dir, PlainPath: path, Backend: setting, KeepPlainFile: true,
		Register: ai.RegisterSecret, Redact: ai.Redact})
}

func (a *mcpAdmin) unlockedStore() (secrets.Store, error) {
	st, err := openMCPSecretStore()
	if err != nil {
		return nil, fmt.Errorf("could not open the secret store: %w", err)
	}
	if !st.Locked() {
		return st, nil
	}
	f, ok := a.in.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return nil, errors.New("the secret store is locked: run this in a terminal to type its passphrase")
	}
	fmt.Fprint(a.errw, "Passphrase of the secret store (hidden): ")
	b, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(a.errw)
	if err != nil {
		return nil, err
	}
	if err := st.Unlock(string(b)); err != nil {
		return nil, err
	}
	return st, nil
}

func (a *mcpAdmin) storeSecrets(vals map[string]string) error {
	st, err := a.unlockedStore()
	if err != nil {
		return err
	}
	defer st.Lock()
	names := make([]string, 0, len(vals))
	for k := range vals {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		ai.RegisterSecret(vals[k])
		if err := st.Set("mcp.secret."+k, vals[k]); err != nil {
			return fmt.Errorf("could not store secret %s: %w", k, err)
		}
	}
	return nil
}

// ── list / get / remove ────────────────────────────────────────────────

func (a *mcpAdmin) servers() ([]mcp.Server, error) {
	user, project, err := a.paths()
	if err != nil {
		return nil, err
	}
	list, warns := mcp.LoadAll(user, project)
	for _, w := range warns {
		fmt.Fprintln(a.errw, "warning: "+ai.Redact(w))
	}
	return list, nil
}

// tokenRe hides values that look like tokens in printed args and URLs.
var tokenRe = lazyre.New(`(?i)(ghp_|github_pat_|glpat-|xox[pbas]-|sk-|lin_api_)[A-Za-z0-9._-]+`)

func hideTokens(s string) string { return tokenRe.ReplaceAllString(ai.Redact(s), "•••") }

func target(s mcp.Server) string {
	if s.Type == mcp.TypeHTTP {
		return hideTokens(s.URL)
	}
	return hideTokens(strings.TrimSpace(s.Command + " " + strings.Join(s.Args, " ")))
}

func status(s mcp.Server) string {
	switch {
	case s.Problem != "":
		return s.Problem
	case s.Disabled:
		return "off"
	}
	return "ok"
}

func (a *mcpAdmin) list() error {
	list, err := a.servers()
	if err != nil {
		return err
	}
	if a.asJSON {
		type row struct {
			Name   string   `json:"name"`
			Scope  string   `json:"scope"`
			Type   string   `json:"type"`
			Target string   `json:"target"`
			Env    []string `json:"env,omitempty"`
			Status string   `json:"status"`
			Path   string   `json:"path"`
		}
		rows := []row{}
		for _, s := range list {
			rows = append(rows, row{s.Name, s.Source, s.Type, target(s), sortedKeys(s.Env), status(s), s.SourcePath})
		}
		b, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Fprintln(a.out, string(b))
		return nil
	}
	if len(list) == 0 {
		fmt.Fprintln(a.out, "No MCP servers. Add one: termocode mcp add --help")
		return nil
	}
	fmt.Fprintf(a.out, "%-20s %-8s %-6s %-8s %s\n", "NAME", "SCOPE", "TYPE", "STATUS", "COMMAND / URL")
	for _, s := range list {
		fmt.Fprintf(a.out, "%-20s %-8s %-6s %-8s %s\n", s.Name, s.Source, s.Type, status(s), target(s))
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// valueKind says where a value comes from, never the value.
func valueKind(v string) string {
	switch {
	case strings.Contains(v, "${secret:"):
		return "secret (in the store)"
	case strings.Contains(v, "${"):
		return "from " + v
	}
	return "set in the file"
}

func (a *mcpAdmin) find(list []mcp.Server) (mcp.Server, error) {
	if len(a.pos) != 1 {
		return mcp.Server{}, usageError{"give one server name"}
	}
	for _, s := range list {
		if s.Name == a.pos[0] {
			return s, nil
		}
	}
	return mcp.Server{}, fmt.Errorf("no server named %s (termocode mcp list)", a.pos[0])
}

func (a *mcpAdmin) get() error {
	list, err := a.servers()
	if err != nil {
		return err
	}
	s, err := a.find(list)
	if err != nil {
		return err
	}
	w := a.out
	fmt.Fprintf(w, "Name:    %s\nScope:   %s (%s)\nType:    %s\nStatus:  %s\n", s.Name, s.Source, s.SourcePath, s.Type, status(s))
	if s.Type == mcp.TypeHTTP {
		fmt.Fprintf(w, "URL:     %s\n", hideTokens(s.URL))
	} else {
		fmt.Fprintf(w, "Command: %s\n", hideTokens(s.Command))
		if len(s.Args) > 0 {
			fmt.Fprintf(w, "Args:    %s\n", hideTokens(strings.Join(s.Args, " ")))
		}
	}
	for _, sec := range []struct {
		title string
		m     map[string]string
	}{{"Env", s.Env}, {"Headers", s.Headers}} {
		if len(sec.m) == 0 {
			continue
		}
		fmt.Fprintf(w, "%s:\n", sec.title)
		for _, k := range sortedKeys(sec.m) {
			fmt.Fprintf(w, "  %s: %s\n", k, valueKind(sec.m[k]))
		}
	}
	return nil
}

func (a *mcpAdmin) remove() error {
	list, err := a.servers()
	if err != nil {
		return err
	}
	if len(a.pos) != 1 {
		return usageError{"give one server name"}
	}
	name := a.pos[0]
	user, project, _ := a.paths()
	var paths []string
	var gone mcp.Server
	for _, f := range []struct{ path, scope string }{{user, mcp.SourceUser}, {project, mcp.SourceProject}} {
		if a.scope != "" && a.scope != f.scope {
			continue
		}
		all, _ := mcp.LoadAll(pick(f.scope == mcp.SourceUser, f.path), pick(f.scope == mcp.SourceProject, f.path))
		for _, s := range all {
			if s.Name == name {
				paths, gone = append(paths, f.path), s
			}
		}
	}
	switch len(paths) {
	case 0:
		return fmt.Errorf("no server named %s (termocode mcp list)", name)
	case 2:
		return usageError{name + " is in both files: add --scope user or --scope project"}
	}
	if err := mcp.RemoveServer(paths[0], name); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Removed %s from %s.\n", name, paths[0])
	// Its secrets go too, unless another entry still uses them.
	used := map[string]bool{}
	for _, s := range list {
		if s.Name != name || s.SourcePath != paths[0] {
			for _, n := range s.SecretNames() {
				used[n] = true
			}
		}
	}
	var drop []string
	for _, n := range gone.SecretNames() {
		if !used[n] {
			drop = append(drop, n)
		}
	}
	if len(drop) == 0 {
		return nil
	}
	st, err := a.unlockedStore()
	if err == nil {
		defer st.Lock()
		for _, n := range drop {
			if err = st.Delete("mcp.secret." + n); err != nil {
				break
			}
		}
	}
	if err != nil {
		fmt.Fprintf(a.errw, "Its secrets stay in the store (%s): %s\n", strings.Join(drop, ", "), ai.Redact(err.Error()))
	}
	return nil
}
