package devcontainer

import (
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// PlanCLISetup is the setup of c through the devcontainer CLI (cli is
// its path): one "devcontainer up" that makes the container, installs
// the features and runs the lifecycle commands itself. It is the way
// for compose files and features; termocode runs no lifecycle command
// of its own here. The container gets the Label with root, so it is
// found again. engine is the engine prefix ("docker", maybe with
// --context c): the CLI gets the binary and the context as env.
func (c Config) PlanCLISetup(root, cli string, engine []string) (Setup, error) {
	if c.Kind() == KindNone {
		return Setup{}, ErrNoImage
	}
	if len(engine) == 0 {
		engine = []string{"docker"}
	}
	root = filepath.Clean(root)
	ws := c.WorkspaceFolder
	if ws == "" || !path.IsAbs(ws) {
		if c.Kind() == KindCompose {
			ws = "/"
		} else {
			ws = "/workspaces/" + filepath.Base(root)
		}
	}
	var argv []string
	if len(engine) >= 3 {
		switch engine[1] {
		case "--context":
			argv = []string{"env", "DOCKER_CONTEXT=" + engine[2]}
		case "--connection":
			argv = []string{"env", "CONTAINER_CONNECTION=" + engine[2]}
		}
	}
	argv = append(argv, cli, "up", "--workspace-folder", root,
		"--id-label", Label+"="+root, "--docker-path", engine[0])
	return Setup{
		WorkspaceFolder: path.Clean(ws),
		CLI:             true,
		Create:          []Step{{Title: "Make the container (devcontainer CLI)", Argv: argv, Dir: root, Up: true}},
	}, nil
}

// FeatureIDs are the feature ids of the file, sorted (the trust card
// lists them: a feature runs its install script in the container).
func (c Config) FeatureIDs() []string {
	out := make([]string, 0, len(c.Features))
	for k := range c.Features {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// UpResult is the JSON line "devcontainer up" prints last.
type UpResult struct {
	Outcome               string `json:"outcome"`
	ContainerID           string `json:"containerId"`
	RemoteUser            string `json:"remoteUser"`
	RemoteWorkspaceFolder string `json:"remoteWorkspaceFolder"`
	Message               string `json:"message"`
	Description           string `json:"description"`
}

// ParseUpResult reads the result of "devcontainer up" from its stdout
// (log lines before it are skipped). An error outcome is an error.
func ParseUpResult(stdout string) (UpResult, error) {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(l, "{") {
			continue
		}
		var r UpResult
		if json.Unmarshal([]byte(l), &r) != nil || r.Outcome == "" {
			continue
		}
		if r.Outcome != "success" {
			msg := r.Message
			if r.Description != "" {
				msg = strings.TrimSpace(msg + ": " + r.Description)
			}
			if msg == "" {
				msg = r.Outcome
			}
			return r, errors.New("devcontainer up: " + msg)
		}
		if r.ContainerID == "" {
			return r, errors.New("devcontainer up: no container id in its result")
		}
		return r, nil
	}
	return UpResult{}, errors.New("devcontainer up printed no result")
}

// ─── the open-project offer ───────────────────────────────────────────

func offerPath() (string, error) {
	p, err := trustPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "devcontainer-offer.json"), nil
}

type offerState struct {
	Never []string `json:"never"`
}

func loadOffer() offerState {
	var s offerState
	if p, err := offerPath(); err == nil {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &s)
		}
	}
	return s
}

// OfferMuted reports whether the user chose Never for root's offer.
func OfferMuted(root string) bool {
	root = filepath.Clean(root)
	for _, r := range loadOffer().Never {
		if r == root {
			return true
		}
	}
	return false
}

// MuteOffer records Never for root (0600 state file).
func MuteOffer(root string) error {
	root = filepath.Clean(root)
	p, err := offerPath()
	if err != nil {
		return err
	}
	s := loadOffer()
	for _, r := range s.Never {
		if r == root {
			return nil
		}
	}
	s.Never = append(s.Never, root)
	sort.Strings(s.Never)
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}
