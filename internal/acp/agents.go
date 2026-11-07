package acp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Agent is one ACP agent the user set up: a program termocode starts.
type Agent struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// agentsFile is <config>/ai/acp_agents.json.
type agentsFile struct {
	Agents []Agent `json:"agents"`
}

// AgentsTemplate is the file written the first time the user opens it.
const AgentsTemplate = `{
  "agents": [
    {"name": "Example agent", "command": "my-agent", "args": ["--acp"]}
  ]
}
`

// LoadAgents reads the agents file; a missing file is no agents. An
// entry without a name or command is skipped.
func LoadAgents(path string) ([]Agent, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f agentsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	var out []Agent
	for _, a := range f.Agents {
		if strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.Command) == "" {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// EnsureAgentsFile writes the template when path does not exist.
func EnsureAgentsFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(AgentsTemplate), 0o600)
}
