package app

import (
	"time"

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
		return map[string]string{"com.docker.compose.project": project, "com.docker.compose.service": service, "com.docker.compose.container-number": "1"}
	}
	st.containerRows = []containers.Container{
		{ID: "9f3c2e1a", Names: []string{"termocode-postgres-1"}, Image: "postgres:16", State: "running", Status: "Up 2 hours (healthy)", Project: "termocode", Labels: compose("termocode", "postgres"), PortsText: "0.0.0.0:5432->5432/tcp"},
		{ID: "a1b2c3d4", Names: []string{"termocode-redis-1"}, Image: "redis:7", State: "running", Status: "Up 2 hours", Project: "termocode", Labels: compose("termocode", "redis"), PortsText: "0.0.0.0:6379->6379/tcp"},
		{ID: "b2c3d4e5", Names: []string{"termocode-api-1"}, Image: "termocode-api", State: "exited", Status: "Exited (1) 3 minutes ago", Project: "termocode", Labels: compose("termocode", "api")},
		{ID: "f3e4d5c6", Names: []string{"shop-web-1"}, Image: "ghcr.io/acme/shop:latest", State: "running", Status: "Up 3 hours", Project: "shop", Labels: compose("shop", "web")},
		{ID: "e4d5c6b7", Names: []string{"shop-worker-1"}, Image: "ghcr.io/acme/shop:latest", State: "running", Status: "Up 3 hours", Project: "shop", Labels: compose("shop", "worker")},
		{ID: "aabbccdd", Names: []string{"nginx"}, Image: "nginx:1.27", State: "running", Status: "Up 3 days", PortsText: "0.0.0.0:8081->80/tcp"},
		{ID: "55667788", Names: []string{"builder"}, Image: "golang:1.23", State: "paused", Status: "Up 1 hour (Paused)"},
		{ID: "11223344", Names: []string{"migrate"}, Image: "termocode-migrate", State: "exited", Status: "Exited (1) 2 hours ago"},
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
	st.registryRows = []containers.Registry{{Host: "ghcr.io", CredsStore: "desktop"}}
	for _, sec := range hostSectionOrder {
		st.hostLoaded[sec] = true
	}
	st.collapsed[hostSection("images")] = false
	st.collapsed[hostGroup("containers", "shop")] = true
	st.cursor = 1 // postgres
	switch variant {
	case "down":
		st.containerEngine.Reachable = false
		st.containerEngine.Problem = services.Problem{Kind: services.ProblemDaemonDown}
		st.containerRows, st.imageRows, st.networkRows, st.volumeRows, st.contextRows = nil, nil, nil, nil, nil
		st.problem = services.Problem{Kind: services.ProblemDaemonDown}
	case "noengine":
		st.containerEngine = containers.Engine{}
		st.containerRows, st.imageRows, st.networkRows, st.volumeRows, st.contextRows = nil, nil, nil, nil, nil
	case "menu":
		m.openContainersSectionsMenu("")
	}
	m.servicesClampCursor()
	st.touch()
}
