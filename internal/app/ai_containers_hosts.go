package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/containerhost"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
)

// Servers (hosts) for the AI, as the hosts tree of the Containers view
// (CONTEXTS) and the web's containers.servers / containers.server show
// them: every docker context (podman: connection) is a host. Every
// container, image, volume, network and compose tool takes an optional
// context argument; it reads or acts on that host (DOCKER_CONTEXT /
// CONTAINER_CONNECTION on the one call, as the hosts tree does) and
// leaves the session's context alone. contexts_list names each host,
// where it points (user@host), ssh / tcp / local, the default, the
// session's, and whether it answers (one short probe per host, all at
// once, each with its own time limit).

// aiContainersProbeWait bounds the reachability probe of one host.
const aiContainersProbeWait = 6 * time.Second

// aiContextParam is the context argument of the container tools.
func aiContextParam() map[string]any {
	return map[string]any{"type": "string", "description": "host: a context (podman: connection) name from contexts_list; default the one this session uses"}
}

// aiValidContextArg checks the context argument's form (the host itself
// is checked against a fresh list when the call runs).
func aiValidContextArg(a map[string]any) error {
	name := strings.TrimSpace(ai.ArgString(a, "context"))
	if name == "" || name == podmanLocal {
		return nil
	}
	if err := containers.ValidName(name); err != nil {
		return fmt.Errorf("context: %v", err)
	}
	return nil
}

// aiOnHost adds "on <context>" to a line when the call names a host.
func aiOnHost(s string, a map[string]any) string {
	if name := strings.TrimSpace(ai.ArgString(a, "context")); name != "" {
		return s + " on " + contextDisplayName(name)
	}
	return s
}

// aiContainersHostAt is the engine of the host a names (its context
// argument), else the session's.
func aiContainersHostAt(ctx context.Context, call aiIDECaller, a map[string]any) (aiContainersHost, error) {
	name := strings.TrimSpace(ai.ArgString(a, "context"))
	if name != "" && name != podmanLocal {
		if err := containers.ValidName(name); err != nil {
			return aiContainersHost{}, fmt.Errorf("context: %v", err)
		}
	}
	h, err := aiContainersGetHost(ctx, call)
	if err != nil || name == "" {
		return h, err
	}
	list, err := webHostContexts(ctx, h)
	if err != nil {
		return h, errors.New(h.bin() + ": " + aiContainersRedact(err.Error()))
	}
	var names []string
	for _, c := range list {
		if c.Name != name {
			names = append(names, c.Name)
			continue
		}
		if aiContextIsSession(h, c) {
			return h, nil // the session's own host: nothing changes
		}
		h.e = webServerEngine(h, name)
		h.picked, h.where = contextDisplayName(name), serverTarget(c)
		h.project, h.busy = "", map[string]bool{}
		return h, nil
	}
	sort.Strings(names)
	return h, fmt.Errorf("no context %q; contexts: %s (see contexts_list)", sanitizeOutputLine(name), strings.Join(names, ", "))
}

// aiContextIsSession: c is the host the session's calls use.
func aiContextIsSession(h aiContainersHost, c containers.Context) bool {
	if h.session != "" {
		return c.Name == h.session
	}
	return c.Current
}

// termCommand is cmd for a terminal tab on h: the picked host's
// DOCKER_CONTEXT (CONTAINER_CONNECTION) replaces the session's.
func (h aiContainersHost) termCommand(cmd services.Command) services.Command {
	if h.picked == "" {
		return cmd
	}
	env := map[string]string{}
	for k, v := range cmd.Env {
		env[k] = v
	}
	k, v, ok := strings.Cut(h.e.ContextEnv(), "=")
	if !ok {
		k, v = "DOCKER_CONTEXT", ""
		if h.e.Kind == "podman" {
			k = "CONTAINER_CONNECTION"
		}
	}
	env[k] = v
	cmd.Env = env
	return cmd
}

// tabKey keys a terminal tab of h: one per host and item.
func (h aiContainersHost) tabKey(item string) string {
	if h.picked == "" {
		return item
	}
	return h.picked + "/" + item
}

// aiContextKind is ssh, tcp or local for an endpoint.
func aiContextKind(c containers.Context) string {
	ep := strings.ToLower(strings.TrimSpace(c.DockerEndpoint))
	switch {
	case containerhost.SSHEndpoint(ep):
		return "ssh"
	case strings.HasPrefix(ep, "tcp://"):
		return "tcp"
	case serverTarget(c) == "this machine":
		return "local"
	case ep == "":
		return "unknown"
	}
	return "other"
}

// aiProbeContexts asks every host for its version at once; the value is
// "" (it answers) or why not.
var aiProbeContexts = func(ctx context.Context, h aiContainersHost, list []containers.Context) map[string]string {
	out := make(map[string]string, len(list))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range list {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			e := webServerEngine(h, name)
			e.Timeout = aiContainersProbeWait
			e.Reachable, e.Problem = false, services.Problem{}
			pctx, cancel := context.WithTimeout(ctx, aiContainersProbeWait)
			defer cancel()
			e.Probe(pctx)
			why := ""
			if !e.Reachable {
				why = shortRemoteError(sanitizeOutputLine(errLine(aiContainersRedact(e.Problem.Detail))))
				if why == "" {
					why = "does not answer"
				}
			}
			mu.Lock()
			out[name] = why
			mu.Unlock()
		}(c.Name)
	}
	wg.Wait()
	return out
}

func aiContextsList(ctx context.Context, call aiIDECaller, _ map[string]any) (string, error) {
	h, err := aiContainersGetHost(ctx, call)
	if err != nil {
		return "", err
	}
	list, err := webHostContexts(ctx, h)
	if err != nil {
		return "", errors.New(aiContainersRedact(err.Error()))
	}
	word := "Contexts"
	if h.e.Kind == "podman" {
		word = "Podman connections"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s: %d. Pass a name as the context argument of any container, image, volume, network or compose tool to read or act on that host; "+
		"never ssh to it yourself. contexts_manage switches, adds or removes one.\n", h.header(ctx), word, len(list))
	probe := aiProbeContexts(ctx, h, list)
	for _, c := range list {
		where := serverTarget(c)
		if where == "" {
			where = "no endpoint"
		}
		parts := []string{contextDisplayName(c.Name), where, aiContextKind(c)}
		if c.Current {
			parts = append(parts, "engine default")
		}
		if h.session != "" && c.Name == h.session {
			parts = append(parts, "this session")
		}
		if aiContextIsSession(h, c) {
			parts = append(parts, "tools use it")
		}
		if why := probe[c.Name]; why != "" {
			parts = append(parts, "not reachable: "+why)
		} else {
			parts = append(parts, "reachable")
		}
		if c.Error != "" {
			parts = append(parts, "error: "+aiOneLineClip(c.Error, 120))
		}
		b.WriteString("- " + strings.Join(parts, " · ") + "\n")
	}
	return aiContainersRedact(strings.TrimRight(b.String(), "\n")), nil
}
