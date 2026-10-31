package services

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// State is what a service is doing now.
type State int

const (
	Unknown State = iota
	NotCreated
	Starting
	Pulling
	Running
	Healthy
	Unhealthy
	Restarting
	Stopping
	Stopped
	Exited
	Paused
)

var stateNames = [...]string{"unknown", "not created", "starting", "pulling", "running", "healthy",
	"unhealthy", "restarting", "stopping", "stopped", "exited", "paused"}

func (s State) String() string {
	if s >= 0 && int(s) < len(stateNames) {
		return stateNames[s]
	}
	return "unknown"
}

// Up reports whether the service is running (in any health).
func (s State) Up() bool { return s == Running || s == Healthy || s == Unhealthy }

// Busy reports a start, stop, restart or pause in flight: a second
// action is ignored. Restarting counts, so a second restart or pause is
// not sent while one runs (FIX-32); a stop still works on a crash loop.
func (s State) Busy() bool {
	return s == Starting || s == Pulling || s == Stopping || s == Restarting
}

// Port is one published (or declared) port.
type Port struct {
	Host      string
	Published int
	Target    int
	Proto     string
}

// String is "0.0.0.0:5432→5432": the host part only when set, the target
// only when it differs or a host is shown, the proto only when not tcp.
func (p Port) String() string {
	var b strings.Builder
	if p.Host != "" {
		b.WriteString(p.Host)
		b.WriteByte(':')
	}
	if p.Published > 0 {
		b.WriteString(strconv.Itoa(p.Published))
	}
	if p.Target > 0 && (p.Target != p.Published || p.Host != "") {
		b.WriteString("→")
		b.WriteString(strconv.Itoa(p.Target))
	}
	if p.Proto != "" && p.Proto != "tcp" {
		b.WriteByte('/')
		b.WriteString(p.Proto)
	}
	return b.String()
}

// Probe is a local service's health check (zero = none).
type Probe struct {
	TCP  int
	HTTP string
}

// Service is one declared service.
type Service struct {
	Name, Backend, Image string
	Ports                []Port // declared (compose) or configured (services.json)
	DependsOn            []string
	Profiles             []string
	HasHealthcheck       bool
	EnvKeys              []string          // names only, never values
	PublicEnv            map[string]string // values of a few non-secret keys (see PublicEnvKey), compose only
	Source               string            // "compose.yaml:12", ".termocode/services.json", "Procfile"
	Command, Cwd         string            // local only
	Env                  map[string]string
	Health               Probe // local only
}

// Status is what `ps` (or the terminal job) says about a service now.
type Status struct {
	Name      string
	State     State
	Health    string // healthy / unhealthy / starting / ""
	ExitCode  int
	StartedAt time.Time
	Text      string // raw compose Status ("Up 2 hours (healthy)")
	Image     string
	Ports     []Port // actually published
	PortOpen  *bool  // from the probe; nil = not probed
}

// ProblemKind says why the compose backend cannot answer.
type ProblemKind int

const (
	ProblemNone ProblemKind = iota
	ProblemNoDocker
	ProblemNoCompose
	ProblemDaemonDown
	ProblemNoPermission
	ProblemBadConfig
	ProblemOther
)

// Problem is a backend-wide error state shown as a note row.
type Problem struct {
	Kind   ProblemKind
	Detail string
	// Engine is the engine kind ("docker" or "podman"); "" means docker.
	Engine string
}

// engineTitle is "Docker" or "Podman" for messages.
func (p Problem) engineTitle() string {
	if p.Engine == "podman" {
		return "Podman"
	}
	return "Docker"
}

// Message is the one-line text for the note row and the AI.
func (p Problem) Message() string {
	switch p.Kind {
	case ProblemNoDocker:
		return "No container engine"
	case ProblemNoCompose:
		return "Docker Compose v2 is needed"
	case ProblemDaemonDown:
		return p.engineTitle() + " is not running"
	case ProblemNoPermission:
		return "No access to the " + p.engineTitle() + " socket"
	case ProblemBadConfig:
		if p.Detail != "" {
			return p.Detail
		}
		return "compose file is not valid"
	case ProblemOther:
		return p.Detail
	}
	return ""
}

// Snapshot is the whole known state of the project's services.
type Snapshot struct {
	Project  string
	Files    []string // compose files, root-relative
	Services []Service
	Status   map[string]Status
	Problem  Problem
	At       time.Time
	Seq      uint64
}

// FormatUptime is the coarse uptime text: 12s, 3m, 2h 14m, 3d.
func FormatUptime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		h, m := int(d/time.Hour), int(d%time.Hour/time.Minute)
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
}

// PublicEnvKey reports whether the value of an environment key may be read
// by the app: the user and database names the database images use. They
// are not secrets, and the Databases view prefills its add flow with them.
// Passwords and every other key stay names only.
func PublicEnvKey(k string) bool {
	switch k {
	case "POSTGRES_USER", "POSTGRES_DB", "MYSQL_USER", "MYSQL_DATABASE", "MARIADB_USER", "MARIADB_DATABASE":
		return true
	}
	return false
}
