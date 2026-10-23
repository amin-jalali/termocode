package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
)

// ── AI tools for the Containers view (CP-9) ──────────────────────────────
//
//   - Read tools run at once: containers_list, containers_inspect,
//     containers_logs, containers_stats, images_list, volumes_list,
//     networks_list, contexts_list, registries_list. Each lists the host
//     fresh (no stale snapshot) and names the engine and the context.
//   - Write tools ask with class containers: containers_control (start,
//     stop, restart, pause, unpause), images_pull (a visible pull: tab)
//     and compose_up (a visible compose: tab). The card has Allow ·
//     Always allow containers · Deny; "Always" lasts for this session only
//     (never saved: a host action can touch another project's
//     containers), and there is no "Always in this project".
//   - Every id, name and reference passes containers.ValidName /
//     ValidImageRef and goes into an argv list; a container must be in a
//     fresh listing. Busy rows and no-op actions are refused before the
//     card.
//   - Never offered: remove, prune, push, tag, run, login, logout,
//     context use / default, disk usage.
//   - Env values never reach the model: inspect passes
//     containers.MaskInspect (names stay, values masked), lists show only
//     compose / OCI facts, everything passes ai.Redact and services.Redact,
//     and the workspace .env secrets are registered first.
//   - Calls carry the session's DOCKER_CONTEXT (CP-6), like the view.

// aiContainersPrompt is the system prompt line when an engine exists.
const aiContainersPrompt = "Containers tools (containers_*, images_*, volumes_list, networks_list, contexts_list, registries_list, compose_up) " +
	"show every container, image, volume and network on this host; use them before suggesting `docker` commands; " +
	"you cannot remove or prune anything."

const (
	aiContainersMaxLines   = 1000
	aiContainersCallWait   = 60 * time.Second
	aiContainersListLimit  = 200
	aiContainersCommandMax = 80
)

var aiContainersActions = []string{"start", "stop", "restart", "pause", "unpause"}

// ── Session trust: "Always allow containers" ─────────────────────────────

// aiContainersGate asks with class containers unless the user chose
// "Always allow containers" in this session.
func aiContainersGate(st *servicesState) func(map[string]any) (bool, string) {
	return func(map[string]any) (bool, string) {
		if st != nil && st.aiContainersAllowed != nil && st.aiContainersAllowed.Load() {
			return false, ""
		}
		return true, ai.ClassContainers
	}
}

// aiContainersAllowSession is "Always allow containers": this session
// only, never config.json.
func (m *Model) aiContainersAllowSession() {
	if m.services == nil {
		return
	}
	if m.services.aiContainersAllowed == nil {
		return
	}
	m.services.aiContainersAllowed.Store(true)
}

// aiAutoApproveSaved reports whether a saved ai_auto_approve class may
// auto-approve: containers never does (session only).
func aiAutoApproveSaved(class string) bool { return class != ai.ClassContainers }

// ── Engine, context, root ────────────────────────────────────────────────

// aiContainersHost is the engine the tools use: the view's detected one,
// else the one on PATH; it carries the session's DOCKER_CONTEXT.
type aiContainersHost struct {
	e       containers.Engine
	session string // the session context ("" = the engine's own)
	root    string
	project string // this workspace's compose project
	busy    map[string]bool
	users   map[string]string // registry host → user signed in this run
}

func aiContainersGetHost(ctx context.Context, call aiIDECaller) (aiContainersHost, error) {
	var h aiContainersHost
	if runtime.GOOS == "android" {
		return h, errors.New("no container engine on this device")
	}
	var ok bool
	_, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		st := m.services
		if st == nil {
			return "", nil, errors.New("the Containers view is not available")
		}
		h.e, ok = st.registryEngine()
		h.session = st.sessionContext
		h.root, h.project = st.root, st.project
		if h.root == "" {
			h.root = aiWorkspaceRoot()
		}
		h.busy = map[string]bool{}
		for k, v := range st.busy {
			if strings.HasPrefix(k, containerBusyPrefix) && v.Busy() {
				h.busy[strings.TrimPrefix(k, containerBusyPrefix)] = true
			}
		}
		h.users = map[string]string{}
		for k, v := range st.registryUsers {
			h.users[k] = v
		}
		return "", nil, nil
	})
	if err != nil {
		return h, err
	}
	if !ok {
		return h, errors.New("no container engine installed (Docker or Podman)")
	}
	if h.e.Timeout <= 0 {
		h.e.Timeout = 10 * time.Second
	}
	// The workspace .env secrets: masked in every result.
	for _, v := range services.EnvFileSecrets(filepath.Join(h.root, ".env")) {
		ai.RegisterSecret(v)
	}
	return h, nil
}

// contextLabel names the context the calls use.
func (h aiContainersHost) contextLabel(ctx context.Context) string {
	if h.e.Kind == "podman" {
		switch {
		case h.session != "":
			return "connection " + h.session + " (this session)"
		case os.Getenv("CONTAINER_CONNECTION") != "":
			return "connection " + os.Getenv("CONTAINER_CONNECTION") + " (CONTAINER_CONNECTION)"
		case runtime.GOOS == "linux":
			return "local podman"
		}
	}
	if h.session != "" {
		return "context " + h.session + " (this session)"
	}
	if env := os.Getenv("DOCKER_CONTEXT"); env != "" {
		return "context " + env + " (DOCKER_CONTEXT)"
	}
	if list, err := h.e.ListContexts(ctx); err == nil {
		for _, c := range list {
			if c.Current {
				return "context " + c.Name
			}
		}
	}
	return "context default"
}

// header is the first line of every result: engine and context.
func (h aiContainersHost) header(ctx context.Context) string {
	kind := h.e.Kind
	if kind == "" {
		kind = filepath.Base(h.e.Binary)
	}
	return "Engine: " + kind + " · " + h.contextLabel(ctx)
}

func (h aiContainersHost) bin() string {
	if h.e.Kind != "" {
		return h.e.Kind
	}
	return filepath.Base(h.e.Binary)
}

// aiContainersRedact masks secrets for the model.
func aiContainersRedact(s string) string {
	out, _ := aiServicesRedact(s)
	return out
}

func aiOneLineClip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// ── Containers: find one ─────────────────────────────────────────────────

func aiContainerName(c containers.Container) string {
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	if len(c.ID) > 12 {
		return c.ID[:12]
	}
	return c.ID
}

func aiShortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func isHex(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return s != ""
}

// aiFindContainer resolves an id, unique id prefix (4+ hex) or name.
func aiFindContainer(list []containers.Container, target string) (containers.Container, error) {
	for _, c := range list {
		if c.ID == target {
			return c, nil
		}
		for _, n := range c.Names {
			if strings.TrimPrefix(n, "/") == target {
				return c, nil
			}
		}
	}
	if len(target) >= 4 && isHex(target) {
		var hits []containers.Container
		for _, c := range list {
			if strings.HasPrefix(c.ID, target) {
				hits = append(hits, c)
			}
		}
		if len(hits) == 1 {
			return hits[0], nil
		}
		if len(hits) > 1 {
			return containers.Container{}, fmt.Errorf("id prefix %q matches %d containers; give more of the id", target, len(hits))
		}
	}
	var names []string
	for _, c := range list {
		names = append(names, aiContainerName(c))
	}
	sort.Strings(names)
	if len(names) > 20 {
		names = append(names[:20], "…")
	}
	if len(names) == 0 {
		return containers.Container{}, fmt.Errorf("no container %q: the host has no containers", target)
	}
	return containers.Container{}, fmt.Errorf("no container %q on this host; containers: %s (see containers_list)", target, strings.Join(names, ", "))
}

// aiResolveContainer lists the host fresh and finds target.
func aiResolveContainer(ctx context.Context, call aiIDECaller, target string) (aiContainersHost, containers.Container, error) {
	if err := containers.ValidName(target); err != nil {
		return aiContainersHost{}, containers.Container{}, err
	}
	h, err := aiContainersGetHost(ctx, call)
	if err != nil {
		return h, containers.Container{}, err
	}
	list, err := h.e.ListContainers(ctx)
	if err != nil {
		return h, containers.Container{}, errors.New(h.e.Kind + ": " + aiContainersRedact(err.Error()))
	}
	c, err := aiFindContainer(list, target)
	return h, c, err
}

// aiControlNoop is why an action would change nothing ("" = it would).
func aiControlNoop(action, state string) string {
	s := strings.ToLower(state)
	switch action {
	case "start":
		if s == "running" || s == "restarting" {
			return "is already running"
		}
		if s == "paused" {
			return "is paused (use unpause)"
		}
	case "stop":
		if s == "exited" || s == "created" || s == "dead" || s == "stopped" {
			return "is already stopped"
		}
	case "pause":
		if s == "paused" {
			return "is already paused"
		}
		if s != "running" {
			return "is not running (" + s + ")"
		}
	case "unpause":
		if s != "paused" {
			return "is not paused (" + s + ")"
		}
	case "restart":
		if s == "paused" {
			return "is paused (use unpause)"
		}
	}
	return ""
}

func aiValidAction(a string) bool {
	for _, x := range aiContainersActions {
		if x == a {
			return true
		}
	}
	return false
}

// aiControlValidate checks a containers_control call against a fresh list.
func aiControlValidate(ctx context.Context, call aiIDECaller, a map[string]any) (aiContainersHost, containers.Container, error) {
	action := strings.TrimSpace(ai.ArgString(a, "action"))
	if !aiValidAction(action) {
		return aiContainersHost{}, containers.Container{}, fmt.Errorf("action must be one of %s (remove and prune are the user's)", strings.Join(aiContainersActions, ", "))
	}
	h, c, err := aiResolveContainer(ctx, call, strings.TrimSpace(ai.ArgString(a, "target")))
	if err != nil {
		return h, c, err
	}
	name := aiContainerName(c)
	if h.busy[c.ID] {
		return h, c, fmt.Errorf("%s is busy with another action; try again in a moment", name)
	}
	if why := aiControlNoop(action, c.State); why != "" {
		return h, c, fmt.Errorf("%s %s — no change, nothing to %s", name, why, action)
	}
	return h, c, nil
}

// ── Reports ──────────────────────────────────────────────────────────────

func aiContainerLine(c containers.Container) string {
	parts := []string{"id " + aiShortID(c.ID)}
	if c.Image != "" {
		parts = append(parts, "image "+c.Image)
	}
	state := c.State
	if state == "" {
		state = "unknown"
	}
	parts = append(parts, state)
	if c.Status != "" {
		parts = append(parts, c.Status)
	}
	if p := strings.TrimSpace(c.PortsText); p != "" {
		parts = append(parts, "ports "+p)
	} else if len(c.Ports) > 0 {
		var ps []string
		for _, p := range c.Ports {
			if p.PublicPort > 0 {
				ps = append(ps, fmt.Sprintf("%d->%d/%s", p.PublicPort, p.PrivatePort, p.Type))
			}
		}
		if len(ps) > 0 {
			parts = append(parts, "ports "+strings.Join(ps, ", "))
		}
	}
	if svc := c.Labels["com.docker.compose.service"]; svc != "" {
		parts = append(parts, "service "+svc)
	}
	if cmd := strings.Trim(strings.TrimSpace(c.Command), `"`); cmd != "" {
		parts = append(parts, fmt.Sprintf("command %q", aiOneLineClip(cmd, aiContainersCommandMax)))
	}
	return "- " + aiContainerName(c) + " · " + strings.Join(parts, " · ")
}

// aiContainersListReport is the containers_list text.
func aiContainersListReport(head, project string, list []containers.Container) string {
	var b strings.Builder
	b.WriteString(head + "\n")
	running := 0
	for _, c := range list {
		if strings.EqualFold(c.State, "running") {
			running++
		}
	}
	fmt.Fprintf(&b, "Containers: %d (%d running)\n", len(list), running)
	n := 0
	for _, g := range containers.GroupByProject(list, project) {
		if g.IsOther || g.Name == "other" {
			b.WriteString("other (no compose project):\n")
		} else {
			fmt.Fprintf(&b, "project %s", g.Name)
			if g.Name == project && project != "" {
				b.WriteString(" (this workspace)")
			}
			if g.WorkingDir != "" {
				fmt.Fprintf(&b, " (dir %s)", g.WorkingDir)
			}
			b.WriteString(":\n")
		}
		for _, c := range g.Containers {
			n++
			if n > aiContainersListLimit {
				continue
			}
			b.WriteString(aiContainerLine(c) + "\n")
		}
	}
	if n > aiContainersListLimit {
		fmt.Fprintf(&b, "… %d more containers not shown\n", n-aiContainersListLimit)
	}
	var ports []string
	for _, c := range list {
		pl := c.Ports
		if len(pl) == 0 && c.PortsText != "" {
			pl = containers.ParsePortsText(c.PortsText)
		}
		seen := map[int]bool{}
		for _, p := range pl {
			if p.PublicPort <= 0 || seen[p.PublicPort] {
				continue
			}
			seen[p.PublicPort] = true
			proto := p.Type
			if proto == "" {
				proto = "tcp"
			}
			ports = append(ports, fmt.Sprintf("port %d → %s (%d/%s, %s)", p.PublicPort, aiContainerName(c), p.PrivatePort, proto, strings.ToLower(c.State)))
		}
	}
	if len(ports) > 0 {
		sort.Strings(ports)
		b.WriteString("Published host ports:\n- " + strings.Join(ports, "\n- ") + "\n")
	} else {
		b.WriteString("No published host ports.\n")
	}
	b.WriteString("More: containers_inspect(target), containers_logs(target), containers_stats.")
	return aiContainersRedact(b.String())
}

// aiCap cuts text to maxKB, keeping the start (inspect) or the end (logs).
func aiCap(text string, maxKB int, keepEnd bool) string {
	limit := maxKB * 1024
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if keepEnd {
		cut := len(text) - limit
		if i := strings.IndexByte(text[cut:], '\n'); i >= 0 {
			cut += i + 1
		}
		return fmt.Sprintf("… [truncated, %d more bytes]\n%s", cut, text[cut:])
	}
	cut := limit
	if i := strings.LastIndexByte(text[:cut], '\n'); i > 0 {
		cut = i
	}
	return fmt.Sprintf("%s\n… [truncated, %d more bytes; containers_max_inspect_kb is %d]", text[:cut], len(text)-cut, maxKB)
}

// ── Read tools ───────────────────────────────────────────────────────────

func aiInspectKinds() []string { return []string{"container", "image", "volume", "network"} }

func aiValidInspectTarget(kind, target string) error {
	if target == "" {
		return errors.New("give target: a container, image, volume or network")
	}
	if strings.HasPrefix(target, "-") {
		return errors.New("target cannot start with \"-\"")
	}
	switch kind {
	case "", "container", "volume", "network":
		return containers.ValidName(target)
	case "image":
		if strings.HasPrefix(target, "sha256:") && isHex(strings.TrimPrefix(target, "sha256:")) {
			return nil
		}
		if isHex(target) && len(target) >= 4 {
			return nil
		}
		return containers.ValidImageRef(target)
	}
	return fmt.Errorf("kind must be one of %s", strings.Join(aiInspectKinds(), ", "))
}

func aiContainersInspect(ctx context.Context, call aiIDECaller, a map[string]any) (string, error) {
	kind := strings.TrimSpace(ai.ArgString(a, "kind"))
	target := strings.TrimSpace(ai.ArgString(a, "target"))
	if err := aiValidInspectTarget(kind, target); err != nil {
		return "", err
	}
	var h aiContainersHost
	var err error
	id, label := target, target
	if kind == "" || kind == "container" {
		var c containers.Container
		h, c, err = aiResolveContainer(ctx, call, target)
		if err != nil {
			return "", err
		}
		kind, id, label = "container", c.ID, aiContainerName(c)
	} else if h, err = aiContainersGetHost(ctx, call); err != nil {
		return "", err
	}
	v, err := h.e.InspectType(ctx, kind, id)
	if err != nil {
		return "", errors.New(aiContainersRedact(err.Error()))
	}
	masked, n := containers.MaskInspect(v)
	data, err := json.MarshalIndent(masked, "", "  ")
	if err != nil {
		return "", err
	}
	body := aiContainersRedact(string(data))
	head := fmt.Sprintf("%s\nInspect %s %s (env values, auth fields and secret-looking labels masked: %d values)", h.header(ctx), kind, label, n)
	return head + "\n" + aiCap(body, loadServicesSettings().MaxInspectKB, false), nil
}

func aiContainersLogs(ctx context.Context, call aiIDECaller, a map[string]any) (string, error) {
	h, c, err := aiResolveContainer(ctx, call, strings.TrimSpace(ai.ArgString(a, "target")))
	if err != nil {
		return "", err
	}
	lines := ai.ArgInt(a, "lines")
	if lines <= 0 {
		lines = 100
	}
	lines = min(lines, aiContainersMaxLines)
	since := strings.TrimSpace(ai.ArgString(a, "since"))
	text, err := h.e.Logs(ctx, c.ID, lines, since)
	if err != nil {
		return "", errors.New(aiContainersRedact(err.Error()))
	}
	text = strings.TrimRight(text, "\n")
	text, n := aiServicesRedact(text)
	count := 0
	if text != "" {
		count = strings.Count(text, "\n") + 1
	}
	head := fmt.Sprintf("%s\nLogs of %s (%s): [%d lines, %d redacted values]", h.header(ctx), aiContainerName(c), aiShortID(c.ID), count, n)
	if text == "" {
		return head + "\n(no log output)", nil
	}
	return head + "\n" + aiCap(text, loadServicesSettings().MaxLogKB, true), nil
}

func aiContainersStats(ctx context.Context, call aiIDECaller, a map[string]any) (string, error) {
	target := strings.TrimSpace(ai.ArgString(a, "target"))
	var h aiContainersHost
	var err error
	var ids []string
	if target != "" {
		var c containers.Container
		h, c, err = aiResolveContainer(ctx, call, target)
		if err != nil {
			return "", err
		}
		ids = []string{c.ID}
	} else if h, err = aiContainersGetHost(ctx, call); err != nil {
		return "", err
	}
	stats, err := h.e.Stats(ctx, ids...)
	if err != nil {
		return "", errors.New(aiContainersRedact(err.Error()))
	}
	var b strings.Builder
	b.WriteString(h.header(ctx) + "\n")
	if len(stats) == 0 {
		b.WriteString("No running containers.")
		return b.String(), nil
	}
	for _, s := range stats {
		fmt.Fprintf(&b, "- %s · cpu %s · mem %s (%s) · net %s · block %s · pids %s\n", s.Container, s.CPUPerc, s.MemUsage, s.MemPerc, s.NetIO, s.BlockIO, s.PIDs)
	}
	return aiContainersRedact(strings.TrimRight(b.String(), "\n")), nil
}

func aiImagesList(ctx context.Context, call aiIDECaller) (string, error) {
	h, err := aiContainersGetHost(ctx, call)
	if err != nil {
		return "", err
	}
	list, err := h.e.ListImages(ctx)
	if err != nil {
		return "", errors.New(aiContainersRedact(err.Error()))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nImages: %d\n", h.header(ctx), len(list))
	for i, im := range list {
		if i >= aiContainersListLimit {
			fmt.Fprintf(&b, "… %d more\n", len(list)-i)
			break
		}
		tags := strings.Join(im.RepoTags, ", ")
		if tags == "" || tags == "<none>:<none>" {
			tags = "<none> (dangling)"
		}
		fmt.Fprintf(&b, "- %s · id %s · size %s", tags, aiShortID(im.ID), im.Size)
		if im.CreatedAt != "" {
			fmt.Fprintf(&b, " · created %s", im.CreatedAt)
		}
		b.WriteString("\n")
	}
	return aiContainersRedact(strings.TrimRight(b.String(), "\n")), nil
}

func aiVolumesList(ctx context.Context, call aiIDECaller) (string, error) {
	h, err := aiContainersGetHost(ctx, call)
	if err != nil {
		return "", err
	}
	list, err := h.e.ListVolumes(ctx)
	if err != nil {
		return "", errors.New(aiContainersRedact(err.Error()))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nVolumes: %d\n", h.header(ctx), len(list))
	for i, v := range list {
		if i >= aiContainersListLimit {
			fmt.Fprintf(&b, "… %d more\n", len(list)-i)
			break
		}
		fmt.Fprintf(&b, "- %s · driver %s", v.Name, v.Driver)
		if v.Scope != "" {
			fmt.Fprintf(&b, " · scope %s", v.Scope)
		}
		if p := v.Labels["com.docker.compose.project"]; p != "" {
			fmt.Fprintf(&b, " · project %s", p)
		}
		b.WriteString("\n")
	}
	return aiContainersRedact(strings.TrimRight(b.String(), "\n")), nil
}

func aiNetworksList(ctx context.Context, call aiIDECaller) (string, error) {
	h, err := aiContainersGetHost(ctx, call)
	if err != nil {
		return "", err
	}
	list, err := h.e.ListNetworks(ctx)
	if err != nil {
		return "", errors.New(aiContainersRedact(err.Error()))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nNetworks: %d\n", h.header(ctx), len(list))
	for i, n := range list {
		if i >= aiContainersListLimit {
			fmt.Fprintf(&b, "… %d more\n", len(list)-i)
			break
		}
		fmt.Fprintf(&b, "- %s · id %s · driver %s · scope %s", n.Name, aiShortID(n.ID), n.Driver, n.Scope)
		if p := n.Labels["com.docker.compose.project"]; p != "" {
			fmt.Fprintf(&b, " · project %s", p)
		}
		b.WriteString("\n")
	}
	return aiContainersRedact(strings.TrimRight(b.String(), "\n")), nil
}

func aiContextsList(ctx context.Context, call aiIDECaller) (string, error) {
	h, err := aiContainersGetHost(ctx, call)
	if err != nil {
		return "", err
	}
	list, err := h.e.ListContexts(ctx)
	if err != nil {
		return "", errors.New(aiContainersRedact(err.Error()))
	}
	var b strings.Builder
	if h.e.Kind == "podman" {
		fmt.Fprintf(&b, "%s\nPodman connections: %d (only the user switches; the tools follow the session's connection)\n", h.header(ctx), len(list))
	} else {
		fmt.Fprintf(&b, "%s\nContexts: %d (only the user switches; the tools follow the session's context)\n", h.header(ctx), len(list))
	}
	for _, c := range list {
		mark := " "
		if (h.session == "" && c.Current) || h.session == c.Name {
			mark = "*"
		}
		fmt.Fprintf(&b, "%s %s", mark, c.Name)
		if c.Current {
			b.WriteString(" (engine default)")
		}
		if h.session == c.Name {
			b.WriteString(" (this session)")
		}
		if ep := c.DockerEndpoint; ep != "" {
			fmt.Fprintf(&b, " · %s", ep)
		} else if c.Host != "" {
			fmt.Fprintf(&b, " · %s", c.Host)
		}
		if c.Error != "" {
			fmt.Fprintf(&b, " · error: %s", aiOneLineClip(c.Error, 120))
		}
		b.WriteString("\n")
	}
	return aiContainersRedact(strings.TrimRight(b.String(), "\n")), nil
}

func aiRegistriesList(ctx context.Context, call aiIDECaller) (string, error) {
	h, err := aiContainersGetHost(ctx, call)
	if err != nil {
		return "", err
	}
	path := registryConfigPath(h.e.Kind)
	cfg, err := containers.ReadRegistryConfig(path)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("cannot read %s", filepath.Base(path))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nRegistries: %d (hosts and users only; signing in and out is the user's)\n", h.header(ctx), len(cfg.Registries))
	for _, r := range cfg.Registries {
		state := "not signed in"
		if r.SignedIn {
			state = "signed in"
		}
		fmt.Fprintf(&b, "- %s · %s", r.Host, state)
		if u := h.users[r.Host]; u != "" {
			fmt.Fprintf(&b, " as %s", u)
		}
		if r.CredHelper != "" {
			fmt.Fprintf(&b, " · helper %s", r.CredHelper)
		} else if r.CredsStore != "" {
			fmt.Fprintf(&b, " · store %s", r.CredsStore)
		}
		b.WriteString("\n")
	}
	return aiContainersRedact(strings.TrimRight(b.String(), "\n")), nil
}

// ── Write tools ──────────────────────────────────────────────────────────

func aiTitle(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

// aiContainersDescribeCtx bounds the listing a card text does.
func aiContainersDescribeCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 15*time.Second)
}

func aiControlDescribe(call aiIDECaller, a map[string]any) string {
	action := strings.TrimSpace(ai.ArgString(a, "action"))
	target := strings.TrimSpace(ai.ArgString(a, "target"))
	verb := action
	if !aiValidAction(action) {
		verb = "control"
	}
	if call == nil {
		return aiTitle(verb) + " container " + target
	}
	ctx, cancel := aiContainersDescribeCtx()
	defer cancel()
	h, c, err := aiControlValidate(ctx, call, a)
	if err != nil {
		return aiTitle(verb) + " container " + target + " (refused: " + aiOneLineClip(err.Error(), 120) + ")"
	}
	return aiControlCardText(action, c, h.bin(), h.contextLabel(ctx))
}

// aiControlCardText is the text of the control card: the name the list
// shows, the command with the short id, and the context.
func aiControlCardText(action string, c containers.Container, bin, contextLabel string) string {
	return fmt.Sprintf("%s container %s (%s %s %s) · %s", aiTitle(action), containerShownName(c), bin, action, aiShortID(c.ID), contextLabel)
}

func aiControlRun(ctx context.Context, call aiIDECaller, a map[string]any) (string, error) {
	h, c, err := aiControlValidate(ctx, call, a)
	if err != nil {
		return "", err
	}
	action := strings.TrimSpace(ai.ArgString(a, "action"))
	run := map[string]func(context.Context, string) error{"start": h.e.Start, "stop": h.e.Stop, "restart": h.e.Restart,
		"pause": h.e.Pause, "unpause": h.e.Unpause}[action]
	key := containerBusyPrefix + c.ID
	state := services.Restarting
	switch action {
	case "start":
		state = services.Starting
	case "stop":
		state = services.Stopping
	}
	_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) {
		m.services.busy[key] = state
		m.services.touch()
		return "", nil, nil
	})
	actx, cancel := context.WithTimeout(ctx, aiContainersCallWait)
	runErr := run(actx, c.ID)
	cancel()
	_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) {
		delete(m.services.busy, key)
		m.services.touch()
		return "", m.containersRefresh("containers"), nil
	})
	name := aiContainerName(c)
	if runErr != nil {
		return "", fmt.Errorf("%s %s failed: %s", action, name, aiContainersRedact(runErr.Error()))
	}
	now := ""
	if list, err := h.e.ListContainers(ctx); err == nil {
		if c2, err := aiFindContainer(list, c.ID); err == nil {
			now = " Now: " + strings.TrimPrefix(aiContainerLine(c2), "- ")
		}
	}
	return aiContainersRedact(fmt.Sprintf("ran `%s %s %s` (%s) · %s.%s", h.bin(), action, aiShortID(c.ID), name, h.contextLabel(ctx), now)), nil
}

func aiPullDescribe(call aiIDECaller, a map[string]any) string {
	ref := strings.TrimSpace(ai.ArgString(a, "ref"))
	if err := containers.ValidImageRef(ref); err != nil {
		return "Pull image " + aiOneLineClip(ref, 60) + " (refused: " + aiOneLineClip(err.Error(), 120) + ")"
	}
	if call == nil {
		return "Pull image " + ref
	}
	ctx, cancel := aiContainersDescribeCtx()
	defer cancel()
	h, err := aiContainersGetHost(ctx, call)
	if err != nil {
		return "Pull image " + ref + " (refused: " + aiOneLineClip(err.Error(), 120) + ")"
	}
	return fmt.Sprintf("Pull image %s (%s pull %s) in a terminal tab · %s", ref, h.bin(), ref, h.contextLabel(ctx))
}

func aiPullRun(ctx context.Context, call aiIDECaller, a map[string]any) (string, error) {
	ref := strings.TrimSpace(ai.ArgString(a, "ref"))
	if err := containers.ValidImageRef(ref); err != nil {
		return "", err
	}
	h, err := aiContainersGetHost(ctx, call)
	if err != nil {
		return "", err
	}
	key := containerTabPrefix + "pull:" + ref
	out, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		if _, running := m.svcTabRunning(key); running {
			m.servicesFocusTab(key)
			return "already pulling " + ref + " in its tab (no change)", nil, nil
		}
		cmd := h.e.PullCommand(ref)
		if err := m.servicesRunCommand(cmd, key); err != nil {
			return "", nil, fmt.Errorf("cannot open the pull tab: %v", err)
		}
		return "", nil, nil
	})
	if err != nil || out != "" {
		return out, err
	}
	head := fmt.Sprintf("started `%s pull %s` in the terminal tab \"pull: %s\" · %s", h.bin(), ref, ref, h.contextLabel(ctx))
	wait := aiWaitArg(a)
	if wait <= 0 {
		return head + "; call images_list later (or images_pull with wait_ms) to see it", nil
	}
	timedOut, err := aiPollUntil(ctx, wait, aiServicesPoll, func() (bool, error) {
		out, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
			if _, running := m.svcTabRunning(key); running {
				return "", nil, nil
			}
			return "done", nil, nil
		})
		return out == "done", err
	})
	if err != nil {
		return "", err
	}
	if timedOut {
		return head + fmt.Sprintf("; still pulling after %s", wait), nil
	}
	return head + "; the pull finished (see the tab for the result)", nil
}

// aiComposeTarget finds a compose project on the host and its folder.
type aiComposeTarget struct {
	h        aiContainersHost
	project  string
	dir      string
	files    []string
	services []string
}

func aiComposeResolve(ctx context.Context, call aiIDECaller, a map[string]any) (aiComposeTarget, error) {
	var t aiComposeTarget
	t.project = strings.TrimSpace(ai.ArgString(a, "project"))
	if err := containers.ValidName(t.project); err != nil {
		return t, fmt.Errorf("project: %v", err)
	}
	switch v := a["services"].(type) {
	case nil:
	case []any:
		for _, s := range v {
			name, _ := s.(string)
			if err := containers.ValidName(name); err != nil {
				return t, fmt.Errorf("services: %v", err)
			}
			t.services = append(t.services, name)
		}
	case []string:
		for _, name := range v {
			if err := containers.ValidName(name); err != nil {
				return t, fmt.Errorf("services: %v", err)
			}
			t.services = append(t.services, name)
		}
	default:
		return t, errors.New("services must be a list of service names")
	}
	h, err := aiContainersGetHost(ctx, call)
	if err != nil {
		return t, err
	}
	t.h = h
	list, err := h.e.ListContainers(ctx)
	if err != nil {
		return t, errors.New(aiContainersRedact(err.Error()))
	}
	var known []string
	found := false
	for _, g := range containers.GroupByProject(list, h.project) {
		if g.IsOther || g.Name == "other" {
			continue
		}
		known = append(known, g.Name)
		if g.Name != t.project {
			continue
		}
		found = true
		t.dir = g.WorkingDir
		for _, c := range g.Containers {
			if len(c.ConfigFiles) > 0 {
				t.files = append([]string(nil), c.ConfigFiles...)
				break
			}
		}
	}
	if t.project == h.project && h.project != "" {
		found = true
		if t.dir == "" {
			t.dir = h.root
		}
	}
	if !found {
		if len(known) == 0 {
			known = []string{"none"}
		}
		return t, fmt.Errorf("no compose project %q on this host; projects: %s (see containers_list)", t.project, strings.Join(known, ", "))
	}
	if !projectDirExists(t.dir) {
		return t, fmt.Errorf("the folder of project %s (%s) is missing; compose_up needs it", t.project, t.dir)
	}
	return t, nil
}

func (t aiComposeTarget) argv() []string {
	return t.h.e.ComposeWithFiles(t.project, t.dir, t.files, append([]string{"up", "-d"}, t.services...)...).Argv
}

func aiComposeDescribe(call aiIDECaller, a map[string]any) string {
	project := strings.TrimSpace(ai.ArgString(a, "project"))
	if call == nil {
		return "Start compose project " + project
	}
	ctx, cancel := aiContainersDescribeCtx()
	defer cancel()
	t, err := aiComposeResolve(ctx, call, a)
	if err != nil {
		return "Start compose project " + aiOneLineClip(project, 60) + " (refused: " + aiOneLineClip(err.Error(), 120) + ")"
	}
	argv := t.argv()
	argv[0] = t.h.bin()
	return fmt.Sprintf("Start compose project %s (%s) in a terminal tab · %s", t.project, svcShellLine(argv), t.h.contextLabel(ctx))
}

func aiComposeRun(ctx context.Context, call aiIDECaller, a map[string]any) (string, error) {
	t, err := aiComposeResolve(ctx, call, a)
	if err != nil {
		return "", err
	}
	cmd := t.h.e.ComposeWithFiles(t.project, t.dir, t.files, append([]string{"up", "-d"}, t.services...)...)
	cmd.Label = "compose: " + t.project + " up"
	key := containerTabPrefix + "compose:" + t.project
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		if err := m.servicesRunCommand(cmd, key); err != nil {
			return "", nil, fmt.Errorf("cannot open the compose tab: %v", err)
		}
		return "", nil, nil
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf("started `%s compose -p %s up -d` in the terminal tab %q · %s; call containers_list to see the state",
		t.h.bin(), t.project, cmd.Label, t.h.contextLabel(ctx)), nil
}

// ── Tool definitions ─────────────────────────────────────────────────────

func (m *Model) aiContainersTools(call aiIDECaller) []ai.Tool {
	gate := aiContainersGate(m.services)
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	num := func(d string) map[string]any { return map[string]any{"type": "integer", "description": d} }
	target := str("container name or id (see containers_list)")
	validTarget := func(a map[string]any) error {
		return containers.ValidName(strings.TrimSpace(ai.ArgString(a, "target")))
	}
	read := func(f func(context.Context, aiIDECaller) (string, error)) func(context.Context, map[string]any) (string, error) {
		return func(ctx context.Context, _ map[string]any) (string, error) { return f(ctx, call) }
	}
	return []ai.Tool{
		{
			Name: "containers_list",
			Description: "Every container on this host (all projects, running and stopped), grouped by compose project: " +
				"id, name, image, state, status, published ports, compose service, command; plus which host port each container " +
				"publishes. Names the engine and the context. Use it for \"why is port N busy?\" before suggesting docker commands.",
			Params:   map[string]any{},
			Describe: func(map[string]any) string { return "List host containers" },
			Run: func(ctx context.Context, _ map[string]any) (string, error) {
				h, err := aiContainersGetHost(ctx, call)
				if err != nil {
					return "", err
				}
				list, err := h.e.ListContainers(ctx)
				if err != nil {
					return "", errors.New(aiContainersRedact(err.Error()))
				}
				return aiContainersListReport(h.header(ctx), h.project, list), nil
			},
		},
		{
			Name: "containers_inspect",
			Description: "The engine's inspect JSON of one container (default), image, volume or network, with every env value, " +
				"auth field and secret-looking label masked (env names stay). Capped at containers_max_inspect_kb.",
			Params: map[string]any{
				"target": str("container name or id; or an image reference, volume or network name with kind"),
				"kind":   map[string]any{"type": "string", "enum": aiInspectKinds(), "description": "what target is (default container)"},
			},
			Required: []string{"target"},
			Validate: func(a map[string]any) error {
				return aiValidInspectTarget(strings.TrimSpace(ai.ArgString(a, "kind")), strings.TrimSpace(ai.ArgString(a, "target")))
			},
			Describe: func(a map[string]any) string { return "Inspect " + ai.ArgString(a, "target") },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				return aiContainersInspect(ctx, call, a)
			},
		},
		{
			Name: "containers_logs",
			Description: "The last log lines of one host container (stdout and stderr, with timestamps), secrets redacted. " +
				"lines default 100, max 1000; since like \"10m\". Capped at services_max_log_kb (newest lines kept).",
			Params: map[string]any{
				"target": target,
				"lines":  num("how many last lines (default 100, max 1000)"),
				"since":  str("only newer than this, e.g. 10m or 1h"),
			},
			Required: []string{"target"},
			Validate: func(a map[string]any) error {
				if s := ai.ArgString(a, "since"); s != "" {
					if _, err := time.ParseDuration(s); err != nil {
						return fmt.Errorf("since %q is not a duration like 10m", s)
					}
				}
				return validTarget(a)
			},
			Describe: func(a map[string]any) string { return "Container logs " + ai.ArgString(a, "target") },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				return aiContainersLogs(ctx, call, a)
			},
		},
		{
			Name:        "containers_stats",
			Description: "One sample of CPU, memory, network and block I/O for one container (target) or every running one.",
			Params:      map[string]any{"target": str("container name or id; default every running container")},
			Validate: func(a map[string]any) error {
				if strings.TrimSpace(ai.ArgString(a, "target")) == "" {
					return nil
				}
				return validTarget(a)
			},
			Describe: func(a map[string]any) string { return "Container stats " + ai.ArgString(a, "target") },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				return aiContainersStats(ctx, call, a)
			},
		},
		{
			Name:        "images_list",
			Description: "Every image on this host: repository:tag, id, size, created; dangling images are marked.",
			Params:      map[string]any{},
			Describe:    func(map[string]any) string { return "List images" },
			Run:         read(aiImagesList),
		},
		{
			Name:        "volumes_list",
			Description: "Every volume on this host: name, driver, scope, compose project.",
			Params:      map[string]any{},
			Describe:    func(map[string]any) string { return "List volumes" },
			Run:         read(aiVolumesList),
		},
		{
			Name:        "networks_list",
			Description: "Every network on this host: name, id, driver, scope, compose project.",
			Params:      map[string]any{},
			Describe:    func(map[string]any) string { return "List networks" },
			Run:         read(aiNetworksList),
		},
		{
			Name:        "contexts_list",
			Description: "The engine's contexts (docker) or connections (podman): which one is the default and which one this session uses. Only the user switches them.",
			Params:      map[string]any{},
			Describe:    func(map[string]any) string { return "List contexts" },
			Run:         read(aiContextsList),
		},
		{
			Name:        "registries_list",
			Description: "Registry hosts from the engine's login file: signed in or not, the user (when known) and the credential helper. Never a password or token.",
			Params:      map[string]any{},
			Describe:    func(map[string]any) string { return "List registries" },
			Run:         read(aiRegistriesList),
		},
		{
			Name: "containers_control",
			Description: "Start, stop, restart, pause or unpause one host container (any project). Asks the user first. " +
				"Refused when the container is busy or already in that state. Removing and pruning are the user's.",
			Params: map[string]any{
				"action": map[string]any{"type": "string", "enum": aiContainersActions, "description": "what to do"},
				"target": target,
			},
			Required: []string{"action", "target"},
			Mutating: true, Class: ai.ClassContainers, Gate: gate,
			Validate: func(a map[string]any) error {
				ctx, cancel := context.WithTimeout(context.Background(), aiContainersCallWait)
				defer cancel()
				_, _, err := aiControlValidate(ctx, call, a)
				return err
			},
			Describe: func(a map[string]any) string { return aiControlDescribe(call, a) },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				return aiControlRun(ctx, call, a)
			},
		},
		{
			Name: "images_pull",
			Description: "Pull an image (ref like nginx:1.27 or ghcr.io/acme/api:1.4) in a visible terminal tab \"pull: <ref>\". " +
				"Asks the user first. wait_ms waits (max 120000) for the pull to end.",
			Params: map[string]any{
				"ref":     str("image reference: repo[:tag][@sha256:digest]"),
				"wait_ms": num("wait up to this long (ms, max 120000) for the pull to finish"),
			},
			Required: []string{"ref"},
			Mutating: true, Class: ai.ClassContainers, Gate: gate,
			Validate: func(a map[string]any) error {
				if err := containers.ValidImageRef(strings.TrimSpace(ai.ArgString(a, "ref"))); err != nil {
					return err
				}
				ctx, cancel := context.WithTimeout(context.Background(), aiContainersCallWait)
				defer cancel()
				_, err := aiContainersGetHost(ctx, call)
				return err
			},
			Describe: func(a map[string]any) string { return aiPullDescribe(call, a) },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				return aiPullRun(ctx, call, a)
			},
		},
		{
			Name: "compose_up",
			Description: "Run `docker compose up -d` for a compose project that has containers on this host (see containers_list), " +
				"in its own folder, in a visible terminal tab. services limits it to some services. Asks the user first. " +
				"For this workspace's services prefer services_start.",
			Params: map[string]any{
				"project":  str("compose project name from containers_list"),
				"services": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "only these services (default all)"},
			},
			Required: []string{"project"},
			Mutating: true, Class: ai.ClassContainers, Gate: gate,
			Validate: func(a map[string]any) error {
				ctx, cancel := context.WithTimeout(context.Background(), aiContainersCallWait)
				defer cancel()
				_, err := aiComposeResolve(ctx, call, a)
				return err
			},
			Describe: func(a map[string]any) string { return aiComposeDescribe(call, a) },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				return aiComposeRun(ctx, call, a)
			},
		},
	}
}

// aiContainersPromptLine is the system prompt line, only when a container
// engine is on this machine (a PATH lookup; no process).
func (m *Model) aiContainersPromptLine() string {
	if runtime.GOOS == "android" {
		return ""
	}
	if m.services != nil && m.services.containerEngine.Binary != "" {
		return "\n" + aiContainersPrompt
	}
	names := []string{"docker", "podman"}
	if pref := preferredContainerEngine(); pref != "auto" {
		names = []string{pref}
	}
	for _, n := range names {
		if _, err := exec.LookPath(n); err == nil {
			return "\n" + aiContainersPrompt
		}
	}
	return ""
}
