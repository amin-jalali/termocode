package devcontainer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/jsonc"
)

// Kind says how the container is made.
type Kind int

const (
	KindNone    Kind = iota // no image, build or compose
	KindImage               // "image"
	KindBuild               // "build" or the old "dockerFile"
	KindCompose             // "dockerComposeFile"
)

// Stages are the lifecycle keys in the order they run.
var Stages = []string{
	"initializeCommand",
	"onCreateCommand",
	"updateContentCommand",
	"postCreateCommand",
	"postStartCommand",
	"postAttachCommand",
}

// Config is the part of devcontainer.json termocode uses.
type Config struct {
	Name            string
	Image           string
	Dockerfile      string
	BuildContext    string
	ComposeFiles    []string
	Service         string
	Features        map[string]json.RawMessage
	WorkspaceFolder string
	WorkspaceMount  string
	RemoteUser      string
	ContainerEnv    map[string]string
	RemoteEnv       map[string]string
	ForwardPorts    []string
	RunArgs         []string
	Mounts          []string

	commands []Command
}

// Command is one lifecycle command. Exactly one of Shell or Args is set:
// a string runs through a shell, an array runs as argv.
type Command struct {
	Stage string
	Name  string // key in the object form, else ""
	Shell string
	Args  []string
	Local bool // initializeCommand runs on this computer
}

// String is the command as the trust card shows it.
func (c Command) String() string {
	if c.Args != nil {
		return strings.Join(c.Args, " ")
	}
	return c.Shell
}

// Where says where the command runs, in words for the trust card.
func (c Command) Where() string {
	if c.Local {
		return "this computer"
	}
	return "the new container"
}

type raw struct {
	Name              string                     `json:"name"`
	Image             string                     `json:"image"`
	DockerFile        string                     `json:"dockerFile"`
	Context           string                     `json:"context"`
	Build             *rawBuild                  `json:"build"`
	DockerComposeFile json.RawMessage            `json:"dockerComposeFile"`
	Service           string                     `json:"service"`
	Features          map[string]json.RawMessage `json:"features"`
	WorkspaceFolder   string                     `json:"workspaceFolder"`
	WorkspaceMount    string                     `json:"workspaceMount"`
	RemoteUser        string                     `json:"remoteUser"`
	ContainerEnv      map[string]string          `json:"containerEnv"`
	RemoteEnv         map[string]string          `json:"remoteEnv"`
	ForwardPorts      []json.RawMessage          `json:"forwardPorts"`
	RunArgs           []string                   `json:"runArgs"`
	Mounts            []json.RawMessage          `json:"mounts"`
}

type rawBuild struct {
	Dockerfile string `json:"dockerfile"`
	Context    string `json:"context"`
}

// Parse reads devcontainer.json source (comments and trailing commas allowed).
func Parse(src []byte) (Config, error) {
	clean := jsonc.Blank(src)
	var r raw
	if err := json.Unmarshal(clean, &r); err != nil {
		return Config{}, fmt.Errorf("devcontainer.json: %w", err)
	}
	c := Config{
		Name: r.Name, Image: r.Image, Service: r.Service, Features: r.Features,
		WorkspaceFolder: r.WorkspaceFolder, WorkspaceMount: r.WorkspaceMount,
		RemoteUser: r.RemoteUser, ContainerEnv: r.ContainerEnv, RemoteEnv: r.RemoteEnv,
		RunArgs: r.RunArgs, Dockerfile: r.DockerFile, BuildContext: r.Context,
	}
	if r.Build != nil {
		if r.Build.Dockerfile != "" {
			c.Dockerfile = r.Build.Dockerfile
		}
		if r.Build.Context != "" {
			c.BuildContext = r.Build.Context
		}
	}
	var err error
	if c.ComposeFiles, err = stringOrList(r.DockerComposeFile); err != nil {
		return Config{}, fmt.Errorf("devcontainer.json: dockerComposeFile: %w", err)
	}
	for _, p := range r.ForwardPorts {
		s, err := portString(p)
		if err != nil {
			return Config{}, fmt.Errorf("devcontainer.json: forwardPorts: %w", err)
		}
		c.ForwardPorts = append(c.ForwardPorts, s)
	}
	for _, m := range r.Mounts {
		s, err := mountString(m)
		if err != nil {
			return Config{}, fmt.Errorf("devcontainer.json: mounts: %w", err)
		}
		c.Mounts = append(c.Mounts, s)
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(clean, &all); err != nil {
		return Config{}, fmt.Errorf("devcontainer.json: %w", err)
	}
	for _, st := range Stages {
		cmds, err := stageCommands(st, all[st])
		if err != nil {
			return Config{}, fmt.Errorf("devcontainer.json: %s: %w", st, err)
		}
		c.commands = append(c.commands, cmds...)
	}
	return c, nil
}

// Kind says how the container is made; compose wins over build over image.
func (c Config) Kind() Kind {
	switch {
	case len(c.ComposeFiles) > 0:
		return KindCompose
	case c.Dockerfile != "":
		return KindBuild
	case c.Image != "":
		return KindImage
	}
	return KindNone
}

// NeedsCLI is true when only the devcontainer CLI can make this container
// (compose or features); image and build work with plain docker.
func (c Config) NeedsCLI() bool {
	return c.Kind() == KindCompose || len(c.Features) > 0
}

// Plan lists every lifecycle command in run order. Empty commands are left out.
func (c Config) Plan() []Command {
	return append([]Command(nil), c.commands...)
}

// TrustHash is a stable hash of the lifecycle commands only, so the trust
// card asks again when a command changes but not when other fields do.
func (c Config) TrustHash() string {
	h := sha256.New()
	for _, cmd := range c.commands {
		b, _ := json.Marshal([]any{cmd.Stage, cmd.Name, cmd.Shell, cmd.Args, cmd.Local})
		h.Write(b)
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Find returns the project's devcontainer.json path, or "" when none.
func Find(root string) string {
	for _, p := range []string{
		filepath.Join(root, ".devcontainer", "devcontainer.json"),
		filepath.Join(root, ".devcontainer.json"),
	} {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

func isNull(b json.RawMessage) bool {
	t := bytes.TrimSpace(b)
	return len(t) == 0 || string(t) == "null"
}

func stringOrList(b json.RawMessage) ([]string, error) {
	if isNull(b) {
		return nil, nil
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		if s == "" {
			return nil, nil
		}
		return []string{s}, nil
	}
	var l []string
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, errors.New("want a string or a list of strings")
	}
	return l, nil
}

func portString(b json.RawMessage) (string, error) {
	var n json.Number
	if json.Unmarshal(b, &n) == nil {
		if _, err := strconv.Atoi(n.String()); err != nil {
			return "", fmt.Errorf("bad port %s", n)
		}
		return n.String(), nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return "", errors.New("want a port number or \"host:port\"")
	}
	return s, nil
}

func mountString(b json.RawMessage) (string, error) {
	var s string
	if json.Unmarshal(b, &s) == nil {
		return s, nil
	}
	var o map[string]any
	if err := json.Unmarshal(b, &o); err != nil || o == nil {
		return "", errors.New("want a string or an object")
	}
	parts := make([]string, 0, len(o))
	for _, k := range []string{"source", "target", "type"} {
		if v, ok := o[k]; ok {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
			delete(o, k)
		}
	}
	rest := make([]string, 0, len(o))
	for k := range o {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	for _, k := range rest {
		parts = append(parts, fmt.Sprintf("%s=%v", k, o[k]))
	}
	return strings.Join(parts, ","), nil
}

// stageCommands reads one lifecycle value: string, list of strings, or an
// object of named strings/lists (run in key order for a stable plan).
func stageCommands(stage string, b json.RawMessage) ([]Command, error) {
	if isNull(b) {
		return nil, nil
	}
	local := stage == "initializeCommand"
	one := func(name string, v json.RawMessage) (*Command, error) {
		var s string
		if json.Unmarshal(v, &s) == nil {
			if strings.TrimSpace(s) == "" {
				return nil, nil
			}
			return &Command{Stage: stage, Name: name, Shell: s, Local: local}, nil
		}
		var l []string
		if err := json.Unmarshal(v, &l); err != nil {
			return nil, errors.New("want a string, a list of strings or an object")
		}
		if len(l) == 0 {
			return nil, nil
		}
		return &Command{Stage: stage, Name: name, Args: l, Local: local}, nil
	}
	if t := bytes.TrimSpace(b); len(t) > 0 && t[0] == '{' {
		var o map[string]json.RawMessage
		if err := json.Unmarshal(b, &o); err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(o))
		for k := range o {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var out []Command
		for _, k := range keys {
			c, err := one(k, o[k])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			if c != nil {
				out = append(out, *c)
			}
		}
		return out, nil
	}
	c, err := one("", b)
	if err != nil || c == nil {
		return nil, err
	}
	return []Command{*c}, nil
}
