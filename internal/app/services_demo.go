package app

import (
	"time"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/clock"
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
