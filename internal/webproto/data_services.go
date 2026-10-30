package webproto

// Project services (WEBP-4): the dev services the laptop's SERVICES view
// lists — Docker Compose services of the project and its local dev
// commands (Procfile, .termocode/services.json) — with their state, and
// start, restart, stop and the newest log lines. The page names a
// service by its name; the laptop runs its own compose or command line
// (a local command the laptop never ran before asks there first).
// Starting and restarting ask in the page; stopping waits for a laptop
// card, as a container's stop does. The topic sends the whole list.

// Service op names.
const (
	OpServicesRefresh = "services.refresh"
	OpServicesLogs    = "services.logs"
	OpServicesStart   = "services.start"
	OpServicesRestart = "services.restart"
	OpServicesStop    = "services.stop"
)

var opServicesRows = []Policy{
	{Op: OpServicesRefresh, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpServicesLogs, New: newOf[ServiceLogsArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpServicesStart, New: newOf[ServiceArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpServicesRestart, New: newOf[ServiceArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpServicesStop, New: newOf[ServiceArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opServicesRows...); return true }()

func init() {
	registerTopic("services", ServicesSnap{}, ServicesSnap{})
	registerResult(OpServicesLogs, ServiceLogs{})
	registerResult(OpServicesStart, ServiceDone{})
	registerResult(OpServicesRestart, ServiceDone{})
	registerResult(OpServicesStop, AskRef{})
}

// Services limits.
const (
	MaxServices        = 200
	MaxServiceName     = 128
	MaxServiceLogLines = 1000
	MaxServiceLogs     = 64 << 10 // bytes of log text, the newest kept
	DefaultServiceLog  = 200
)

// ServicesSnap is the services topic.
type ServicesSnap struct {
	// State: "none" the project has no services, "loading" before the
	// laptop read them, "ready".
	State        string        `json:"state" enum:"none|loading|ready"`
	Project      string        `json:"project,omitempty"` // the compose project
	ComposeFiles []string      `json:"compose_files"`
	Problem      string        `json:"problem,omitempty"`      // "Docker is not running" …
	ConfigError  string        `json:"config_error,omitempty"` // services.json does not parse
	Services     []ServiceItem `json:"services"`
	More         int           `json:"more,omitempty"`
}

// ServiceItem is one service. Kind "compose" is a Docker Compose
// service, "local" a dev command in a laptop terminal. State is the
// laptop's ("running", "healthy", "exited" …); Busy: starting or stopping
// from the laptop now.
type ServiceItem struct {
	Name    string        `json:"name"`
	Kind    string        `json:"kind" enum:"compose|local"`
	State   string        `json:"state"`
	Health  string        `json:"health,omitempty"`
	Exit    int           `json:"exit,omitempty"`
	Busy    bool          `json:"busy,omitempty"`
	Image   string        `json:"image,omitempty"`
	Command string        `json:"command,omitempty"` // a local service's command line (to read)
	Source  string        `json:"source,omitempty"`  // "compose.yaml:12", "Procfile"
	Status  string        `json:"status,omitempty"`  // compose's own words ("Up 2 hours")
	Since   int64         `json:"since,omitempty"`   // Unix ms it came up
	Ports   []ServicePort `json:"ports"`
}

// ServicePort is a published port; Open is what the laptop's probe saw
// ("" not probed).
type ServicePort struct {
	Published int    `json:"published"`
	Target    int    `json:"target,omitempty"`
	Proto     string `json:"proto,omitempty"`
	Open      string `json:"open,omitempty" enum:"yes|no"`
}

// ServiceLogs answers services.logs.
type ServiceLogs struct {
	Name      string   `json:"name"`
	Lines     []string `json:"lines"`
	Redacted  int      `json:"redacted,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
}

// ServiceDone answers start and restart. Asked: the laptop asks first
// (a local command it never ran), nothing runs until it says yes.
// Note: why nothing changed ("already running").
type ServiceDone struct {
	Asked bool   `json:"asked,omitempty"`
	Note  string `json:"note,omitempty"`
}

// ServiceArgs names a service.
type ServiceArgs struct {
	Name string `json:"name"`
}

// ServiceLogsArgs asks for a service's newest log lines.
type ServiceLogsArgs struct {
	Name  string `json:"name"`
	Lines int    `json:"lines,omitempty"` // 0: 200
}

func (a *ServiceArgs) validate(int) error {
	if !validName(a.Name, MaxServiceName) {
		return invalid("bad service name")
	}
	return nil
}

func (a *ServiceLogsArgs) validate(int) error {
	if !validName(a.Name, MaxServiceName) || a.Lines < 0 || a.Lines > MaxServiceLogLines {
		return invalid("bad service name or lines")
	}
	return nil
}
