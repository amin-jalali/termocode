package tasks

import (
	"path/filepath"
	"sort"
	"strings"
)

// Docker task types for tasks.json ("type": "docker-build" | "docker-run" |
// "docker-compose"). The app runs the derived command like any other task;
// this file only builds the command line.

type rawDockerBuild struct {
	Context    string            `json:"context"`
	Dockerfile string            `json:"dockerfile"`
	Tag        string            `json:"tag"`
	Target     string            `json:"target"`
	Platform   string            `json:"platform"`
	Pull       bool              `json:"pull"`
	BuildArgs  map[string]string `json:"buildArgs"`
}

type rawDockerRun struct {
	Image         string            `json:"image"`
	ContainerName string            `json:"containerName"`
	Ports         []string          `json:"ports"`
	Env           map[string]string `json:"env"`
	EnvFiles      []string          `json:"envFiles"`
	Volumes       []string          `json:"volumes"`
	Network       string            `json:"network"`
	Remove        *bool             `json:"remove"` // default true
	Command       string            `json:"command"`
}

type rawDockerCompose struct {
	Files    []string `json:"files"`
	Profiles []string `json:"profiles"`
	Services []string `json:"services"`
	Down     bool     `json:"down"`
	Detached bool     `json:"detached"`
	Build    bool     `json:"build"`
}

// dockerTask fills t from a docker-* task type. ok is false for an
// unknown type or a task that cannot run (docker-run without an image).
func dockerTask(r rawTask, t *Task) bool {
	switch r.Type {
	case "docker-build":
		b := rawDockerBuild{}
		if r.DockerBuild != nil {
			b = *r.DockerBuild
		}
		t.Command = dockerBuildCommand(b)
		if t.Group == "" {
			t.Group = GroupBuild
		}
		return true
	case "docker-run":
		if r.DockerRun == nil || strings.TrimSpace(r.DockerRun.Image) == "" {
			return false
		}
		t.Command = dockerRunCommand(*r.DockerRun)
		// Values stay off the command line ("-e KEY" reads the task env).
		if len(r.DockerRun.Env) > 0 {
			env := make(map[string]string, len(t.Env)+len(r.DockerRun.Env))
			for k, v := range t.Env {
				env[k] = v
			}
			for k, v := range r.DockerRun.Env {
				env[k] = v
			}
			t.Env = env
		}
		return true
	case "docker-compose":
		c := rawDockerCompose{}
		if r.DockerCompose != nil {
			c = *r.DockerCompose
		}
		t.Command = dockerComposeCommand(c)
		return true
	}
	return false
}

func dockerBuildCommand(b rawDockerBuild) string {
	argv := []string{"docker", "build"}
	if b.Dockerfile != "" {
		argv = append(argv, "-f", ShellQuote(b.Dockerfile))
	}
	if b.Tag != "" {
		argv = append(argv, "-t", ShellQuote(b.Tag))
	}
	if b.Target != "" {
		argv = append(argv, "--target", ShellQuote(b.Target))
	}
	if b.Platform != "" {
		argv = append(argv, "--platform", ShellQuote(b.Platform))
	}
	if b.Pull {
		argv = append(argv, "--pull")
	}
	for _, k := range sortedKeys(b.BuildArgs) {
		argv = append(argv, "--build-arg", ShellQuote(k+"="+b.BuildArgs[k]))
	}
	ctx := b.Context
	if ctx == "" {
		ctx = "."
	}
	return strings.Join(append(argv, ShellQuote(ctx)), " ")
}

func dockerRunCommand(r rawDockerRun) string {
	argv := []string{"docker", "run"}
	if r.Remove == nil || *r.Remove {
		argv = append(argv, "--rm")
	}
	if r.ContainerName != "" {
		argv = append(argv, "--name", ShellQuote(r.ContainerName))
	}
	for _, p := range r.Ports {
		argv = append(argv, "-p", ShellQuote(p))
	}
	for _, k := range sortedKeys(r.Env) {
		argv = append(argv, "-e", ShellQuote(k))
	}
	for _, f := range r.EnvFiles {
		argv = append(argv, "--env-file", ShellQuote(f))
	}
	for _, v := range r.Volumes {
		argv = append(argv, "-v", ShellQuote(v))
	}
	if r.Network != "" {
		argv = append(argv, "--network", ShellQuote(r.Network))
	}
	argv = append(argv, ShellQuote(r.Image))
	if c := strings.TrimSpace(r.Command); c != "" {
		argv = append(argv, c) // the user's own shell words, as written
	}
	return strings.Join(argv, " ")
}

func dockerComposeCommand(c rawDockerCompose) string {
	argv := []string{"docker", "compose"}
	for _, f := range c.Files {
		argv = append(argv, "-f", ShellQuote(f))
	}
	for _, p := range c.Profiles {
		argv = append(argv, "--profile", ShellQuote(p))
	}
	if c.Down {
		return strings.Join(append(argv, "down"), " ")
	}
	argv = append(argv, "up")
	if c.Detached {
		argv = append(argv, "-d")
	}
	if c.Build {
		argv = append(argv, "--build")
	}
	for _, s := range c.Services {
		argv = append(argv, ShellQuote(s))
	}
	return strings.Join(argv, " ")
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// composeFiles are the compose file names docker compose looks for.
var composeFiles = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// detectDocker offers build for a Dockerfile and up/down for a compose
// file in the root.
func detectDocker(root string) []Task {
	var out []Task
	if exists(root, "Dockerfile") {
		out = append(out, Task{Label: "docker: build", Command: "docker build -t " + ShellQuote(dockerTag(root)) + " .", Group: GroupBuild, Source: "docker"})
	}
	for _, f := range composeFiles {
		if exists(root, f) {
			q := ShellQuote(f)
			out = append(out,
				Task{Label: "docker: compose up", Command: "docker compose -f " + q + " up", Source: "docker"},
				Task{Label: "docker: compose down", Command: "docker compose -f " + q + " down", Source: "docker"})
			break
		}
	}
	return out
}

// dockerTag is the image name for root: its folder name in lower case,
// other characters turned into "-" ("My App" → "my-app").
func dockerTag(root string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(filepath.Base(root)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	tag := strings.Trim(b.String(), "-._")
	if tag == "" {
		return "app"
	}
	return tag
}
