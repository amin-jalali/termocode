package webproto

import "strings"

// The rest of the Containers view from the page (web parity, Containers
// area): the host's images, networks, volumes, contexts and registries;
// an inspect, an image's history, one stats sample and the disk usage;
// a Docker Hub search and a repository's remote tags; and the actions.
//
// Pause, tag, create, compose up and restart, the session's context,
// starting every service and the services.json template ask in the page
// (Cancel first) and run at once. What ends work, removes, sends or runs
// something waits for a laptop card: a shell or attach (it opens a
// laptop terminal the page sees in its terminals), an image's pull,
// push, run and remove, every prune, a network's or volume's remove,
// compose stop and down, the machine's default context, a registry
// sign-in (the password is typed on the laptop, never here) or sign-out,
// adding a database (its password too) and stopping every service.
//
// Env values never cross: an inspect is masked on the hub, a history's
// ENV steps lose their values, and a run takes no env at all. The hub
// names an item only by what its last listing gave.

// Container op names (web parity).
const (
	OpContainersHost           = "containers.host"
	OpContainersInspect        = "containers.inspect"
	OpContainersHistory        = "containers.history"
	OpContainersStats          = "containers.stats"
	OpContainersDiskUsage      = "containers.disk_usage"
	OpContainersSearch         = "containers.search"
	OpContainersTags           = "containers.tags"
	OpContainersPause          = "containers.pause"
	OpContainersTag            = "containers.tag"
	OpContainersCreate         = "containers.create"
	OpContainersCompose        = "containers.compose"
	OpContainersContext        = "containers.context"
	OpContainersExec           = "containers.exec"
	OpContainersImage          = "containers.image"
	OpContainersPrune          = "containers.prune"
	OpContainersStorageRemove  = "containers.storage_remove"
	OpContainersComposeAsk     = "containers.compose_ask"
	OpContainersContextDefault = "containers.context_default"
	OpContainersRegistry       = "containers.registry"
	OpContainersOpenDB         = "containers.open_db"
	OpServicesStartAll         = "services.start_all"
	OpServicesStopAll          = "services.stop_all"
	OpServicesConfigure        = "services.configure"
)

var opContainersRows = []Policy{
	{Op: OpContainersHost, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpContainersInspect, New: newOf[InspectArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpContainersHistory, New: newOf[RefArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpContainersStats, New: newOf[ContainerArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpContainersDiskUsage, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpContainersSearch, New: newOf[SearchTermArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpContainersTags, New: newOf[RefArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpContainersPause, New: newOf[ContainerArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpContainersTag, New: newOf[ImageTagArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpContainersCreate, New: newOf[StorageArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpContainersCompose, New: newOf[ComposeArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpContainersContext, New: newOf[ContextArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpContainersExec, New: newOf[ExecArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpContainersImage, New: newOf[ImageActArgs](), Role: RoleController, Rate: slow, MaxBytes: 2 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpContainersPrune, New: newOf[PruneArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpContainersStorageRemove, New: newOf[StorageArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpContainersComposeAsk, New: newOf[ComposeAskArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpContainersContextDefault, New: newOf[ContextArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpContainersRegistry, New: newOf[RegistryArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpContainersOpenDB, New: newOf[ContainerArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpServicesStartAll, New: newOf[EmptyArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpServicesStopAll, New: newOf[EmptyArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpServicesConfigure, New: newOf[EmptyArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opContainersRows...); return true }()

func init() {
	registerResult(OpContainersHost, ContainersHost{})
	registerResult(OpContainersInspect, ContainerInspect{})
	registerResult(OpContainersHistory, ImageHistory{})
	registerResult(OpContainersStats, ContainerStats{})
	registerResult(OpContainersDiskUsage, DiskUsage{})
	registerResult(OpContainersSearch, ImageSearch{})
	registerResult(OpContainersTags, RemoteTags{})
	registerResult(OpContainersPause, ContainerDone{})
	registerResult(OpContainersTag, HostDone{})
	registerResult(OpContainersCreate, HostDone{})
	registerResult(OpContainersCompose, HostDone{})
	registerResult(OpContainersContext, HostDone{})
	registerResult(OpServicesStartAll, HostDone{})
	registerResult(OpServicesConfigure, ServicesConfig{})
	for _, op := range []string{OpContainersExec, OpContainersImage, OpContainersPrune, OpContainersStorageRemove, OpContainersComposeAsk,
		OpContainersContextDefault, OpContainersRegistry, OpContainersOpenDB, OpServicesStopAll} {
		registerResult(op, AskRef{})
	}
}

// Containers host limits.
const (
	MaxHostItems     = 500  // images, networks, volumes each
	MaxInspectText   = 256 << 10
	MaxHistoryRows   = 200
	MaxSearchResults = 50
	MaxRemoteTags    = 500
	MaxRunPorts      = 8
	MaxContainerRef  = 255
	MaxContainerName = 128
)

// ── Args ────────────────────────────────────────────────────────────────

// InspectArgs names one item to inspect: a container by its hex id, an
// image by its reference or id, a network or volume by its name.
type InspectArgs struct {
	Kind string `json:"kind" enum:"container|image|network|volume"`
	ID   string `json:"id"`
}

// RefArgs names an image or a repository by its reference.
type RefArgs struct {
	Ref string `json:"ref"`
}

// SearchTermArgs is one Docker Hub search.
type SearchTermArgs struct {
	Term string `json:"term"`
}

// ImageTagArgs gives a listed image (Ref) a new reference (Tag).
type ImageTagArgs struct {
	Ref string `json:"ref"`
	Tag string `json:"tag"`
}

// StorageArgs names a network or volume.
type StorageArgs struct {
	Kind string `json:"kind" enum:"network|volume"`
	Name string `json:"name"`
}

// ComposeArgs starts (up) or restarts a compose project.
type ComposeArgs struct {
	Project string `json:"project"`
	Verb    string `json:"verb" enum:"up|restart"`
}

// ComposeAskArgs stops a compose project or removes its containers
// (down): a laptop card.
type ComposeAskArgs struct {
	Project string `json:"project"`
	Verb    string `json:"verb" enum:"stop|down"`
}

// ContextArgs names an engine context (podman: a connection).
type ContextArgs struct {
	Name string `json:"name"`
}

// ExecArgs opens a shell in, or attaches to, a container.
type ExecArgs struct {
	ID   string `json:"id"`
	Kind string `json:"kind" enum:"shell|attach"`
}

// ImageActArgs is an image action. Pull takes any reference; push, run
// and remove one the last containers.host listed. Run may name the
// container and publish ports; it never takes env values.
type ImageActArgs struct {
	Verb  string   `json:"verb" enum:"pull|push|run|remove"`
	Ref   string   `json:"ref"`
	Name  string   `json:"name,omitempty"`
	Ports []string `json:"ports,omitempty"`
}

// PruneArgs is one prune; "images-all" removes every unused image.
type PruneArgs struct {
	Kind string `json:"kind" enum:"containers|images|images-all|networks|volumes|system"`
}

// RegistryArgs signs in to (the laptop asks for the password) or out
// of a registry host; connect with no host lets the laptop ask it.
type RegistryArgs struct {
	Verb string `json:"verb" enum:"connect|signout"`
	Host string `json:"host"`
}

func validRefChars(s string, extra string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(extra, r)) {
			return false
		}
	}
	return true
}

// validImageRef is an image reference or id: no flag, no space.
func validImageRef(s string) bool {
	return s != "" && len(s) <= MaxContainerRef && s[0] != '-' && validRefChars(s, "._-/:@")
}

// validItemName is a network, volume, context or project name.
func validItemName(s string) bool {
	return s != "" && len(s) <= MaxContainerName && s[0] != '-' && validRefChars(s, "._-")
}

func (a *InspectArgs) validate(int) error {
	ok := false
	switch a.Kind {
	case "container":
		ok = validContainerID(a.ID)
	case "image":
		ok = validImageRef(a.ID)
	case "network", "volume":
		ok = validItemName(a.ID)
	}
	if !ok {
		return invalid("bad kind or id")
	}
	return nil
}

func (a *RefArgs) validate(int) error {
	if !validImageRef(a.Ref) {
		return invalid("bad image reference")
	}
	return nil
}

func (a *SearchTermArgs) validate(int) error {
	if a.Term == "" || len(a.Term) > 100 || a.Term[0] == '-' || !validRefChars(a.Term, "._-/:") {
		return invalid("bad search term")
	}
	return nil
}

func (a *ImageTagArgs) validate(int) error {
	if !validImageRef(a.Ref) || !validImageRef(a.Tag) {
		return invalid("bad image reference")
	}
	return nil
}

func (a *StorageArgs) validate(int) error {
	if a.Kind != "network" && a.Kind != "volume" || !validItemName(a.Name) {
		return invalid("bad kind or name")
	}
	return nil
}

func (a *ComposeArgs) validate(int) error {
	if a.Verb != "up" && a.Verb != "restart" || !validItemName(a.Project) {
		return invalid("bad project or verb")
	}
	return nil
}

func (a *ComposeAskArgs) validate(int) error {
	if a.Verb != "stop" && a.Verb != "down" || !validItemName(a.Project) {
		return invalid("bad project or verb")
	}
	return nil
}

func (a *ContextArgs) validate(int) error {
	if !validItemName(a.Name) {
		return invalid("bad context name")
	}
	return nil
}

func (a *ExecArgs) validate(int) error {
	if !validContainerID(a.ID) || a.Kind != "shell" && a.Kind != "attach" {
		return invalid("bad container id or kind")
	}
	return nil
}

func (a *ImageActArgs) validate(int) error {
	switch a.Verb {
	case "pull", "push", "remove":
		if a.Name != "" || len(a.Ports) > 0 {
			return invalid("only run takes a name or ports")
		}
	case "run":
	default:
		return invalid("bad verb")
	}
	if !validImageRef(a.Ref) {
		return invalid("bad image reference")
	}
	if a.Name != "" && !validItemName(a.Name) {
		return invalid("bad container name")
	}
	if len(a.Ports) > MaxRunPorts {
		return invalid("too many ports")
	}
	for _, p := range a.Ports {
		if p == "" || len(p) > 64 || p[0] == '-' || !validRefChars(p, ".:/[]") {
			return invalid("bad port")
		}
	}
	return nil
}

func (a *PruneArgs) validate(int) error {
	switch a.Kind {
	case "containers", "images", "images-all", "networks", "volumes", "system":
		return nil
	}
	return invalid("bad prune kind")
}

func (a *RegistryArgs) validate(int) error {
	if a.Verb != "connect" && a.Verb != "signout" {
		return invalid("bad verb")
	}
	if a.Host == "" && a.Verb == "connect" {
		return nil
	}
	if a.Host == "" || len(a.Host) > 255 || a.Host[0] == '-' || !validRefChars(a.Host, ".-_:/[]") {
		return invalid("bad registry host")
	}
	return nil
}

// ── Results ─────────────────────────────────────────────────────────────

// ContainersHost answers containers.host: everything of the host but its
// containers (containers.list). A section the engine could not list
// names its problem in Problems ("images": "…").
type ContainersHost struct {
	Engine     string            `json:"engine"`
	Context    string            `json:"context,omitempty"`
	Images     []ImageItem       `json:"images"`
	Networks   []NetworkItem     `json:"networks"`
	Volumes    []VolumeItem      `json:"volumes"`
	Contexts   []ContextItem     `json:"contexts"`
	Registries []RegistryItem    `json:"registries"`
	Problems   map[string]string `json:"problems,omitempty"`
}

// ImageItem is one image. Tags are its references ("" none: dangling).
type ImageItem struct {
	ID      string   `json:"id"` // the short id (12 hex)
	Tags    []string `json:"tags"`
	Size    string   `json:"size,omitempty"`
	Created string   `json:"created,omitempty"` // "3 days ago"
	InUse   bool     `json:"in_use,omitempty"`  // a container uses it
}

// NetworkItem is one network; BuiltIn ones (bridge, host, none) stay.
type NetworkItem struct {
	Name    string `json:"name"`
	Driver  string `json:"driver,omitempty"`
	Scope   string `json:"scope,omitempty"`
	BuiltIn bool   `json:"built_in,omitempty"`
}

// VolumeItem is one volume.
type VolumeItem struct {
	Name   string `json:"name"`
	Driver string `json:"driver,omitempty"`
	Size   string `json:"size,omitempty"`
}

// ContextItem is one engine context. Current: the engine's default;
// Session: the one this session uses now.
type ContextItem struct {
	Name        string `json:"name"`
	Current     bool   `json:"current,omitempty"`
	Session     bool   `json:"session,omitempty"`
	Description string `json:"description,omitempty"`
}

// RegistryItem is one registry the engine knows (never a credential).
type RegistryItem struct {
	Host     string `json:"host"`
	SignedIn bool   `json:"signed_in,omitempty"`
	User     string `json:"user,omitempty"`
}

// ContainerInspect answers containers.inspect: pretty JSON with env
// values, auth fields and secret-looking labels masked (Masked counts
// them), then redacted again.
type ContainerInspect struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Text      string `json:"text"`
	Masked    int    `json:"masked,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// ImageHistory answers containers.history, newest layer first.
type ImageHistory struct {
	Ref  string       `json:"ref"`
	Rows []HistoryRow `json:"rows"`
}

// HistoryRow is one layer; an ENV step's values are masked.
type HistoryRow struct {
	ID        string `json:"id,omitempty"`
	Created   string `json:"created,omitempty"`
	CreatedBy string `json:"created_by"`
	Size      string `json:"size,omitempty"`
	Comment   string `json:"comment,omitempty"`
}

// ContainerStats answers containers.stats: one sample.
type ContainerStats struct {
	ID      string `json:"id"`
	CPU     string `json:"cpu"`
	Mem     string `json:"mem"`
	MemPerc string `json:"mem_perc"`
	Net     string `json:"net"`
	Block   string `json:"block"`
	PIDs    string `json:"pids"`
}

// DiskUsage answers containers.disk_usage: the engine's table.
type DiskUsage struct {
	Lines []string `json:"lines"`
}

// ImageSearch answers containers.search.
type ImageSearch struct {
	Term    string       `json:"term"`
	Results []SearchItem `json:"results"`
}

// SearchItem is one Docker Hub result.
type SearchItem struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Stars       int    `json:"stars,omitempty"`
	Official    bool   `json:"official,omitempty"`
}

// RemoteTags answers containers.tags. Note: why there are none ("needs
// sign-in; pull by name still works").
type RemoteTags struct {
	Repo      string   `json:"repo"`
	Tags      []string `json:"tags"`
	Truncated bool     `json:"truncated,omitempty"`
	Note      string   `json:"note,omitempty"`
}

// HostDone answers an in-page action; Note says what happened ("Opened
// in a laptop terminal", "Already using remote").
type HostDone struct {
	Note string `json:"note,omitempty"`
}

// ServicesConfig answers services.configure: the workspace-relative path
// of services.json; Created: the laptop wrote the template just now.
type ServicesConfig struct {
	Path    string `json:"path"`
	Created bool   `json:"created,omitempty"`
}
