package devcontainer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ErrNeedsCLI: compose files and features need the devcontainer CLI;
// PlanSetup makes only image and build containers, PlanCLISetup the rest.
var ErrNeedsCLI = errors.New("this dev container uses compose or features: it needs the devcontainer CLI")

// ErrNoImage: the file names no image, Dockerfile or compose file.
var ErrNoImage = errors.New("devcontainer.json names no image or Dockerfile")

// Step is one command of the setup, in order.
type Step struct {
	Title string   // for the progress toast ("Build the image")
	Argv  []string // argv[0] is the program
	Dir   string   // local folder the command starts in ("" = none)
	Local bool     // runs on this computer, not through the engine
	// Lifecycle is true for the file's own commands (the trust card
	// lists them); false for the engine steps termocode adds.
	Lifecycle bool
	// Up is the "devcontainer up" step: its stdout is the result with
	// the container id and folder (ParseUpResult).
	Up bool
}

// Setup is how termocode makes a dev container for root and what to
// open in it afterwards.
type Setup struct {
	Name            string // container name: termocode-dc-<hash of root>
	Image           string // the image run (built for KindBuild)
	WorkspaceFolder string // the folder to open in the container
	// Create makes the container (initializeCommand first, then build
	// and run); Lifecycle runs in it after (onCreate … postAttach).
	Create    []Step
	Lifecycle []Step
	// CLI: the devcontainer CLI makes the container and runs the
	// lifecycle; Name is "" until its result names the container.
	CLI bool
}

// ContainerName is the name termocode gives the dev container of root:
// the same root always gets the same name, so it is found again.
func ContainerName(root string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return "termocode-dc-" + hex.EncodeToString(sum[:4])
}

// Label marks the containers termocode made from a devcontainer.json;
// its value is the local project folder.
const Label = "termocode.devcontainer"

// keepAlive is the command a dev container runs instead of its own
// entrypoint command, so it stays up (overrideCommand, the default).
var keepAlive = []string{"sh", "-c", "while sleep 1000; do :; done"}

// PlanSetup is the setup of c for the project at root (file is the path
// of devcontainer.json). engine is the engine command prefix ("docker",
// maybe with --context c). envFile, when set, is a 0600 file with the
// containerEnv lines: values never go into argv.
func (c Config) PlanSetup(root, file string, engine []string, envFile string) (Setup, error) {
	if len(engine) == 0 {
		engine = []string{"docker"}
	}
	switch c.Kind() {
	case KindNone:
		return Setup{}, ErrNoImage
	case KindCompose:
		return Setup{}, ErrNeedsCLI
	}
	if len(c.Features) > 0 {
		return Setup{}, ErrNeedsCLI
	}
	root = filepath.Clean(root)
	name := ContainerName(root)
	ws := c.WorkspaceFolder
	if ws == "" || !path.IsAbs(ws) {
		ws = "/workspaces/" + filepath.Base(root)
	}
	ws = path.Clean(ws)
	subst := func(s string) string {
		return strings.NewReplacer(
			"${localWorkspaceFolder}", root,
			"${localWorkspaceFolderBasename}", filepath.Base(root),
			"${containerWorkspaceFolder}", ws,
			"${containerWorkspaceFolderBasename}", path.Base(ws),
		).Replace(s)
	}
	s := Setup{Name: name, WorkspaceFolder: ws}
	eng := func(args ...string) []string { return append(append([]string(nil), engine...), args...) }
	for _, cmd := range c.commands {
		if cmd.Stage == "initializeCommand" {
			s.Create = append(s.Create, Step{Title: "initializeCommand", Argv: commandArgv(cmd, subst), Dir: root, Local: true, Lifecycle: true})
		}
	}
	s.Image = c.Image
	if c.Kind() == KindBuild {
		base := filepath.Dir(file)
		df := c.Dockerfile
		if !filepath.IsAbs(df) {
			df = filepath.Join(base, df)
		}
		ctxDir := base
		if c.BuildContext != "" {
			ctxDir = c.BuildContext
			if !filepath.IsAbs(ctxDir) {
				ctxDir = filepath.Join(base, ctxDir)
			}
		}
		s.Image = name + ":latest"
		s.Create = append(s.Create, Step{Title: "Build the image", Argv: eng("build", "-f", df, "-t", s.Image, ctxDir), Dir: root})
	}
	run := []string{"run", "-d", "--name", name, "--label", Label + "=" + root}
	if c.WorkspaceMount != "" {
		run = append(run, "--mount", subst(c.WorkspaceMount))
	} else {
		run = append(run, "-v", root+":"+ws)
	}
	run = append(run, "-w", ws)
	if envFile != "" {
		run = append(run, "--env-file", envFile)
	}
	for _, p := range c.ForwardPorts {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			run = append(run, "-p", "127.0.0.1:"+p+":"+p)
		}
	}
	for _, m := range c.Mounts {
		run = append(run, "--mount", subst(m))
	}
	for _, a := range c.RunArgs {
		run = append(run, subst(a))
	}
	run = append(run, "--entrypoint", "")
	run = append(run, s.Image)
	run = append(run, keepAlive...)
	s.Create = append(s.Create, Step{Title: "Start the container", Argv: eng(run...), Dir: root})
	for _, cmd := range c.commands {
		if cmd.Stage == "initializeCommand" {
			continue
		}
		ex := []string{"exec", "-w", ws}
		if c.RemoteUser != "" {
			ex = append(ex, "-u", c.RemoteUser)
		}
		ex = append(ex, name)
		s.Lifecycle = append(s.Lifecycle, Step{Title: cmd.Stage, Argv: eng(append(ex, commandArgv(cmd, subst)...)...), Lifecycle: true})
	}
	return s, nil
}

// commandArgv is the argv of one lifecycle command: a string runs
// through sh -c, a list runs as it is.
func commandArgv(cmd Command, subst func(string) string) []string {
	if cmd.Args != nil {
		out := make([]string, len(cmd.Args))
		for i, a := range cmd.Args {
			out[i] = subst(a)
		}
		return out
	}
	return []string{"sh", "-c", subst(cmd.Shell)}
}

// EnvFileText is the --env-file body for containerEnv (sorted, one
// KEY=value per line). A value with a newline cannot be passed this way
// and is an error.
func (c Config) EnvFileText() (string, error) {
	if len(c.ContainerEnv) == 0 {
		return "", nil
	}
	keys := make([]string, 0, len(c.ContainerEnv))
	for k := range c.ContainerEnv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := c.ContainerEnv[k]
		if strings.ContainsAny(k, "=\n") || strings.Contains(v, "\n") {
			return "", errors.New("containerEnv " + strconv.Quote(k) + " cannot be passed in an env file")
		}
		b.WriteString(k + "=" + v + "\n")
	}
	return b.String(), nil
}

// ─── trust ────────────────────────────────────────────────────────────

// trustPath is where the trusted command hashes are kept.
func trustPath() (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "termocode", "devcontainer-trust.json"), nil
}

func loadTrust() map[string]string {
	p, err := trustPath()
	if err != nil {
		return map[string]string{}
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return map[string]string{}
	}
	m := map[string]string{}
	if json.Unmarshal(b, &m) != nil {
		return map[string]string{}
	}
	return m
}

// Trusted reports whether the user already agreed to these exact
// lifecycle commands for root.
func Trusted(root string, c Config) bool {
	return loadTrust()[filepath.Clean(root)] == c.TrustHash()
}

// Trust records that the user agreed to c's lifecycle commands for root.
func Trust(root string, c Config) error {
	p, err := trustPath()
	if err != nil {
		return err
	}
	m := loadTrust()
	m[filepath.Clean(root)] = c.TrustHash()
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}
