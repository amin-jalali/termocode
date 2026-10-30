package services

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// ConfigPath is the project file for local services, root-relative.
const ConfigPath = ".termocode/services.json"

// FileConfig is .termocode/services.json.
type FileConfig struct {
	Compose struct {
		File     string   `json:"file"`
		Profiles []string `json:"profiles"`
		Project  string   `json:"project"`
	} `json:"compose"`
	Services []LocalConfig `json:"services"`
}

// LocalConfig is one local command service.
type LocalConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Cwd     string            `json:"cwd"`
	Env     map[string]string `json:"env"`
	Port    int               `json:"port"`
	Health  struct {
		TCP  int    `json:"tcp"`
		HTTP string `json:"http"`
	} `json:"health"`
}

var nameRe = lazyre.New(`^[a-z0-9_-]+$`)

// ValidName reports whether s is a valid service name.
func ValidName(s string) bool { return nameRe.MatchString(s) }

// ParseFileConfig reads services.json (JSONC: comments and trailing
// commas allowed) and validates it.
func ParseFileConfig(data []byte) (FileConfig, error) {
	var fc FileConfig
	if err := json.Unmarshal(StripJSONC(data), &fc); err != nil {
		return fc, fmt.Errorf("%s: %w", ConfigPath, err)
	}
	seen := map[string]bool{}
	for i, s := range fc.Services {
		where := fmt.Sprintf("%s: service %d", ConfigPath, i+1)
		switch {
		case !ValidName(s.Name):
			return fc, fmt.Errorf("%s: name %q must match [a-z0-9_-]+", where, s.Name)
		case seen[s.Name]:
			return fc, fmt.Errorf("%s: name %q is used twice", where, s.Name)
		case strings.TrimSpace(s.Command) == "":
			return fc, fmt.Errorf("%s (%s): command is empty", where, s.Name)
		case s.Port < 0 || s.Port > 65535, s.Health.TCP < 0 || s.Health.TCP > 65535:
			return fc, fmt.Errorf("%s (%s): port must be 1-65535", where, s.Name)
		case s.Cwd != "" && (filepath.IsAbs(s.Cwd) || strings.HasPrefix(filepath.Clean(s.Cwd), "..")):
			return fc, fmt.Errorf("%s (%s): cwd must stay inside the project", where, s.Name)
		}
		if h := s.Health.HTTP; h != "" {
			u, err := url.Parse(h)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fc, fmt.Errorf("%s (%s): health.http must be an http(s) URL", where, s.Name)
			}
		}
		seen[s.Name] = true
	}
	return fc, nil
}

// LocalServices turns the config into services.
func (fc FileConfig) LocalServices() []Service {
	out := make([]Service, 0, len(fc.Services))
	for _, c := range fc.Services {
		s := Service{Name: c.Name, Backend: "local", Command: c.Command, Cwd: c.Cwd, Env: c.Env,
			Source: ConfigPath, Health: Probe{TCP: c.Health.TCP, HTTP: c.Health.HTTP}}
		if c.Port > 0 {
			s.Ports = []Port{{Published: c.Port, Target: c.Port, Proto: "tcp"}}
			if s.Health == (Probe{}) {
				s.Health.TCP = c.Port
			}
		}
		for k := range c.Env {
			s.EnvKeys = append(s.EnvKeys, k)
		}
		sort.Strings(s.EnvKeys)
		out = append(out, s)
	}
	return out
}

// Template is what "Containers: Configure Services" writes.
const Template = `{
  // termocode services: local commands shown in the Services view.
  // Each one runs in its own terminal tab; x stops it (SIGTERM).
  // Docker Compose services come from compose.yaml without any setup.

  // Optional: choose the compose file and profiles.
  // "compose": { "file": "compose.yaml", "profiles": ["dev"] },

  "services": [
    {
      "name": "web",                  // [a-z0-9_-]+
      "command": "npm run dev",       // a shell line, like tasks.json
      "cwd": ".",                     // relative to the project
      // "env": { "PORT": "5173" },
      "port": 5173                    // shown in the list; probed with TCP
      // "health": { "http": "http://localhost:5173/" }
    }
  ]
}
`

// ParseProcfile reads `name: command` lines. $PORT and other foreman
// rules are not emulated.
func ParseProcfile(src, source string) []Service {
	var out []Service
	seen := map[string]bool{}
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, cmd, ok := strings.Cut(line, ":")
		name, cmd = strings.TrimSpace(name), strings.TrimSpace(cmd)
		if !ok || cmd == "" || !ValidName(name) || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, Service{Name: name, Backend: "local", Command: cmd, Source: source})
	}
	return out
}

// StripJSONC removes // and /* */ comments (outside strings) and
// trailing commas before } or ].
func StripJSONC(src []byte) []byte {
	out := make([]byte, 0, len(src))
	inStr, esc := false, false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inStr {
			out = append(out, c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out = append(out, c)
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i++
		default:
			out = append(out, c)
		}
	}
	res := make([]byte, 0, len(out))
	inStr, esc = false, false
	for i := 0; i < len(out); i++ {
		c := out[i]
		if inStr {
			res = append(res, c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
		}
		if c == ',' {
			j := i + 1
			for j < len(out) && (out[j] == ' ' || out[j] == '\t' || out[j] == '\n' || out[j] == '\r') {
				j++
			}
			if j < len(out) && (out[j] == '}' || out[j] == ']') {
				continue
			}
		}
		res = append(res, c)
	}
	return res
}
