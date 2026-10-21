// Package containers contains pure container-engine data types and parsers.
package containers

import "time"

// Container is a normalized row from an engine's container listing.
type Container struct {
	ID          string
	Names       []string
	Image       string
	ImageID     string
	Command     string
	Created     time.Time
	CreatedAt   string
	State       string
	Status      string
	Ports       []Port
	PortsText   string
	Labels      map[string]string
	Size        string
	Networks    []string
	Mounts      []string
	Project     string
	WorkingDir  string
	ConfigFiles []string
}

// Port describes one published or exposed container port.
type Port struct {
	IP          string
	PrivatePort int
	PublicPort  int
	Type        string
}

// Image is a normalized row from an engine's image listing.
type Image struct {
	ID          string
	RepoTags    []string
	RepoDigests []string
	Created     time.Time
	CreatedAt   string
	Size        string
	VirtualSize string
	Labels      map[string]string
}

// Network describes a network returned by a CLI listing.
type Network struct {
	ID      string
	Name    string
	Driver  string
	Scope   string
	Created time.Time
	Labels  map[string]string
}

// Volume describes a volume returned by a CLI listing.
type Volume struct {
	Name       string
	Driver     string
	Mountpoint string
	Scope      string
	Created    time.Time
	Labels     map[string]string
	Size       string
}

// Context describes a CLI context. Endpoint fields vary by engine/version.
type Context struct {
	Name           string
	Current        bool
	DockerEndpoint string
	Host           string
	TLS            bool
	Description    string
	Error          string
}

// Registry contains only registry host and credential-helper metadata.
// Credential material (including auth fields) is intentionally not modeled.
type Registry struct {
	Host       string
	CredsStore string
	CredHelper string
}

// ProjectGroup contains containers sharing a compose project label.
type ProjectGroup struct {
	Name       string
	WorkingDir string
	Containers []Container
	IsOther    bool
}

// Event is a normalized engine event.
type Event struct {
	Type   string
	Action string
	Actor  string
	Time   time.Time
	Labels map[string]string
}

// Stat is one row from a non-streaming stats result.
type Stat struct {
	Container string
	CPUPerc   string
	MemUsage  string
	MemPerc   string
	NetIO     string
	BlockIO   string
	PIDs      string
}

// HistoryEntry is one image history row.
type HistoryEntry struct {
	ID        string
	Created   time.Time
	CreatedAt string
	CreatedBy string
	Size      string
	Comment   string
}

// Version is the normalized subset of engine version output.
type Version struct {
	Client     string
	Server     string
	Version    string
	APIVersion string
}
