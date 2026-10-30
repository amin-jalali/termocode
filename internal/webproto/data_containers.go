package webproto

// The containers data (WEB-44, web design 2.9): the host's containers
// and the tail of one container's logs. Env values, labels and mounts
// never leave the hub; the logs pass the same redaction as the AI's.

func init() {
	registerResult(OpContainersList, ContainersList{})
	registerResult(OpContainersLogs, ContainerLogs{})
	registerResult(OpContainersStart, ContainerDone{})
	registerResult(OpContainersRestart, ContainerDone{})
	registerResult(OpContainersStop, AskRef{})
	registerResult(OpContainersRemove, AskRef{})
}

// Containers limits.
const (
	MaxContainers       = 200
	MaxContainerLogs    = 64 << 10 // bytes of log text, the newest kept
	DefaultContainerLog = 200      // lines when containers.logs names none
)

// ContainersList answers containers.list.
type ContainersList struct {
	Engine     string          `json:"engine"`            // "docker", "podman"
	Context    string          `json:"context,omitempty"` // the engine context or connection
	Containers []ContainerItem `json:"containers"`
	More       int             `json:"more,omitempty"` // past MaxContainers
}

// ContainerItem is one container.
type ContainerItem struct {
	ID      string `json:"id"` // the full id the ops take
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"`  // "running", "exited", "paused" …
	Status  string `json:"status"` // "Up 2 hours (healthy)"
	Ports   string `json:"ports,omitempty"`
	Project string `json:"project,omitempty"` // its compose project
	Busy    bool   `json:"busy,omitempty"`    // starting or stopping on the laptop now
}

// ContainerLogs answers containers.logs.
type ContainerLogs struct {
	ID        string   `json:"id"`
	Lines     []string `json:"lines"`
	Redacted  int      `json:"redacted,omitempty"`  // values the redaction hid
	Truncated bool     `json:"truncated,omitempty"` // older text cut at MaxContainerLogs
}

// ContainerDone answers start and restart: the container's state after.
type ContainerDone struct {
	State  string `json:"state"`
	Status string `json:"status,omitempty"`
}
