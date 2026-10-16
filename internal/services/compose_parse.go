package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// psEntry is one container in `docker compose ps --format json`. Compose
// before 2.21 prints one JSON array, later ones JSON lines; field casing
// of Health varies.
type psEntry struct {
	Service    string
	Name       string
	Image      string
	State      string
	Health     string
	ExitCode   int
	Status     string
	Publishers []struct {
		URL           string
		TargetPort    int
		PublishedPort int
		Protocol      string
	}
}

// ParsePS reads `compose ps --all --format json` (array or lines) into
// statuses by service name. at is the read time, for StartedAt.
func ParsePS(data []byte, at time.Time) (map[string]Status, error) {
	out := map[string]Status{}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return out, nil
	}
	var entries []psEntry
	if data[0] == '[' {
		if err := json.Unmarshal(data, &entries); err != nil {
			return nil, fmt.Errorf("compose ps: %w", err)
		}
	} else {
		for _, line := range bytes.Split(data, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			var e psEntry
			if err := json.Unmarshal(line, &e); err != nil {
				return nil, fmt.Errorf("compose ps: %w", err)
			}
			entries = append(entries, e)
		}
	}
	for _, e := range entries {
		name := e.Service
		if name == "" {
			name = e.Name
		}
		st := Status{
			Name:     name,
			Health:   strings.ToLower(e.Health),
			ExitCode: e.ExitCode,
			Text:     e.Status,
			Image:    e.Image,
		}
		if st.ExitCode == 0 {
			st.ExitCode = exitCodeFromText(e.Status)
		}
		st.State = StateFromPS(e.State, st.Health, e.Status, st.ExitCode)
		if d, ok := upFor(e.Status); ok {
			st.StartedAt = at.Add(-d)
		}
		seen := map[string]bool{}
		for _, p := range e.Publishers {
			if p.PublishedPort == 0 {
				continue
			}
			key := fmt.Sprintf("%d/%d/%s", p.PublishedPort, p.TargetPort, p.Protocol)
			if seen[key] { // the same port on IPv4 and IPv6
				continue
			}
			seen[key] = true
			st.Ports = append(st.Ports, Port{Host: p.URL, Published: p.PublishedPort, Target: p.TargetPort, Proto: p.Protocol})
		}
		// Two replicas of one service: keep the worse one visible.
		if old, ok := out[name]; ok && old.State.Up() && !st.State.Up() {
			continue
		}
		out[name] = st
	}
	return out, nil
}

// StateFromPS maps compose's State / Health / Status text to a State.
func StateFromPS(state, health, text string, exitCode int) State {
	health = strings.ToLower(health)
	switch strings.ToLower(state) {
	case "running":
		return runningState(health, text)
	case "restarting":
		return Restarting
	case "exited", "dead":
		if exitCode != 0 {
			return Exited
		}
		return Stopped
	case "created":
		return Stopped
	case "paused":
		return Paused
	case "removing":
		return Stopping
	}
	// No State field (very old compose): read the text.
	switch {
	case strings.HasPrefix(text, "Up "):
		if strings.Contains(text, "(Paused)") {
			return Paused
		}
		return runningState(health, text)
	case strings.HasPrefix(text, "Exited"):
		if exitCode != 0 || exitCodeFromText(text) != 0 {
			return Exited
		}
		return Stopped
	case strings.HasPrefix(text, "Restarting"):
		return Restarting
	case strings.HasPrefix(text, "Created"):
		return Stopped
	}
	return Unknown
}

func runningState(health, text string) State {
	if health == "" {
		switch {
		case strings.Contains(text, "(unhealthy)"):
			health = "unhealthy"
		case strings.Contains(text, "(healthy)"):
			health = "healthy"
		case strings.Contains(text, "(health: starting)"):
			health = "starting"
		}
	}
	switch health {
	case "healthy":
		return Healthy
	case "unhealthy":
		return Unhealthy
	case "starting":
		return Starting
	}
	return Running
}

var exitRe = regexp.MustCompile(`^(?:Exited|Restarting) \((-?\d+)\)`)

func exitCodeFromText(text string) int {
	if m := exitRe.FindStringSubmatch(text); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

var upRe = regexp.MustCompile(`^Up (Less than a second|About an? (hour|minute)|(\d+) (second|minute|hour|day|week|month|year)s?)`)

// upFor reads the uptime from a Status text ("Up 2 hours (healthy)"). The
// docker text is coarse; so is the result.
func upFor(text string) (time.Duration, bool) {
	m := upRe.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	switch {
	case m[1] == "Less than a second":
		return 0, true
	case m[2] == "hour":
		return time.Hour, true
	case m[2] == "minute":
		return time.Minute, true
	}
	n, _ := strconv.Atoi(m[3])
	unit := map[string]time.Duration{
		"second": time.Second, "minute": time.Minute, "hour": time.Hour,
		"day": 24 * time.Hour, "week": 7 * 24 * time.Hour, "month": 30 * 24 * time.Hour,
		"year": 365 * 24 * time.Hour,
	}[m[4]]
	return time.Duration(n) * unit, true
}

// Config is what `compose config --format json` says about the project.
type Config struct {
	Project  string
	Services []Service // sorted by name
	// SecretValues are env values whose keys look secret (≥ 8 chars), for
	// ai.RegisterSecret. They never go into a Service.
	SecretValues []string
}

// secretKey matches env names whose values must never reach the model.
var secretKey = regexp.MustCompile(`(?i)(pass|secret|token|key|dsn|url|auth|credential)`)

// IsSecretKey reports whether an env name looks like it holds a secret.
func IsSecretKey(k string) bool { return secretKey.MatchString(k) }

type configPort struct {
	Target    int             `json:"target"`
	Published json.RawMessage `json:"published"` // "5432" (new) or 5432 (old)
	HostIP    string          `json:"host_ip"`
	Protocol  string          `json:"protocol"`
}

// ParseConfig reads `docker compose config --format json`.
func ParseConfig(data []byte) (Config, error) {
	var doc struct {
		Name     string `json:"name"`
		Services map[string]struct {
			Image       string                     `json:"image"`
			Ports       []configPort               `json:"ports"`
			DependsOn   map[string]json.RawMessage `json:"depends_on"`
			Profiles    []string                   `json:"profiles"`
			Healthcheck *struct {
				Disable bool `json:"disable"`
			} `json:"healthcheck"`
			Environment map[string]*string `json:"environment"`
		} `json:"services"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return Config{}, fmt.Errorf("compose config: %w", err)
	}
	cfg := Config{Project: doc.Name}
	seen := map[string]bool{}
	for name, s := range doc.Services {
		svc := Service{Name: name, Backend: "compose", Image: s.Image, Profiles: s.Profiles,
			HasHealthcheck: s.Healthcheck != nil && !s.Healthcheck.Disable}
		for dep := range s.DependsOn {
			svc.DependsOn = append(svc.DependsOn, dep)
		}
		sort.Strings(svc.DependsOn)
		for _, p := range s.Ports {
			pub := strings.Trim(string(p.Published), `"`)
			n, _ := strconv.Atoi(pub)
			svc.Ports = append(svc.Ports, Port{Host: p.HostIP, Published: n, Target: p.Target, Proto: p.Protocol})
		}
		for k, v := range s.Environment {
			svc.EnvKeys = append(svc.EnvKeys, k)
			if v != nil && IsSecretKey(k) && len(strings.TrimSpace(*v)) >= 8 && !seen[*v] {
				seen[*v] = true
				cfg.SecretValues = append(cfg.SecretValues, *v)
			}
		}
		sort.Strings(svc.EnvKeys)
		cfg.Services = append(cfg.Services, svc)
	}
	sort.Slice(cfg.Services, func(i, j int) bool { return cfg.Services[i].Name < cfg.Services[j].Name })
	sort.Strings(cfg.SecretValues)
	return cfg, nil
}

// ComposeServiceLines finds the 1-based line of each service key under
// the top-level `services:` block of a compose YAML file. It is a line
// scan, not a YAML parser: good enough for "open at the service".
func ComposeServiceLines(src string) map[string]int {
	out := map[string]int{}
	in := false
	indent := -1
	for i, line := range strings.Split(src, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		lead := len(line) - len(strings.TrimLeft(line, " \t"))
		if lead == 0 {
			in = strings.HasPrefix(trim, "services:")
			indent = -1
			continue
		}
		if !in {
			continue
		}
		if indent < 0 {
			indent = lead
		}
		if lead != indent {
			continue
		}
		if k, _, ok := strings.Cut(trim, ":"); ok {
			out[strings.Trim(k, `"'`)] = i + 1
		}
	}
	return out
}

// ProblemFromStderr classifies a failed docker call by its error text.
func ProblemFromStderr(stderr string) Problem {
	s := strings.TrimSpace(stderr)
	if s == "" {
		return Problem{}
	}
	first, _, _ := strings.Cut(s, "\n")
	first = strings.TrimSpace(first)
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "cannot connect to the docker daemon"), strings.Contains(low, "is the docker daemon running"):
		return Problem{Kind: ProblemDaemonDown, Detail: first}
	case strings.Contains(low, "permission denied") && strings.Contains(low, "docker"):
		return Problem{Kind: ProblemNoPermission, Detail: first}
	case strings.Contains(low, "'compose' is not a docker command"), strings.Contains(low, "unknown shorthand flag: 'f'"),
		strings.Contains(low, "unknown command \"compose\""):
		return Problem{Kind: ProblemNoCompose, Detail: first}
	case strings.HasPrefix(low, "validating "), strings.HasPrefix(low, "yaml: "), strings.Contains(low, "invalid compose project"),
		strings.Contains(low, "no configuration file provided"), strings.Contains(low, "is invalid"):
		return Problem{Kind: ProblemBadConfig, Detail: first}
	}
	return Problem{Kind: ProblemOther, Detail: first}
}
