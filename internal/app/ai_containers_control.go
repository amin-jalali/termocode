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
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/lazyre"
	"github.com/amin-jalali/termocode/internal/services"
)

// containers_control: every action of the Containers view's container
// row, its menu and the palette's Containers: … Selected rows, on any
// host (context). It asks where the GUI asks:
//
//   - a card every time, Cancel first, no "Always allow" (the GUI's own
//     cards): stop (the page's laptop card), kill, remove, restart
//     policy, CPU and memory limits, network connect and disconnect,
//     and an export that replaces a file;
//   - the containers class (Allow · Always allow containers for this
//     session, the page's in-page confirm): start, restart, pause,
//     unpause, rename, commit and export.
//
// Every id, name and value is checked before the card (the engine's own
// rules) and goes into an argv list; a container is found in a fresh
// listing of its host; a busy row and a no-op are refused.

var aiContainersActions = []string{"start", "stop", "restart", "pause", "unpause", "kill", "remove", "rename",
	"restart_policy", "resources", "network_connect", "network_disconnect", "commit", "export"}

// aiControlCards are the actions the GUI asks on a card every time.
var aiControlCards = map[string]bool{"stop": true, "kill": true, "remove": true, "restart_policy": true, "resources": true,
	"network_connect": true, "network_disconnect": true}

var (
	aiSignalRe = lazyre.New(`^(SIG)?[A-Z][A-Z0-9]*(\+[0-9]+)?$|^[0-9]{1,2}$`)
	aiCPUsRe   = lazyre.New(`^[0-9]+(\.[0-9]+)?$`)
	aiSizeRe   = lazyre.New(`^[0-9]+[bkmgBKMG]?$`)
	aiCtrName  = lazyre.New(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
)

// aiControlReq is one checked containers_control call.
type aiControlReq struct {
	action string
	h      aiContainersHost
	c      containers.Container
	argv   []string // the engine command without the binary (the card shows it)
	pre    []string // run first (remove force: stop)
	file   string   // export: the absolute file
	exists bool     // export: the file is there (the GUI's Replace card)
	note   string   // the GUI card's sentence
}

func aiRunning(state string) bool {
	s := strings.ToLower(state)
	return s == "running" || s == "restarting" || s == "paused"
}

func aiValidRestartPolicy(p string) error {
	base, n, hasN := strings.Cut(p, ":")
	switch {
	case !hasN && (base == "no" || base == "always" || base == "unless-stopped" || base == "on-failure"):
		return nil
	case hasN && base == "on-failure" && n != "" && strings.Trim(n, "0123456789") == "":
		return nil
	}
	return fmt.Errorf("restart policy %q is not known (no, always, unless-stopped, on-failure[:N])", p)
}

// aiControlValidate checks a containers_control call against a fresh list.
func aiControlValidate(ctx context.Context, call aiIDECaller, a map[string]any) (aiControlReq, error) {
	r := aiControlReq{action: strings.TrimSpace(ai.ArgString(a, "action"))}
	if !contains(aiContainersActions, r.action) {
		return r, fmt.Errorf("action must be one of %s", strings.Join(aiContainersActions, ", "))
	}
	if err := aiValidContextArg(a); err != nil {
		return r, err
	}
	h, c, err := aiResolveContainer(ctx, call, a, strings.TrimSpace(ai.ArgString(a, "target")))
	r.h, r.c = h, c
	if err != nil {
		return r, err
	}
	name := aiContainerName(c)
	if h.busy[c.ID] {
		return r, fmt.Errorf("%s is busy with another action; try again in a moment", name)
	}
	short := aiShortID(c.ID)
	switch r.action {
	case "start", "stop", "restart", "pause", "unpause":
		if why := aiControlNoop(r.action, c.State); why != "" {
			return r, fmt.Errorf("%s %s — no change, nothing to %s", name, why, r.action)
		}
		r.argv = []string{r.action, short}
		if r.action == "stop" {
			r.note = "What runs in it ends."
		}
	case "kill":
		if s := strings.ToLower(c.State); s != "running" && s != "paused" {
			return r, fmt.Errorf("%s is not running (%s)", name, s)
		}
		sig := strings.TrimSpace(ai.ArgString(a, "signal"))
		if sig == "" {
			sig = "KILL"
		}
		if !aiSignalRe.MatchString(sig) {
			return r, fmt.Errorf("signal %q is not valid (like KILL, TERM, HUP or 9)", sig)
		}
		r.argv = []string{"kill", "--signal", sig, short}
		r.note = "This ends it at once; unsaved work in it is lost. Signal TERM stops it gently."
	case "remove":
		if aiRunning(c.State) {
			if !ai.ArgBool(a, "force") {
				return r, fmt.Errorf("%s is %s: stop it first, or pass force: true to stop and remove it", name, strings.ToLower(c.State))
			}
			r.pre = []string{"stop", short}
		}
		r.argv = []string{"rm", short}
		r.note = "This cannot be undone; its volumes stay."
	case "rename":
		n := strings.TrimSpace(ai.ArgString(a, "name"))
		if !aiCtrName.MatchString(n) {
			return r, fmt.Errorf("name %q is not a container name (letters, digits, _ . -)", n)
		}
		if n == name || n == containerShownName(c) {
			return r, fmt.Errorf("%s already has that name (no change)", name)
		}
		if list, err := h.e.ListContainers(ctx); err == nil {
			if other, err := aiFindContainer(list, n); err == nil && other.ID != c.ID {
				return r, fmt.Errorf("another container is named %s", n)
			}
		}
		r.argv = []string{"rename", short, n}
	case "restart_policy":
		p := strings.TrimSpace(ai.ArgString(a, "policy"))
		if err := aiValidRestartPolicy(p); err != nil {
			return r, err
		}
		r.argv = []string{"update", "--restart", p, short}
		r.note = "Its restart policy becomes " + p + "."
	case "resources":
		r.argv = []string{"update"}
		for _, f := range []struct{ arg, flag string }{{"cpus", "--cpus"}, {"memory", "--memory"}, {"memory_swap", "--memory-swap"}} {
			v := strings.TrimSpace(ai.ArgString(a, f.arg))
			if v == "" {
				continue
			}
			if (f.arg == "cpus" && !aiCPUsRe.MatchString(v)) || (f.arg != "cpus" && !aiSizeRe.MatchString(v)) {
				return r, fmt.Errorf("%s %q is not valid (cpus like 1.5, sizes like 512m or 1g)", f.arg, v)
			}
			r.argv = append(r.argv, f.flag, v)
		}
		if len(r.argv) == 1 {
			return r, errors.New("give cpus, memory or memory_swap (no limit to change)")
		}
		r.argv = append(r.argv, short)
		r.note = "This changes its limits now. Too little memory can stop it."
	case "network_connect", "network_disconnect":
		net := strings.TrimSpace(ai.ArgString(a, "network"))
		if err := containers.ValidName(net); err != nil {
			return r, fmt.Errorf("network: %v", err)
		}
		on := contains(c.Networks, net)
		verb := strings.TrimPrefix(r.action, "network_")
		if verb == "connect" {
			if on {
				return r, fmt.Errorf("%s is already on %s (no change)", name, net)
			}
			list, err := h.e.ListNetworks(ctx)
			if err != nil {
				return r, errors.New(aiContainersRedact(err.Error()))
			}
			known := false
			var names []string
			for _, n := range list {
				known = known || n.Name == net
				names = append(names, n.Name)
			}
			if !known {
				return r, fmt.Errorf("no network %q on this host; networks: %s (see networks_list)", net, strings.Join(names, ", "))
			}
			r.note = name + " joins " + net + "."
		} else {
			if !on {
				return r, fmt.Errorf("%s is not on %s; it is on: %s", name, net, strings.Join(c.Networks, ", "))
			}
			r.note = name + " leaves " + net + "; it cannot reach that network's containers after this."
		}
		r.argv = []string{"network", verb, net, short}
	case "commit":
		ref, msg := strings.TrimSpace(ai.ArgString(a, "image")), strings.TrimSpace(ai.ArgString(a, "message"))
		if ref != "" {
			if err := containers.ValidImageRef(ref); err != nil {
				return r, fmt.Errorf("image: %v", err)
			}
		}
		if strings.ContainsAny(msg, "\n\r\x00") {
			return r, errors.New("message must be one line")
		}
		if r.argv, err = containers.CommitArgs(short, ref, msg); err != nil {
			return r, err
		}
	case "export":
		rel := strings.TrimSpace(ai.ArgString(a, "file"))
		if r.file, err = aiDBInside(aiCtrRoot(h), rel); err != nil {
			return r, fmt.Errorf("file: %v", err)
		}
		_, statErr := os.Stat(r.file)
		r.exists = statErr == nil
		if r.argv, err = containers.ExportArgs(short, r.file); err != nil {
			return r, err
		}
		if r.exists {
			r.note = rel + " exists. Exporting replaces it."
		}
	}
	return r, nil
}

// aiCtrRoot is the workspace folder files of the container tools are in.
func aiCtrRoot(h aiContainersHost) string {
	if h.root != "" {
		return h.root
	}
	return aiWorkspaceRoot()
}

// aiControlGate: the GUI's card actions ask every time; the rest asks
// with the containers class (Always allow containers skips it).
func aiControlGate(st *servicesState) func(map[string]any) (bool, string) {
	session := aiContainersGate(st)
	return func(a map[string]any) (bool, string) {
		action := strings.TrimSpace(ai.ArgString(a, "action"))
		if aiControlCards[action] || !contains(aiContainersActions, action) {
			return true, ""
		}
		if action == "export" {
			if rel := strings.TrimSpace(ai.ArgString(a, "file")); rel != "" {
				if p, err := aiDBInside(aiCtrRootOf(st), rel); err == nil {
					if _, err := os.Stat(p); err == nil {
						return true, "" // Replace file? is a card
					}
				}
			}
		}
		return session(a)
	}
}

func aiCtrRootOf(st *servicesState) string {
	if st != nil && st.root != "" {
		return st.root
	}
	return aiWorkspaceRoot()
}

func aiControlDescribe(call aiIDECaller, a map[string]any) string {
	action := strings.TrimSpace(ai.ArgString(a, "action"))
	target := strings.TrimSpace(ai.ArgString(a, "target"))
	verb := strings.ReplaceAll(action, "_", " ")
	if !contains(aiContainersActions, action) {
		verb = "control"
	}
	if call == nil {
		return aiTitle(verb) + " container " + target
	}
	ctx, cancel := aiContainersDescribeCtx()
	defer cancel()
	r, err := aiControlValidate(ctx, call, a)
	if err != nil {
		return aiTitle(verb) + " container " + target + " (refused: " + aiOneLineClip(err.Error(), 120) + ")"
	}
	line := svcShellLine(append([]string{r.h.bin()}, r.argv...))
	if len(r.pre) > 0 {
		line = svcShellLine(append([]string{r.h.bin()}, r.pre...)) + " && " + line
	}
	out := fmt.Sprintf("%s container %s (%s) · %s", aiTitle(verb), containerShownName(r.c), line, r.h.contextLabel(ctx))
	if r.note != "" {
		out += " · " + r.note
	}
	return out
}

// aiEngineRun runs one engine command (argv without the binary) on h.
func aiEngineRun(ctx context.Context, h aiContainersHost, d time.Duration, args ...string) (string, error) {
	x := h.e.X
	if x == nil {
		x = servicesExec
	}
	if env := h.e.ContextEnv(); env != "" {
		x = services.WithEnv(x, env)
	}
	rctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	stdout, stderr, code, err := x.Capture(rctx, "", append([]string{h.e.Binary}, args...)...)
	if err == nil && code != 0 {
		err = errors.New(errLine(stderr))
	}
	if err != nil {
		return stdout, errors.New(shortRemoteError(aiContainersRedact(err.Error())))
	}
	return stdout, nil
}

func aiControlRun(ctx context.Context, call aiIDECaller, a map[string]any) (string, error) {
	r, err := aiControlValidate(ctx, call, a)
	if err != nil {
		return "", err
	}
	c, h := r.c, r.h
	key := containerBusyPrefix + c.ID
	state := map[string]services.State{"start": services.Starting, "stop": services.Stopping, "kill": services.Stopping,
		"remove": services.Stopping}[r.action]
	if state == 0 {
		state = services.Restarting
	}
	_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) {
		m.services.busy[key] = state
		m.services.touch()
		return "", nil, nil
	})
	wait := aiContainersCallWait
	if r.action == "commit" || r.action == "export" {
		wait = archiveJobTimeout
	}
	argv := append([]string(nil), r.argv...) // the card shows the short id; the full id runs
	for i, v := range argv {
		if v == aiShortID(c.ID) {
			argv[i] = c.ID
		}
	}
	var runErr error
	if len(r.pre) > 0 {
		_, runErr = aiEngineRun(ctx, h, wait, r.pre[0], c.ID)
	}
	if runErr == nil {
		if r.action == "export" {
			if err := os.MkdirAll(filepath.Dir(r.file), 0o755); err != nil {
				runErr = err
			}
		}
	}
	if runErr == nil {
		_, runErr = aiEngineRun(ctx, h, wait, argv...)
	}
	_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) {
		delete(m.services.busy, key)
		m.services.touch()
		if r.action == "commit" {
			return "", tea.Batch(m.containersRefresh("containers"), m.containersRefresh("images")), nil
		}
		return "", m.containersRefresh("containers"), nil
	})
	name := aiContainerName(c)
	if runErr != nil {
		return "", fmt.Errorf("%s %s failed: %s", strings.ReplaceAll(r.action, "_", " "), name, aiContainersRedact(runErr.Error()))
	}
	now := ""
	if r.action != "remove" {
		if list, err := h.e.ListContainers(ctx); err == nil {
			if c2, err := aiFindContainer(list, c.ID); err == nil {
				now = " Now: " + strings.TrimPrefix(aiContainerLine(c2), "- ")
			}
		}
	}
	return aiContainersRedact(fmt.Sprintf("ran `%s` (%s) · %s.%s", svcShellLine(append([]string{h.bin()}, r.argv...)), name, h.contextLabel(ctx), now)), nil
}

// aiContainerChangesOrTop is containers_inspect kind changes (`diff`:
// the files a container changed against its image) or processes (`top`).
func aiContainerChangesOrTop(ctx context.Context, call aiIDECaller, a map[string]any, kind, target string) (string, error) {
	h, c, err := aiResolveContainer(ctx, call, a, target)
	if err != nil {
		return "", err
	}
	name := aiContainerName(c)
	if kind == "processes" {
		t, err := h.e.Top(ctx, c.ID)
		if err != nil {
			return "", errors.New(aiContainersRedact(err.Error()))
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s\nProcesses of %s: %d\n%s\n", h.header(ctx), name, len(t.Rows), strings.Join(t.Titles, " · "))
		for _, row := range t.Rows {
			b.WriteString("- " + strings.Join(row, " · ") + "\n")
		}
		return aiContainersRedact(aiCap(strings.TrimRight(b.String(), "\n"), loadServicesSettings().MaxInspectKB, false)), nil
	}
	args, err := containers.DiffArgs(c.ID)
	if err != nil {
		return "", err
	}
	out, err := aiEngineRun(ctx, h, aiContainersCallWait, args...)
	if err != nil {
		return "", err
	}
	text := containerDiffText(name, containers.ParseDiff(out))
	return aiContainersRedact(h.header(ctx) + "\n" + aiCap(text, loadServicesSettings().MaxInspectKB, false)), nil
}
