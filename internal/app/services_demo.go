package app

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
)

// servicesDemo loads the plan's mock into the Services view without
// docker (DevActionMsg "svcdemo", for the screenshots and goldens):
// postgres healthy, redis running, api starting, mailhog not created,
// a local web. variant "down": Docker is not running; "empty": no
// services at all.
func (m *Model) servicesDemo(variant string) {
	st := newServicesState()
	m.services = st
	st.opened, st.loaded, st.everShown, st.visible = true, true, true, true
	st.root = m.taskVars().WorkspaceFolder
	m.activity.SetActive(activity.ViewServices)
	if !m.showExp {
		m.showExp = true
		m.applyLayout()
	}
	m.focus = FocusExplorer
	// The Services shots show the PROJECT part only: no host section
	// would ever leave "Loading…" without docker.
	for _, sec := range hostSectionOrder {
		st.sections[sec] = false
	}
	if variant == "empty" {
		st.touch()
		return
	}
	now := clock.Now()
	port := func(p int) []services.Port { return []services.Port{{Published: p, Target: p, Proto: "tcp"}} }
	st.det = services.Detection{ComposeFiles: []string{"compose.yaml"}, Compose: true,
		Local: []services.Service{{Name: "web", Backend: "local", Command: "npm run dev", Ports: port(5173),
			Health: services.Probe{TCP: 5173}, Source: "Procfile"}}}
	st.local = st.det.Local
	st.project = "termocode"
	st.compose = []services.Service{
		{Name: "postgres", Backend: "compose", Image: "postgres:16", Ports: port(5432), HasHealthcheck: true},
		{Name: "redis", Backend: "compose", Image: "redis:7", Ports: port(6379)},
		{Name: "api", Backend: "compose", Ports: port(8080), DependsOn: []string{"postgres"}},
		{Name: "mailhog", Backend: "compose", Image: "mailhog/mailhog", Ports: port(8025)},
	}
	st.status = map[string]services.Status{
		"postgres": {Name: "postgres", State: services.Healthy, Health: "healthy", Text: "Up 2 hours (healthy)", Image: "postgres:16",
			StartedAt: now.Add(-(2*time.Hour + 14*time.Minute)), Ports: []services.Port{{Host: "0.0.0.0", Published: 5432, Target: 5432, Proto: "tcp"}}},
		"redis": {Name: "redis", State: services.Running, Text: "Up 2 hours", Image: "redis:7",
			StartedAt: now.Add(-(2*time.Hour + 14*time.Minute)), Ports: port(6379)},
		"api": {Name: "api", State: services.Starting, Health: "starting", Text: "Up 12 seconds (health: starting)",
			StartedAt: now.Add(-12 * time.Second), Ports: port(8080)},
	}
	st.localSt = map[string]services.Status{"web": {Name: "web", State: services.Running, StartedAt: now.Add(-62 * time.Minute)}}
	st.cursor = 1 // postgres
	if variant == "down" {
		st.problem = services.Problem{Kind: services.ProblemDaemonDown}
		st.cursor = 3
	}
	st.touch()
}

// containersDemo is the Containers view with a canned host (DevActionMsg
// "contdemo"): the PROJECT of servicesDemo plus containers, images,
// networks, volumes, contexts and a registry. variant "down": the engine
// is not running; "noengine": no docker / podman; "menu": the ⋯ menu.
func (m *Model) containersDemo(variant string) {
	m.servicesDemo("")
	st := m.services
	st.sections = map[string]bool{}
	st.containerDetected = true
	st.containerEngine = containers.Engine{Binary: "docker", Kind: "docker", Reachable: true,
		Version: containers.Version{Version: "27.3.1"}}
	compose := func(project, service string) map[string]string {
		workingDir, configFiles := st.root, "compose.yaml"
		if project == "shop" {
			workingDir = st.root + "/docs"
			configFiles = "compose.yaml"
		}
		return map[string]string{"com.docker.compose.project": project, "com.docker.compose.service": service,
			"com.docker.compose.container-number": "1", "com.docker.compose.project.working_dir": workingDir,
			"com.docker.compose.project.config_files": configFiles}
	}
	st.containerRows = []containers.Container{
		{ID: "9f3c2e1a", Names: []string{"termocode-postgres-1"}, Image: "postgres:16", State: "running", Status: "Up 2 hours (healthy)", Project: "termocode", Labels: compose("termocode", "postgres"), PortsText: "0.0.0.0:5432->5432/tcp", Mounts: []string{"termocode_db"}},
		{ID: "a1b2c3d4", Names: []string{"termocode-redis-1"}, Image: "redis:7", State: "running", Status: "Up 2 hours", Project: "termocode", Labels: compose("termocode", "redis"), PortsText: "0.0.0.0:6379->6379/tcp"},
		{ID: "b2c3d4e5", Names: []string{"termocode-api-1"}, Image: "termocode-api", State: "exited", Status: "Exited (1) 3 minutes ago", Project: "termocode", Labels: compose("termocode", "api"), Networks: []string{"termocode_default"}},
		{ID: "f3e4d5c6", Names: []string{"shop-web-1"}, Image: "ghcr.io/acme/shop:latest", State: "running", Status: "Up 3 hours", Project: "shop", Labels: compose("shop", "web")},
		{ID: "e4d5c6b7", Names: []string{"shop-worker-1"}, Image: "ghcr.io/acme/shop:latest", State: "running", Status: "Up 3 hours", Project: "shop", Labels: compose("shop", "worker")},
		{ID: "aabbccdd", Names: []string{"nginx"}, Image: "nginx:1.27", State: "running", Status: "Up 3 days", PortsText: "0.0.0.0:8081->80/tcp"},
		{ID: "55667788", Names: []string{"builder"}, Image: "golang:1.23", State: "paused", Status: "Up 1 hour (Paused)"},
		{ID: "11223344", Names: []string{"migrate"}, Image: "termocode-migrate", State: "exited", Status: "Exited (1) 2 hours ago"},
	}
	for i := range st.containerRows {
		if st.containerRows[i].Project != "" {
			st.containerRows[i].WorkingDir = st.containerRows[i].Labels["com.docker.compose.project.working_dir"]
			st.containerRows[i].ConfigFiles = []string{st.containerRows[i].Labels["com.docker.compose.project.config_files"]}
		}
	}
	st.imageRows = []containers.Image{
		{ID: "sha256:0a1b2c3d4e5f", RepoTags: []string{"postgres:16"}, Size: "421MB"},
		{ID: "sha256:1b2c3d4e5f6a", RepoTags: []string{"postgres:15-alpine"}, Size: "239MB"},
		{ID: "sha256:2c3d4e5f6a7b", RepoTags: []string{"redis:7"}, Size: "116MB"},
		{ID: "sha256:3d4e5f6a7b8c", Size: "88MB"},
	}
	st.networkRows = []containers.Network{{Name: "bridge", Driver: "bridge", Scope: "local"}, {Name: "termocode_default", Driver: "bridge", Scope: "local"}, {Name: "host", Driver: "host", Scope: "local"}}
	st.volumeRows = []containers.Volume{{Name: "termocode_db", Driver: "local"}, {Name: "cache", Driver: "local"}}
	st.contextRows = []containers.Context{{Name: "default", Current: true, DockerEndpoint: "unix:///var/run/docker.sock"}, {Name: "prod", DockerEndpoint: "ssh://deploy@prod"}}
	st.registryRows = []containers.Registry{{Host: "ghcr.io", CredsStore: "desktop", SignedIn: true}}
	for _, sec := range hostSectionOrder {
		st.hostLoaded[sec] = true
	}
	st.collapsed[hostSection("images")] = false
	st.collapsed[hostGroup("containers", "shop")] = true
	st.cursor = 1 // postgres
	switch variant {
	case "remove":
		for i, r := range st.rows() {
			if r.kind == svcRowHostItem && r.host == hostContainer && r.hostID == "11223344" {
				st.cursor = i
				break
			}
		}
	case "stop-remove":
		for i, r := range st.rows() {
			if r.kind == svcRowHostItem && r.host == hostContainer && r.hostID == "aabbccdd" {
				st.cursor = i
				break
			}
		}
	case "compose-down":
		for i, r := range st.rows() {
			if r.kind == svcRowGroup && strings.HasPrefix(r.section, "grp/containers/") && r.name == "shop" {
				st.cursor = i
				break
			}
		}
	case "images":
		st.collapsed[hostSection("images")] = false
	case "networks":
		st.collapsed[hostSection("networks")] = false
	case "volumes":
		st.collapsed[hostSection("volumes")] = false
	}
	switch variant {
	case "images":
		for i, r := range st.rows() {
			if r.kind == svcRowSection && r.section == hostSection("images") {
				st.cursor = i
				break
			}
		}
	case "networks":
		for i, r := range st.rows() {
			if r.kind == svcRowHostItem && r.host == hostNetwork {
				st.cursor = i
				break
			}
		}
	case "network-users":
		st.collapsed[hostSection("networks")] = false
		st.collapsed["item/network/termocode_default"] = false
		st.storageInspectState["item/network/termocode_default"] = "done"
		st.storageUsers["item/network/termocode_default"] = []string{"termocode-api-1"}
		for i, r := range st.rows() {
			if r.kind == svcRowHostItem && r.host == hostNetwork && r.hostID == "termocode_default" {
				st.cursor = i
				break
			}
		}
	case "volumes":
		for i, r := range st.rows() {
			if r.kind == svcRowHostItem && r.host == hostVolume {
				st.cursor = i
				break
			}
		}
	case "system-prune":
		m.askPrune(pruneSystem)
	case "image-prune":
		for i, r := range st.rows() {
			if r.kind == svcRowSection && r.section == hostSection("images") {
				st.cursor = i
				break
			}
		}
		m.askImagePrune()
	case "network-remove":
		st.collapsed[hostSection("networks")] = false
		st.storageInspectState["item/network/termocode_default"] = "done"
		st.storageUsers["item/network/termocode_default"] = []string{"termocode-api-1"}
		for i, r := range st.rows() {
			if r.kind == svcRowHostItem && r.host == hostNetwork && r.hostID == "termocode_default" {
				st.cursor = i
				m.askStorageRemove(r)
				break
			}
		}
	case "volume-remove":
		st.collapsed[hostSection("volumes")] = false
		st.storageInspectState["item/volume/cache"] = "done"
		for i, r := range st.rows() {
			if r.kind == svcRowHostItem && r.host == hostVolume && r.hostID == "cache" {
				st.cursor = i
				m.askStorageRemove(r)
				break
			}
		}
	case "volume-prune":
		st.collapsed[hostSection("volumes")] = false
		for i, r := range st.rows() {
			if r.kind == svcRowSection && r.section == hostSection("volumes") {
				st.cursor = i
				break
			}
		}
		m.showStoragePrune("volumes", []string{"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"})
	case "image-remove":
		for i, r := range st.rows() {
			if r.kind == svcRowHostItem && r.host == hostImage && strings.Contains(r.hostID, "3d4e5f6a7b8c") {
				st.cursor = i
				m.askImageRemove(st.imageRows[r.idx])
				break
			}
		}
	case "image-in-use":
		for i, r := range st.rows() {
			if r.kind == svcRowHostItem && r.host == hostImage && r.tag == "16" {
				st.cursor = i
				m.askImageRemove(st.imageRows[r.idx])
				break
			}
		}
	case "network-prune":
		st.collapsed[hostSection("networks")] = false
		for i, r := range st.rows() {
			if r.kind == svcRowSection && r.section == hostSection("networks") {
				st.cursor = i
				break
			}
		}
		m.askStoragePrune("networks")
	case "volume-in-use":
		st.collapsed[hostSection("volumes")] = false
		st.storageInspectState["item/volume/termocode_db"] = "done"
		st.storageUsers["item/volume/termocode_db"] = []string{"termocode-postgres-1"}
		for i, r := range st.rows() {
			if r.kind == svcRowHostItem && r.host == hostVolume && r.hostID == "termocode_db" {
				st.cursor = i
				m.askStorageRemove(r)
				break
			}
		}
	case "down":
		st.containerEngine.Reachable = false
		st.containerEngine.Problem = services.Problem{Kind: services.ProblemDaemonDown}
		// The contexts stay: `docker context ls` reads files, not the daemon.
		st.containerRows, st.imageRows, st.networkRows, st.volumeRows = nil, nil, nil, nil
		st.problem = services.Problem{Kind: services.ProblemDaemonDown}
	case "noengine":
		st.containerEngine = containers.Engine{}
		st.containerRows, st.imageRows, st.networkRows, st.volumeRows, st.contextRows = nil, nil, nil, nil, nil
	case "registries", "registry-connect":
		// REGISTRIES open, ghcr.io open with its local image; the other
		// sections folded so it fits.
		st.collapsed[hostSection("images")] = true
		st.collapsed[hostSection("containers")] = true
		st.collapsed[hostSection("registries")] = false
		st.imageRows = append(st.imageRows, containers.Image{ID: "sha256:4e5f6a7b8c9d", RepoTags: []string{"ghcr.io/acme/shop:latest"}, Size: "212MB"})
		st.registryRows = []containers.Registry{
			{Host: "https://index.docker.io/v1/", CredsStore: "desktop", SignedIn: true},
			{Host: "ghcr.io", CredsStore: "desktop", SignedIn: true},
			{Host: "quay.io", CredsStore: "desktop", CredHelper: "pass"},
		}
		st.registryUsers = map[string]string{"ghcr.io": "amin-jalali"}
		st.collapsed[registryFoldKey("ghcr.io")] = false
		for i, r := range st.rows() {
			if r.kind == svcRowHostItem && r.host == hostRegistry && r.hostID == "ghcr.io" {
				st.cursor = i
				break
			}
		}
		if variant == "registry-connect" {
			st.regPending = registryPending{host: "ghcr.io", user: "amin-jalali"}
			m.askRegistryToken()
			for _, r := range "demo-not-a-real-token" {
				m.prompt, _ = m.prompt.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			}
		}
	case "help":
		// HELP open on its first row; the big sections folded.
		for _, sec := range []string{"containers", "images", "networks", "volumes", "contexts"} {
			st.collapsed[hostSection(sec)] = true
		}
		st.collapsed[hostSection("help")] = false
		for i, r := range st.rows() {
			if r.kind == svcRowHostItem && r.host == hostHelpGuide {
				st.cursor = i
				break
			}
		}
	case "filter": // CP-12: the "/" filter while typing
		st.filter, st.filterTyping = "pos", true
		m.servicesCursorTo(func(r svcRow) bool { return r.kind == svcRowHostItem && r.host == hostContainer })
	case "tags": // CP-12: remote tags under an image repo, a private repo in REGISTRIES
		st.collapsed[hostSection("containers")] = true
		st.collapsed[hostSection("registries")] = false
		pg, _ := containers.ParseRemoteRepo("postgres")
		st.remoteTags = map[string]*remoteTags{
			"postgres": {repo: pg, ref: "postgres", inImages: true, open: true, loaded: true,
				tags: []string{"17", "17-alpine", "16", "16-alpine", "15", "15-alpine"}},
		}
		priv, _ := containers.ParseRemoteRepo("ghcr.io/acme/shop")
		st.remoteTags["ghcr.io/acme/shop"] = &remoteTags{repo: priv, ref: "ghcr.io/acme/shop", open: true, loaded: true, err: containers.ErrNeedsSignIn.Error()}
		st.remoteOrder = []string{"ghcr.io/acme/shop"}
		m.servicesCursorTo(func(r svcRow) bool { return r.host == hostRemoteTag && r.tag == "16-alpine" })
	case "menu":
		m.openContainersSectionsMenu("")
	case "db-menu": // the row menu of the postgres compose service
		m.openServicesMenu(svcRow{kind: svcRowService, section: svcSecCompose, name: "postgres"}, activity.Width+6, 6)
	case "db-container-menu": // the row menu of the postgres container
		for i, r := range st.rows() {
			if r.kind == svcRowHostItem && r.host == hostContainer && r.hostID == "9f3c2e1a" {
				st.cursor = i
				m.openContainerMenu(r, activity.Width+6, 6)
				break
			}
		}
	}
	m.servicesClampCursor()
	st.touch()
}
