package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/containerhost"
	"github.com/amin-jalali/termocode/internal/containers"
)

// contexts_manage: the hosts (servers) of the Containers view, managed as
// the GUI does (the hosts tree, the context row menu, the palette's
// Containers: … Context… rows, the web's containers.add_server /
// context / context_default / host_terminal).
//
// It asks exactly where the GUI asks: add (+ Add Server…), make default
// and remove show the GUI's own card (Cancel first) every time, with no
// "Always allow"; use (this session only) asks with the containers class
// as the page's in-page confirm does; inspect and terminal (Open
// Terminal, which the GUI opens at once) run without a question. The
// refusals are the GUI's: the built-in context, the engine default and
// the session's own host are never removed.

var aiContextOps = []string{"add", "use", "default", "remove", "inspect", "terminal"}

// aiContextReq is one parsed contexts_manage call.
type aiContextReq struct {
	op   string
	h    aiContainersHost
	name string
	ctx  containers.Context // the listed host (not for add)
	spec containers.ContextSpec
	argv []string // the engine command (add, default, remove, inspect)
}

func aiContextParse(ctx context.Context, call aiIDECaller, a map[string]any) (aiContextReq, error) {
	r := aiContextReq{op: ai.ArgString(a, "op"), name: strings.TrimSpace(ai.ArgString(a, "name"))}
	if !contains(aiContextOps, r.op) {
		return r, errors.New("give op: " + strings.Join(aiContextOps, ", "))
	}
	if r.name == "" {
		return r, errors.New("give name: a context (podman: connection) name; contexts_list lists them")
	}
	if r.op == "add" {
		r.spec = containers.ContextSpec{Name: r.name, Host: strings.TrimSpace(ai.ArgString(a, "host")),
			Description: strings.TrimSpace(ai.ArgString(a, "description")),
			CA:          strings.TrimSpace(ai.ArgString(a, "tls_ca")), Cert: strings.TrimSpace(ai.ArgString(a, "tls_cert")),
			Key: strings.TrimSpace(ai.ArgString(a, "tls_key"))}
		if id := strings.TrimSpace(ai.ArgString(a, "identity")); id != "" {
			r.spec.Key = id
		}
		if r.spec.Host == "" {
			return r, errors.New("add: give host, like ssh://user@host (a port when not 22), tcp://host:2376 or unix:///path")
		}
	} else if r.name != podmanLocal {
		if err := containers.ValidName(r.name); err != nil {
			return r, err
		}
	}
	if call == nil {
		return r, nil
	}
	h, err := aiContainersGetHost(ctx, call)
	if err != nil {
		return r, err
	}
	r.h = h
	if r.op == "add" {
		if r.argv, err = h.e.CreateContextArgs(r.spec); err != nil {
			return r, err
		}
	}
	list, err := webHostContexts(ctx, h)
	if err != nil {
		return r, errors.New(aiContainersRedact(err.Error()))
	}
	var names []string
	found := false
	for _, c := range list {
		names = append(names, c.Name)
		if c.Name == r.name {
			r.ctx, found = c, true
		}
	}
	word := r.word()
	switch {
	case r.op == "add" && found:
		return r, fmt.Errorf("a %s named %s exists (contexts_list)", word, sanitizeOutputLine(r.name))
	case r.op == "add":
		return r, nil
	case !found:
		return r, fmt.Errorf("no %s %q; %ss: %s (see contexts_list)", word, sanitizeOutputLine(r.name), word, strings.Join(names, ", "))
	}
	switch r.op {
	case "default":
		if r.name == podmanLocal {
			return r, errors.New("local is not a connection; plain podman on this machine needs no default")
		}
		if r.ctx.Current {
			return r, fmt.Errorf("%s is already the default (no change)", sanitizeOutputLine(r.name))
		}
		r.argv = h.e.UseContextArgs(r.name)
	case "remove":
		switch {
		case r.name == podmanLocal || r.name == "default":
			return r, fmt.Errorf("%s cannot be removed: it is the engine's own %s", contextDisplayName(r.name), word)
		case r.ctx.Current:
			return r, fmt.Errorf("this %s is the default; make another one the default first (op default)", word)
		case h.session == r.name:
			return r, fmt.Errorf("this session uses %s; switch to another %s first (op use)", sanitizeOutputLine(r.name), word)
		}
		if r.argv, err = h.e.RemoveContextArgs(r.name); err != nil {
			return r, err
		}
	case "inspect":
		if r.argv, err = h.e.InspectContextArgs(r.name); err != nil {
			return r, err
		}
	case "use":
		if aiContextIsSession(h, r.ctx) && h.e.Reachable {
			return r, fmt.Errorf("this session already uses %s (no change)", contextDisplayName(r.name))
		}
	case "terminal":
		ep := r.ctx.DockerEndpoint
		if _, ok := hostTerminalArgv(ep); !ok && !hostIsLocal(ep) {
			if containerhost.SSHEndpoint(ep) {
				return r, errors.New("the host of " + contextDisplayName(r.name) + " is not a plain user@host")
			}
			return r, errors.New("Open Terminal needs an ssh:// host or this machine")
		}
	}
	return r, nil
}

func (r aiContextReq) word() string {
	if r.h.e.Kind == "podman" {
		return "connection"
	}
	return "context"
}

func (r aiContextReq) command() string {
	return svcShellLine(append([]string{r.h.bin()}, r.argv...))
}

func aiContextDescribe(call aiIDECaller, a map[string]any) string {
	op, name := ai.ArgString(a, "op"), sanitizeOutputLine(strings.TrimSpace(ai.ArgString(a, "name")))
	ctx, cancel := aiContainersDescribeCtx()
	defer cancel()
	r, err := aiContextParse(ctx, call, a)
	if err != nil {
		return "Manage " + name + " (" + op + "; refused: " + aiOneLineClip(err.Error(), 120) + ")"
	}
	where := serverTarget(r.ctx)
	switch op {
	case "add":
		return fmt.Sprintf("Add server %s (%s): a new %s for every terminal on this machine; it does not switch to it", name, r.command(), r.word())
	case "use":
		return fmt.Sprintf("Use %s %s (%s) in this session only: every container action goes there; your default is unchanged", r.word(), name, where)
	case "default":
		return fmt.Sprintf("Make %s the default %s (%s): this changes the default for every terminal and app on this machine, not only termocode", name, r.word(), r.command())
	case "remove":
		return fmt.Sprintf("Remove %s %s (%s): it goes for every terminal on this machine; the remote host and its containers are not touched", r.word(), name, r.command())
	case "inspect":
		return "Inspect " + r.word() + " " + name
	}
	return "Open a terminal on " + name + " (" + where + ")"
}

func aiContextRun(ctx context.Context, call aiIDECaller, a map[string]any) (string, error) {
	r, err := aiContextParse(ctx, call, a)
	if err != nil {
		return "", err
	}
	name := contextDisplayName(r.name)
	refresh := func(out string) (string, error) {
		_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) {
			if m.services != nil && m.services.visible {
				return "", m.containersRefreshContexts(), nil
			}
			return "", nil, nil
		})
		return out, nil
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	switch r.op {
	case "add":
		if err := r.h.e.CreateContext(runCtx, r.spec); err != nil {
			return "", fmt.Errorf("could not add %s: %s", name, sanitizeOutputLine(errLine(aiContainersRedact(err.Error()))))
		}
		return refresh(fmt.Sprintf("added %s (a %s for %s); pass context %s to the container tools to work there, or op use to switch this session", name, r.word(), r.spec.Host, r.name))
	case "default":
		if err := r.h.e.UseContext(runCtx, r.name); err != nil {
			return "", fmt.Errorf("could not change the default: %s", sanitizeOutputLine(errLine(aiContainersRedact(err.Error()))))
		}
		return refresh("the default " + r.word() + " is now " + name + " (for new terminals and other apps)")
	case "remove":
		if err := r.h.e.RemoveContext(runCtx, r.name); err != nil {
			return "", fmt.Errorf("could not remove %s: %s", name, sanitizeOutputLine(errLine(aiContainersRedact(err.Error()))))
		}
		return refresh("removed " + r.word() + " " + name)
	case "inspect":
		text, err := r.h.e.InspectContext(runCtx, r.name)
		if err != nil {
			return "", errors.New(sanitizeOutputLine(errLine(aiContainersRedact(err.Error()))))
		}
		return aiContainersRedact(r.h.header(ctx) + "\nInspect " + r.word() + " " + name + "\n" + aiCap(text, loadServicesSettings().MaxInspectKB, false)), nil
	case "use":
		return call(ctx, func(m *Model) (string, tea.Cmd, error) {
			st := m.webServicesState()
			st.cancelHostWork()
			st.stopHostWatch()
			st.sessionContext = r.name
			if r.name == podmanLocal {
				st.sessionContext = ""
			}
			st.resetHostSnapshot(true)
			out := "this session now uses " + r.word() + " " + name + " (" + serverTarget(r.ctx) + "); your default is unchanged"
			if st.visible {
				return out, tea.Batch(m.containersDetectWithWatch(true), m.servicesRefresh()), nil
			}
			return out, nil, nil
		})
	}
	ep := r.ctx.DockerEndpoint
	return call(ctx, func(m *Model) (string, tea.Cmd, error) {
		key := hostTermTabPrefix + r.name
		label := "terminal: " + name
		if _, ok := hostTerminalArgv(ep); ok {
			label = "ssh: " + name
		}
		open := false
		for _, t := range m.terminalTabs {
			open = open || t.Service == key
		}
		if cmd := m.openHostTerminal(r.name, ep); cmd != nil {
			return "", cmd, errors.New("could not open a terminal on " + name + " (the IDE shows why)")
		}
		if open {
			return "focused the open terminal tab \"" + label + "\" (no new tab)", nil, nil
		}
		return "opened the terminal tab \"" + label + "\" on " + serverTarget(r.ctx) + "; what is typed there runs on that host (terminal_read reads it)", nil, nil
	})
}

// aiContextsManageTool is contexts_manage.
func (m *Model) aiContextsManageTool(call aiIDECaller) ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	session := aiContainersGate(m.services)
	return ai.Tool{
		Name: "contexts_manage",
		Description: "Manage the hosts (servers) of the Containers view, as the GUI does. op add is + Add Server… (name, host " +
			"ssh://user@host or tcp://host:2376 or unix:///path; tls_ca / tls_cert / tls_key for tcp, identity for podman ssh); " +
			"use switches this session's host (your default stays); default makes one the machine's default; remove removes one " +
			"(never the built-in, the default or the session's); inspect reads its settings; terminal opens a terminal tab on the " +
			"host (ssh to user@host, or a shell here). add, default and remove ask the user every time; use asks; inspect and terminal run at once.",
		Params: map[string]any{
			"op":          map[string]any{"type": "string", "enum": aiContextOps, "description": "what to do"},
			"name":        str("the context name (add: the new one; letters, digits, . - _)"),
			"host":        str("add: ssh://user@host[:port], tcp://host:2376 or unix:///path"),
			"description": str("add: a note shown with the context"),
			"tls_ca":      str("add (tcp): CA certificate file"), "tls_cert": str("add (tcp): client certificate file"),
			"tls_key": str("add (tcp): client key file"), "identity": str("add (podman ssh): identity file (default the ssh agent)"),
		},
		Required: []string{"op", "name"},
		Mutating: true,
		Gate: func(a map[string]any) (bool, string) {
			switch ai.ArgString(a, "op") {
			case "inspect", "terminal":
				return false, ""
			case "use":
				return session(a)
			}
			return true, ""
		},
		Validate: func(a map[string]any) error {
			ctx, cancel := context.WithTimeout(context.Background(), aiContainersCallWait)
			defer cancel()
			_, err := aiContextParse(ctx, call, a)
			return err
		},
		Describe: func(a map[string]any) string { return aiContextDescribe(call, a) },
		Run:      func(ctx context.Context, a map[string]any) (string, error) { return aiContextRun(ctx, call, a) },
	}
}
