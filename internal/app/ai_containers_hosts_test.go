package app

import (
	"context"
	"strings"
	"testing"
)

// Servers (hosts): every container tool reads or acts on any host (a
// docker context / podman connection) by its context argument; the
// default stays the session's. contexts_list says where each host
// points, which is the default and the session's, and whether it answers.

// The tools of the Containers area that touch an engine take context.
func TestAIContainersToolsTakeContext(t *testing.T) {
	m := &Model{services: newServicesState()}
	for _, tl := range m.aiContainersTools(nil) {
		if tl.Name == "registries_list" || tl.Name == "contexts_list" || tl.Name == "contexts_manage" {
			continue // the login file and the context list are this machine's
		}
		p, ok := tl.Params["context"].(map[string]any)
		if !ok || !strings.Contains(p["description"].(string), "contexts_list") {
			t.Errorf("%s has no context argument naming contexts_list: %v", tl.Name, tl.Params["context"])
		}
	}
}

// contexts_list: the name, where it points (user@host), ssh or local, the
// engine, the default and whether it answers (a remote that fails says why).
func TestAIContextsListWhereAndReachable(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	f.Set(t, "fail-prod.err", "error during connect: ssh: connect to host prod.example port 22: Connection refused")
	out, err := aiContainersTool(t, m.aiContainersTools(call), "contexts_list").Run(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "contexts_list", out,
		"default · this machine · local · engine default · tools use it · reachable",
		"prod · deploy@prod.example · ssh · not reachable: ssh: Connection refused",
		"context argument")
	probed := false
	for _, c := range f.CtxCalls() {
		if strings.HasPrefix(c, "prod|version") {
			probed = true
		}
	}
	if !probed {
		t.Errorf("prod was not probed: %q", f.CtxCalls())
	}
}

// containers_list context=prod reads prod (DOCKER_CONTEXT=prod on every
// engine call but the context list) and names it; the session is unchanged.
func TestAIContainersListOtherContext(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	out, err := aiContainersTool(t, m.aiContainersTools(call), "containers_list").Run(context.Background(), map[string]any{"context": "prod"})
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "containers_list prod", out, "context prod (deploy@prod.example)", "termocode-api-1")
	sawPS := false
	for _, c := range f.CtxCalls() {
		ctx, argv, _ := strings.Cut(c, "|")
		if strings.HasPrefix(argv, "context ") {
			continue
		}
		if ctx != "prod" {
			t.Errorf("call without DOCKER_CONTEXT=prod: %q", c)
		}
		sawPS = sawPS || strings.HasPrefix(argv, "ps ")
	}
	if !sawPS {
		t.Errorf("no ps on prod: %q", f.CtxCalls())
	}
	if m.services.sessionContext != "" {
		t.Errorf("the session context moved to %q", m.services.sessionContext)
	}
	// A host that does not answer fails with its reason.
	f.Set(t, "fail-prod.err", "error during connect: ssh: connect to host prod.example port 22: Connection refused")
	if _, err := aiContainersTool(t, m.aiContainersTools(call), "containers_list").Run(context.Background(), map[string]any{"context": "prod"}); err == nil ||
		!strings.Contains(err.Error(), "context prod") {
		t.Errorf("unreachable prod: %v", err)
	}
}

// An unknown or bad context is refused before any card, naming the hosts.
func TestAIContainersUnknownContextRefused(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	tools := m.aiContainersTools(call)
	for _, name := range []string{"images_list", "volumes_list", "networks_list", "containers_stats"} {
		_, err := aiContainersTool(t, tools, name).Run(context.Background(), map[string]any{"context": "nope"})
		if err == nil || !strings.Contains(err.Error(), "no context \"nope\"") || !strings.Contains(err.Error(), "prod") {
			t.Errorf("%s context nope: %v", name, err)
		}
	}
	ctl := aiContainersTool(t, tools, "containers_control")
	if err := ctl.Validate(map[string]any{"action": "stop", "target": "termocode-api-1", "context": "-H"}); err == nil {
		t.Error("context -H accepted")
	}
	for _, c := range f.Calls() {
		if !strings.HasPrefix(c, "context ") && !strings.HasPrefix(c, "version") {
			t.Errorf("a refused call ran %q", c)
		}
	}
}

// A write on another host: the card names the host, and the call runs there.
func TestAIContainersControlOtherContext(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	ctl := aiContainersTool(t, m.aiContainersTools(call), "containers_control")
	a := map[string]any{"action": "restart", "target": "termocode-api-1", "context": "prod"}
	if err := ctl.Validate(a); err != nil {
		t.Fatal(err)
	}
	if got := ctl.Describe(a); !strings.Contains(got, "context prod (deploy@prod.example)") {
		t.Errorf("card = %q", got)
	}
	if _, err := ctl.Run(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	ran := false
	for _, c := range f.CtxCalls() {
		if c == "prod|restart 9f3c2e1a1234" {
			ran = true
		}
	}
	if !ran {
		t.Errorf("restart did not run on prod: %q", f.CtxCalls())
	}
}
