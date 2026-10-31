package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── AI tools for the Services view (SV-9) ────────────────────────────────
//
//   - services_list, services_status, services_logs run at once (read
//     only). A refresh runs first, so the model never reads a stale row.
//   - services_start, services_stop, services_restart ask (class
//     "services") unless the user chose "Always allow services" or
//     "Always in this project" (session file, never the repo).
//   - Env values never reach the model: names only; .env and compose
//     secrets are registered with ai.RegisterSecret; logs and status pass
//     ai.Redact and services.Redact.
//   - down, --force-recreate, exec and volume work are not tools.

// aiServicesPrompt is the system prompt line when the project has services.
const aiServicesPrompt = "Services tools (services_*) show the project's dev services (Docker Compose or local commands); " +
	"use them for status and logs before suggesting commands, and never run `docker compose down` or remove volumes."

const (
	aiServicesLoadWait = 60 * time.Second
	aiServicesPoll     = 500 * time.Millisecond
	aiServicesMaxLines = 1000
)

// servicesOpenHidden makes the state usable without showing the view (no
// watcher: that needs the view on screen).
func (m *Model) servicesOpenHidden() {
	st := m.services
	if !st.opened {
		st.opened = true
		st.root = m.taskVars().WorkspaceFolder
		m.servicesLoadSession()
	}
}

// servicesRefreshTarget starts a refresh (or queues one) and returns the
// snapshot seq that will hold its result.
func (m *Model) servicesRefreshTarget() (uint64, tea.Cmd) {
	m.servicesOpenHidden()
	st := m.services
	if st.inflight {
		st.pending = true
		return st.reqSeq + 1, nil
	}
	cmd := m.servicesRefresh()
	return st.reqSeq, cmd
}

// aiServicesFresh refreshes the snapshot and waits for it.
func aiServicesFresh(ctx context.Context, call aiIDECaller) error {
	var target uint64
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		t, cmd := m.servicesRefreshTarget()
		target = t
		return "", cmd, nil
	}); err != nil {
		return err
	}
	timedOut, err := aiPollUntil(ctx, aiServicesLoadWait, 50*time.Millisecond, func() (bool, error) {
		out, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
			if m.services.doneSeq >= target {
				return "done", nil, nil
			}
			return "", nil, nil
		})
		return out == "done", err
	})
	if err != nil {
		return err
	}
	if timedOut {
		return errors.New("docker did not answer in time")
	}
	return nil
}

// aiServicesNames lists "section/name" of every declared service.
func (st *servicesState) aiNames() []string {
	var out []string
	for _, s := range st.compose {
		out = append(out, s.Name)
	}
	for _, s := range st.local {
		out = append(out, s.Name)
	}
	return out
}

// aiFind maps a model-given name to (section, name); compose first.
func (st *servicesState) aiFind(name string) (string, bool) {
	if _, ok := st.findService(svcSecCompose, name); ok {
		return svcSecCompose, true
	}
	if _, ok := st.findService(svcSecLocal, name); ok {
		return svcSecLocal, true
	}
	return "", false
}

// aiServicesValidate checks name / all against the loaded services.
func aiServicesValidate(call aiIDECaller, a map[string]any, allowAll bool) error {
	name := strings.TrimSpace(ai.ArgString(a, "name"))
	all := ai.ArgBool(a, "all")
	if all && !allowAll {
		return errors.New("pass one service name")
	}
	if name == "" && !all {
		if allowAll {
			return errors.New("pass name (see services_list) or all: true")
		}
		return errors.New("pass name (see services_list)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), aiServicesLoadWait)
	defer cancel()
	if err := aiServicesFresh(ctx, call); err != nil {
		return err
	}
	_, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		st := m.services
		if len(st.aiNames()) == 0 {
			return "", nil, errors.New("this project has no services (no compose.yaml, Procfile or .termocode/services.json)")
		}
		if all {
			return "", nil, nil
		}
		if _, ok := st.aiFind(name); !ok {
			return "", nil, fmt.Errorf("unknown service %q; known: %s", name, strings.Join(st.aiNames(), ", "))
		}
		return "", nil, nil
	})
	return err
}

// aiServicesRedact masks secrets in text for the model and counts them.
func aiServicesRedact(s string) (string, int) {
	before := strings.Count(s, "[REDACTED]")
	s, _ = services.Redact(ai.Redact(s))
	return s, strings.Count(s, "[REDACTED]") - before
}

// ── Reports ──────────────────────────────────────────────────────────────

// aiServicesListReport is the services_list text.
func (m *Model) aiServicesListReport() string {
	st := m.services
	var b strings.Builder
	d := st.det
	if d.Empty() {
		return "This project has no services: no compose.yaml / compose.yml / docker-compose.yml, no Procfile and no .termocode/services.json."
	}
	if len(d.ComposeFiles) > 0 {
		fmt.Fprintf(&b, "Docker Compose: files %s", strings.Join(d.ComposeFiles, ", "))
		if st.project != "" {
			fmt.Fprintf(&b, ", project %q", st.project)
		}
		if len(d.Profiles) > 0 {
			fmt.Fprintf(&b, ", profiles %s", strings.Join(d.Profiles, ", "))
		}
		b.WriteString("\n")
		lines := map[string]int{}
		if len(d.ComposeFiles) == 1 {
			if src, err := os.ReadFile(svcAbs(st.root, d.ComposeFiles[0])); err == nil {
				lines = services.ComposeServiceLines(string(src))
			}
		}
		for _, s := range st.compose {
			b.WriteString(aiServiceLine(s, st.statusOf(svcSecCompose, s.Name)))
			if l := lines[s.Name]; l > 0 {
				fmt.Fprintf(&b, "; source %s:%d", d.ComposeFiles[0], l)
			}
			b.WriteString("\n")
		}
	}
	if len(st.local) > 0 || d.ConfigErr != nil {
		b.WriteString("Local commands (each runs in its own terminal tab):\n")
		if d.ConfigErr != nil {
			fmt.Fprintf(&b, "(%s)\n", d.ConfigErr)
		}
		for _, s := range st.local {
			b.WriteString(aiServiceLine(s, st.statusOf(svcSecLocal, s.Name)))
			fmt.Fprintf(&b, "; command %q; source %s\n", s.Command, s.Source)
		}
	}
	if p := st.problem; p.Kind != services.ProblemNone {
		fmt.Fprintf(&b, "Problem: %s\n", p.Message())
	}
	b.WriteString("Read state with services_status(name), logs with services_logs(name).")
	out, _ := aiServicesRedact(b.String())
	return out
}

// aiServiceLine is one service of services_list (no env values).
func aiServiceLine(s services.Service, st services.Status) string {
	var parts []string
	if s.Image != "" {
		parts = append(parts, "image "+s.Image)
	} else if s.Backend == svcSecCompose {
		parts = append(parts, "built from the project")
	}
	var ports []string
	for _, p := range s.Ports {
		proto := p.Proto
		if proto == "" {
			proto = "tcp"
		}
		ports = append(ports, fmt.Sprintf("%d→%d/%s", p.Published, p.Target, proto))
	}
	if len(ports) > 0 {
		parts = append(parts, "ports "+strings.Join(ports, ", "))
	}
	if len(s.DependsOn) > 0 {
		parts = append(parts, "depends_on "+strings.Join(s.DependsOn, ", "))
	}
	if len(s.Profiles) > 0 {
		parts = append(parts, "profiles "+strings.Join(s.Profiles, ", "))
	}
	if s.Backend == svcSecCompose {
		hc := "no"
		if s.HasHealthcheck {
			hc = "yes"
		}
		parts = append(parts, "healthcheck "+hc)
	}
	if len(s.EnvKeys) > 0 {
		parts = append(parts, "env keys "+strings.Join(s.EnvKeys, ", "))
	}
	state := st.State.String()
	if st.State == services.Exited {
		state = fmt.Sprintf("exited (%d)", st.ExitCode)
	}
	return fmt.Sprintf("- %s [%s] %s; last state: %s", s.Name, s.Backend, strings.Join(parts, "; "), state)
}

// aiServicesStatusReport is the services_status text for one service
// ("" = all).
func (m *Model) aiServicesStatusReport(name string) string {
	st := m.services
	var b strings.Builder
	one := func(section string, s services.Service) {
		ss := st.statusOf(section, s.Name)
		fmt.Fprintf(&b, "- %s [%s]: %s", s.Name, section, ss.State)
		if ss.Health != "" {
			fmt.Fprintf(&b, ", health %s", ss.Health)
		}
		if ss.State == services.Exited || ss.ExitCode != 0 {
			fmt.Fprintf(&b, ", exit code %d", ss.ExitCode)
		}
		if !ss.StartedAt.IsZero() && ss.State.Up() {
			fmt.Fprintf(&b, ", up for %s", services.FormatUptime(st.at.Sub(ss.StartedAt)))
		}
		if ss.Text != "" {
			fmt.Fprintf(&b, ", status %q", ss.Text)
		}
		var ports []string
		list := ss.Ports
		if len(list) == 0 && section == svcSecLocal {
			list = s.Ports
		}
		for _, p := range list {
			if p.Published == 0 {
				continue
			}
			t := fmt.Sprintf("localhost:%d→%d", p.Published, p.Target)
			if ss.PortOpen != nil {
				if *ss.PortOpen {
					t += " open"
				} else {
					t += " closed"
				}
			}
			ports = append(ports, t)
		}
		if len(ports) > 0 {
			b.WriteString(", ports " + strings.Join(ports, ", "))
		} else if ss.State.Up() {
			b.WriteString(", no published ports")
		}
		b.WriteString("\n")
	}
	for _, s := range st.compose {
		if name == "" || s.Name == name {
			one(svcSecCompose, s)
		}
	}
	for _, s := range st.local {
		if name == "" || s.Name == name {
			one(svcSecLocal, s)
		}
	}
	if p := st.problem; p.Kind != services.ProblemNone {
		fmt.Fprintf(&b, "Problem: %s\n", p.Message())
	}
	out, _ := aiServicesRedact(strings.TrimRight(b.String(), "\n"))
	return out
}

// aiServicesSettled reports whether name reached its wait goal: healthy
// (running without a healthcheck) or exited.
func (st *servicesState) aiSettled(section, name string) bool {
	s := st.statusOf(section, name)
	svc, _ := st.findService(section, name)
	switch s.State {
	case services.Healthy, services.Exited, services.Stopped:
		return true
	case services.Running:
		return !svc.HasHealthcheck
	case services.Unhealthy:
		return true
	}
	return false
}

// ── Logs ─────────────────────────────────────────────────────────────────

// aiServicesLogs captures, redacts and caps the logs of name.
func aiServicesLogs(ctx context.Context, call aiIDECaller, name string, lines int, since time.Duration) (string, error) {
	if err := aiServicesFresh(ctx, call); err != nil {
		return "", err
	}
	var section string
	var comp *services.Compose
	var local string
	maxKB := loadServicesSettings().MaxLogKB
	_, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		st := m.services
		if name == "all" {
			section, comp = svcSecCompose, st.composeBackend()
			if comp == nil {
				return "", nil, errors.New("Docker Compose is not available: " + st.composeProblem().Message())
			}
			return "", nil, nil
		}
		sec, ok := st.aiFind(name)
		if !ok {
			return "", nil, fmt.Errorf("unknown service %q; known: %s", name, strings.Join(st.aiNames(), ", "))
		}
		section = sec
		if sec == svcSecCompose {
			comp = st.composeBackend()
			if comp == nil {
				return "", nil, errors.New("Docker Compose is not available: " + st.composeProblem().Message())
			}
			return "", nil, nil
		}
		t, ok := m.svcTabAny(svcLocalTab(name))
		if !ok || m.nvim == nil {
			return "", nil, fmt.Errorf("local service %q has not run in this session (no terminal tab)", name)
		}
		var got []string
		if err := m.nvim.EvalLuaArgs(`local b, n = ...
if not vim.api.nvim_buf_is_valid(b) then return {} end
local lines = vim.api.nvim_buf_get_lines(b, 0, -1, false)
while #lines > 0 and lines[#lines]:match('^%s*$') do table.remove(lines) end
local from = math.max(#lines - n + 1, 1)
local out = {}
for i = from, #lines do out[#out + 1] = lines[i] end
return out`, &got, t.BufID, lines); err != nil {
			return "", nil, err
		}
		local = strings.Join(got, "\n")
		return "", nil, nil
	})
	if err != nil {
		return "", err
	}
	text := local
	if section == svcSecCompose {
		target := name
		if name == "all" {
			target = ""
		}
		ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		text, err = comp.Logs(ctx2, target, lines, since)
		if err != nil {
			return "", err
		}
	}
	text = strings.TrimRight(text, "\n")
	text, n := aiServicesRedact(text)
	count := 0
	if text != "" {
		count = strings.Count(text, "\n") + 1
	}
	head := fmt.Sprintf("[%d lines, %d redacted values]", count, n)
	if text == "" {
		return head + "\n(no log output)", nil
	}
	limit := maxKB * 1024
	if len(text) > limit {
		cut := len(text) - limit
		// Keep the newest lines: cut from the front, at a line start.
		if i := strings.IndexByte(text[cut:], '\n'); i >= 0 {
			cut += i + 1
		}
		return fmt.Sprintf("%s\n… [truncated, %d more bytes]\n%s", head, cut, text[cut:]), nil
	}
	return head + "\n" + text, nil
}

// svcTabAny finds a tab with key, running or not.
func (m *Model) svcTabAny(key string) (terminalTab, bool) {
	for _, t := range m.terminalTabs {
		if t.Service == key {
			return t, true
		}
	}
	return terminalTab{}, false
}

// ── Mutating tools ───────────────────────────────────────────────────────

// aiServicesGate: ask (class services) unless the project is allowed.
func aiServicesGate(st *servicesState) func(map[string]any) (bool, string) {
	return func(map[string]any) (bool, string) {
		if st != nil && st.aiAllowed.Load() {
			return false, ""
		}
		return true, ai.ClassServices
	}
}

// aiServicesDescribe is the confirm card line of a mutating call.
func aiServicesDescribe(verb string, a map[string]any) string {
	name := ai.ArgString(a, "name")
	if ai.ArgBool(a, "all") || name == "" {
		return strings.ToUpper(verb[:1]) + verb[1:] + " all services"
	}
	cmd := map[string]string{"start": "docker compose up -d ", "stop": "docker compose stop ", "restart": "docker compose restart "}[verb]
	return fmt.Sprintf("%s service %s (%s%s)", strings.ToUpper(verb[:1])+verb[1:], name, cmd, name)
}

func aiServicesStart(ctx context.Context, call aiIDECaller, name string, all bool) (string, error) {
	return call(ctx, func(m *Model) (string, tea.Cmd, error) {
		st := m.services
		section := svcSecCompose
		if !all {
			section, _ = st.aiFind(name)
		} else {
			name = ""
		}
		if section == svcSecLocal {
			if _, running := m.svcTabRunning(svcLocalTab(name)); running {
				return fmt.Sprintf("%s is already running (no change)", name), nil, nil
			}
			svc, _ := st.findService(svcSecLocal, name)
			if !st.localOK[svcCommandHash(svc)] {
				// The AI's start was approved: the command is OK too.
				st.localOK[svcCommandHash(svc)] = true
				m.servicesSaveSession()
			}
			cmd := m.servicesSpawnLocal(svc)
			if _, ok := m.svcTabRunning(svcLocalTab(name)); !ok {
				return "", cmd, fmt.Errorf("could not open a terminal tab for %s; nothing started", name)
			}
			return fmt.Sprintf("started local service %s in its terminal tab (%q); call services_status(name: %q, wait_ms: 20000) to wait",
				name, svc.Command, name), cmd, nil
		}
		if len(st.compose) == 0 {
			return "", nil, errors.New("no Docker Compose services here")
		}
		note, cmd := m.servicesStart(svcSecCompose, name)
		if note != "" {
			return note + " (no change)", cmd, nil
		}
		if _, ok := m.svcTabRunning(svcTabUp); !ok {
			return "", cmd, errors.New("could not open the terminal tab services: up; nothing started")
		}
		if name == "" {
			return "started `docker compose up -d` in the terminal tab services: up; call services_status(name, wait_ms: 30000) to wait for a service", cmd, nil
		}
		return fmt.Sprintf("started `docker compose up -d %s` in the terminal tab services: up; call services_status(name: %q, wait_ms: 30000) to wait for healthy",
			name, name), cmd, nil
	})
}

// aiServicesWaitDone runs a stop / restart and waits until it is over.
func aiServicesWaitDone(ctx context.Context, call aiIDECaller, start func(m *Model, done func(*Model, error)) (string, tea.Cmd)) (string, error) {
	type result struct{ err error }
	ch := make(chan result, 1)
	send := func(r result) { // first answer wins; never blocks Update
		select {
		case ch <- r:
		default:
		}
	}
	note, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		note, cmd := start(m, func(_ *Model, err error) { send(result{err}) })
		if note != "" {
			send(result{})
		}
		return note, cmd, nil
	})
	if err != nil {
		return "", err
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return "", r.err
		}
	case <-ctx.Done():
		return "", errAIChatStopped
	case <-time.After(70 * time.Second):
		return "", errors.New("docker compose did not finish in 70 s")
	}
	return note, nil
}

func aiServicesStop(ctx context.Context, call aiIDECaller, name string, all bool) (string, error) {
	var section string
	if !all {
		if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
			section, _ = m.services.aiFind(name)
			return "", nil, nil
		}); err != nil {
			return "", err
		}
	}
	note, err := aiServicesWaitDone(ctx, call, func(m *Model, done func(*Model, error)) (string, tea.Cmd) {
		if all {
			var cmds []tea.Cmd
			n, c := m.servicesStop(svcSecCompose, "", done)
			cmds = append(cmds, c)
			for _, s := range m.services.local {
				_, c := m.servicesStopLocal(s.Name, nil)
				cmds = append(cmds, c)
			}
			if len(m.services.compose) == 0 {
				done(m, nil)
				return "", tea.Batch(cmds...)
			}
			return n, tea.Batch(cmds...)
		}
		return m.servicesStop(section, name, done)
	})
	if err != nil {
		return "", err
	}
	if note != "" {
		return note + " (no change)", nil
	}
	if err := aiServicesFresh(ctx, call); err != nil {
		return "", err
	}
	target := ""
	if !all {
		target = name
	}
	return call(ctx, func(m *Model) (string, tea.Cmd, error) {
		return "stopped. Now:\n" + m.aiServicesStatusReport(target), nil, nil
	})
}

func aiServicesRestart(ctx context.Context, call aiIDECaller, name string) (string, error) {
	var section string
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		section, _ = m.services.aiFind(name)
		return "", nil, nil
	}); err != nil {
		return "", err
	}
	note, err := aiServicesWaitDone(ctx, call, func(m *Model, done func(*Model, error)) (string, tea.Cmd) {
		if section == svcSecLocal {
			// The AI's restart was approved: the command is OK too.
			svc, _ := m.services.findService(svcSecLocal, name)
			m.services.localOK[svcCommandHash(svc)] = true
			m.servicesSaveSession()
			c := m.servicesSpawnLocal(svc)
			done(m, nil)
			return "", c
		}
		return m.servicesRestart(section, name, done)
	})
	if err != nil {
		return "", err
	}
	if note != "" {
		return note + " (no change)", nil
	}
	// A service that dies at once is the common failure: look again 2 s on.
	select {
	case <-ctx.Done():
		return "", errAIChatStopped
	case <-time.After(2 * time.Second):
	}
	if err := aiServicesFresh(ctx, call); err != nil {
		return "", err
	}
	return call(ctx, func(m *Model) (string, tea.Cmd, error) {
		s := m.services.statusOf(section, name)
		rep := m.aiServicesStatusReport(name)
		if s.State == services.Exited && s.ExitCode != 0 {
			return fmt.Sprintf("restarted, but %s exited (%d) 2 s after restart — read services_logs(name: %q).\n%s", name, s.ExitCode, name, rep), nil, nil
		}
		return "restarted. Now:\n" + rep, nil, nil
	})
}

// ── Tool definitions ─────────────────────────────────────────────────────

// aiServicesTools are services_list / status / logs / start / stop /
// restart.
func (m *Model) aiServicesTools(call aiIDECaller) []ai.Tool {
	st := m.services
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	num := func(d string) map[string]any { return map[string]any{"type": "integer", "description": d} }
	boolean := func(d string) map[string]any { return map[string]any{"type": "boolean", "description": d} }
	gate := aiServicesGate(st)
	return []ai.Tool{
		{
			Name: "services_list",
			Description: "List the project's dev services: Docker Compose services (compose.yaml) and local commands " +
				"(.termocode/services.json, Procfile). Per service: image, declared ports (published→target), depends_on, " +
				"profiles, healthcheck, env key NAMES (never values), source file:line, last known state.",
			Params:   map[string]any{},
			Describe: func(map[string]any) string { return "List services" },
			Run: func(ctx context.Context, _ map[string]any) (string, error) {
				if err := aiServicesFresh(ctx, call); err != nil {
					return "", err
				}
				return call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiServicesListReport(), nil, nil })
			},
		},
		{
			Name: "services_status",
			Description: "Current state of one service (name) or all: running / healthy / unhealthy / exited (code), " +
				"up since, docker's status text, published ports and whether they accept a TCP connection. " +
				"wait_ms waits (max 120000) until the service is healthy (running when it has no healthcheck) or exits.",
			Params: map[string]any{
				"name":    str("service name from services_list; default all"),
				"wait_ms": num("wait up to this long (ms, max 120000) for healthy / running / exited"),
				"probe":   boolean("TCP-probe published ports (default true)"),
			},
			Validate: func(a map[string]any) error {
				if ai.ArgString(a, "name") == "" {
					return nil
				}
				return aiServicesValidate(call, a, false)
			},
			Describe: func(a map[string]any) string { return "Service status " + ai.ArgString(a, "name") },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				name := strings.TrimSpace(ai.ArgString(a, "name"))
				wait := aiWaitArg(a)
				var timedOut bool
				if name != "" && wait > 0 {
					t, err := aiPollUntil(ctx, wait, aiServicesPoll, func() (bool, error) {
						if err := aiServicesFresh(ctx, call); err != nil {
							return false, err
						}
						out, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
							sec, _ := m.services.aiFind(name)
							if m.services.aiSettled(sec, name) {
								return "done", nil, nil
							}
							return "", nil, nil
						})
						return out == "done", err
					})
					if err != nil {
						return "", err
					}
					timedOut = t
				} else if err := aiServicesFresh(ctx, call); err != nil {
					return "", err
				}
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					out := m.aiServicesStatusReport(name)
					if timedOut {
						out = fmt.Sprintf("still not settled after %s:\n%s", wait, out)
					}
					return out, nil, nil
				})
			},
		},
		{
			Name: "services_logs",
			Description: "The last log lines of a service (name, or \"all\" for every compose service), with secrets " +
				"redacted. lines default 100, max 1000; since like \"10m\" (compose only). Local services: their terminal tab.",
			Params: map[string]any{
				"name":  str("service name from services_list, or \"all\""),
				"lines": num("how many last lines (default 100, max 1000)"),
				"since": str("only newer than this, e.g. 10m or 1h (compose only)"),
			},
			Required: []string{"name"},
			Validate: func(a map[string]any) error {
				if s := ai.ArgString(a, "since"); s != "" {
					if _, err := time.ParseDuration(s); err != nil {
						return fmt.Errorf("since %q is not a duration like 10m", s)
					}
				}
				if ai.ArgString(a, "name") == "all" {
					return nil
				}
				return aiServicesValidate(call, a, false)
			},
			Describe: func(a map[string]any) string { return "Service logs " + ai.ArgString(a, "name") },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				lines := ai.ArgInt(a, "lines")
				if lines <= 0 {
					lines = 100
				}
				lines = min(lines, aiServicesMaxLines)
				since, _ := time.ParseDuration(ai.ArgString(a, "since"))
				return aiServicesLogs(ctx, call, strings.TrimSpace(ai.ArgString(a, "name")), lines, since)
			},
		},
		{
			Name: "services_start",
			Description: "Start a service (name) or all (all: true): compose runs `docker compose up -d` in a visible " +
				"terminal tab (pulls show there); a local service runs in its own tab. Then call services_status(name, wait_ms).",
			Params: map[string]any{
				"name": str("service name from services_list"),
				"all":  boolean("start every service"),
			},
			Mutating: true, Class: ai.ClassServices, Gate: gate,
			Validate: func(a map[string]any) error { return aiServicesValidate(call, a, true) },
			Describe: func(a map[string]any) string { return aiServicesDescribe("start", a) },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				return aiServicesStart(ctx, call, strings.TrimSpace(ai.ArgString(a, "name")), ai.ArgBool(a, "all"))
			},
		},
		{
			Name: "services_stop",
			Description: "Stop a service (name) or all (all: true): `docker compose stop` (containers and volumes stay) " +
				"or SIGTERM to a local service. Returns the new state.",
			Params: map[string]any{
				"name": str("service name from services_list"),
				"all":  boolean("stop every service"),
			},
			Mutating: true, Class: ai.ClassServices, Gate: gate,
			Validate: func(a map[string]any) error { return aiServicesValidate(call, a, true) },
			Describe: func(a map[string]any) string { return aiServicesDescribe("stop", a) },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				return aiServicesStop(ctx, call, strings.TrimSpace(ai.ArgString(a, "name")), ai.ArgBool(a, "all"))
			},
		},
		{
			Name:        "services_restart",
			Description: "Restart one service (`docker compose restart`, or stop + start for a local one). Returns the new state; says so when it exits again at once.",
			Params:      map[string]any{"name": str("service name from services_list")},
			Required:    []string{"name"},
			Mutating:    true, Class: ai.ClassServices, Gate: gate,
			Validate: func(a map[string]any) error { return aiServicesValidate(call, a, false) },
			Describe: func(a map[string]any) string { return aiServicesDescribe("restart", a) },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				return aiServicesRestart(ctx, call, strings.TrimSpace(ai.ArgString(a, "name")))
			},
		},
	}
}

// aiRedactResults passes every tool result through ai.RedactSecrets: a
// secret the IDE registered (the provider's API key, a password from the
// project's .env or compose file) is masked in read_file, run_command
// and every other result, not only in the services tools.
func aiRedactResults(tools []ai.Tool) []ai.Tool {
	for i := range tools {
		run := tools[i].Run
		if run == nil {
			continue
		}
		tools[i].Run = func(ctx context.Context, a map[string]any) (string, error) {
			out, err := run(ctx, a)
			return ai.RedactSecrets(out), err
		}
	}
	return tools
}

// aiServicesPromptLine is the system prompt line, only when the project
// declares services (a few stats; no process).
func (m *Model) aiServicesPromptLine(root string) string {
	if d := services.Detect(root, os.Getenv, func(string) (string, error) { return "", nil }, "linux"); d.Empty() {
		return ""
	}
	return "\n" + aiServicesPrompt
}

// ── Confirm card: "Always in this project" ───────────────────────────────

// aiServicesConfirmButton is the extra button of a class-services card.
func aiServicesConfirmButton(class string) []confirm.Button {
	if class != ai.ClassServices {
		return nil
	}
	return []confirm.Button{{ID: "project", Title: "Always in project"}}
}

// aiServicesAllowProject stores "Always in this project".
func (m *Model) aiServicesAllowProject() {
	m.servicesOpenHidden()
	m.services.aiAllowed.Store(true)
	m.servicesSaveSession()
}

// openServicesTrust shows the project's AI trust with "Forget" (AI:
// Services Trust…, user only).
func (m *Model) openServicesTrust() tea.Cmd {
	m.servicesOpenHidden()
	if !m.services.aiAllowed.Load() {
		return m.servicesToast(toast.Info, "The AI asks before it starts or stops services here",
			"\"Always in this project\" on its confirm card stops the questions")
	}
	items := []picker.Item{
		{ID: "svc-trust:keep", Title: "Keep: the AI starts / stops services here without asking"},
		{ID: "svc-trust:forget", Title: "Forget: ask again before every start / stop"},
	}
	m.openTasksPickerItems(" Services Trust ", items)
	return nil
}

// handleServicesTrustSelect answers the trust picker.
func (m *Model) handleServicesTrustSelect(id string) tea.Cmd {
	if id == "svc-trust:forget" {
		m.services.aiAllowed.Store(false)
		m.servicesSaveSession()
		return m.servicesToast(toast.Info, "The AI asks again", "before it starts, stops or restarts services here")
	}
	return nil
}

// svcAbs is p made absolute against root.
func svcAbs(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}
