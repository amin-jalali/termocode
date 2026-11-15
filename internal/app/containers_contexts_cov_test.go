package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

// The context row menu: use, default, copy, inspect, create, remove;
// copy needs an endpoint; the default card and its answer.
func TestCovContainerContextMenu(t *testing.T) {
	m := contextTestModel(t)
	f := hostFake(t)
	f.Set(t, "inspect.json", `[{"Name":"prod"}]`)
	m.services.containerEngine.Binary = f.Docker()
	var copied []string
	defer SetClipboardSink(func(s string) { copied = append(copied, s) })()
	r := svcRow{kind: svcRowHostItem, host: hostContext, hostID: "prod"}
	m.openContextMenu(r, 4, 4)
	if !m.menuOpen {
		t.Fatal("no menu")
	}
	_ = m.handleContextRowMenuSelect("copy", r)
	if len(copied) != 1 || copied[0] != "ssh://me@prod" {
		t.Fatalf("copied %v", copied)
	}
	_ = m.copyContextEndpoint("default")
	_ = m.copyContextEndpoint(podmanLocal)
	if !strings.Contains(ctrToast(m), "No endpoint") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.handleContextRowMenuSelect("default", r)
	if !m.confirmOpen || m.pendingHostOperation != "default-context" {
		t.Fatal("no default card")
	}
	done := ctrRun[hostActionDoneMsg](t, m.runHostAction("use"))
	if done.err != nil || f.Count("context use prod") != 1 {
		t.Fatalf("%+v %v", done, f.Calls())
	}
	_ = m.onHostActionDone(done)
	if !strings.Contains(ctrToast(m), "Default context is now prod") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onHostActionDone(hostActionDoneMsg{operation: "default-context", target: "prod", err: errors.New("x")})
	if !strings.Contains(ctrToast(m), "Could not change the default context") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	msg := ctrRun[contextInspectMsg](t, m.handleContextRowMenuSelect("inspect", r))
	if msg.err != nil || m.onContextInspect(msg) != nil {
		t.Fatalf("inspect %+v", msg)
	}
	_ = m.onContextInspect(contextInspectMsg{name: "prod", err: errors.New("gone")})
	if !strings.Contains(ctrToast(m), "Cannot inspect prod") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	m.promptOpen = false
	_ = m.handleContextRowMenuSelect("create", r)
	if !m.promptOpen || m.promptKind != promptKindContextName {
		t.Fatal("no create prompt")
	}
	_ = m.handleContextRowMenuSelect("remove", r)
	if !m.confirmOpen || m.pendingContextVerb != "remove" {
		t.Fatal("no remove card")
	}
	done2 := ctrRun[contextManageDoneMsg](t, m.runContextManage("remove"))
	if done2.err != nil || f.Count("context rm prod") != 1 {
		t.Fatalf("%+v %v", done2, f.Calls())
	}
	_ = m.onContextManageDone(done2)
	if !strings.Contains(ctrToast(m), "Removed context prod") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onContextManageDone(contextManageDoneMsg{verb: "create", name: "x", err: errors.New("boom")})
	if !strings.Contains(ctrToast(m), "Could not create context x") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onContextManageDone(contextManageDoneMsg{verb: "create", name: "x"})
	if !strings.Contains(ctrToast(m), "Created context x") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if m.handleContextRowMenuSelect("dance", r) != nil {
		t.Fatal("unknown id")
	}
	// The default card for podman's local row and with no binary yet.
	_ = m.askContextDefault(podmanLocal)
	if !strings.Contains(ctrToast(m), "local is not a connection") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	m.services.containerEngine.Binary = ""
	_ = m.askContextDefault("prod")
	if !strings.Contains(cardText(m), "docker context use prod") || m.askContextDefault("") != nil {
		t.Fatalf("card %s", cardText(m))
	}
}

// Create: name, host, TLS (docker tcp) or identity (podman ssh); each
// mistake stops it; the card runs context create.
func TestCovContainerContextCreate(t *testing.T) {
	m := contextTestModel(t)
	f := hostFake(t)
	m.services.containerEngine.Binary = f.Docker()
	start := func() {
		m.promptOpen = false
		_ = m.askContextCreate()
	}
	start()
	_ = m.onContextPrompt(promptKindContextName, "prod")
	if !strings.Contains(ctrToast(m), "prod already exists") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	start()
	_ = m.onContextPrompt(promptKindContextName, "edge")
	_ = m.onContextPrompt(promptKindContextHost, "tcp://10.0.0.5:2376")
	if !m.promptOpen || m.promptKind != promptKindContextTLS {
		t.Fatal("no TLS prompt")
	}
	_ = m.onContextPrompt(promptKindContextTLS, "ca.pem cert.pem")
	if !strings.Contains(ctrToast(m), "give three paths") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	start()
	_ = m.onContextPrompt(promptKindContextName, "edge")
	_ = m.onContextPrompt(promptKindContextHost, "tcp://10.0.0.5:2376")
	_ = m.onContextPrompt(promptKindContextTLS, "/ca.pem /cert.pem /key.pem")
	if !m.confirmOpen || !strings.Contains(cardText(m), "--docker") {
		t.Fatalf("card %s", cardText(m))
	}
	done := ctrRun[contextManageDoneMsg](t, m.runContextManage("create"))
	if done.err != nil || f.Count("context create edge") != 1 {
		t.Fatalf("%+v %v", done, f.Calls())
	}
	// Podman: an identity file, one only.
	m.services.containerEngine.Kind = "podman"
	start()
	_ = m.onContextPrompt(promptKindContextName, "box")
	_ = m.onContextPrompt(promptKindContextHost, "ssh://me@box")
	if m.promptKind != promptKindContextTLS {
		t.Fatal("no identity prompt")
	}
	_ = m.onContextPrompt(promptKindContextTLS, "a b")
	if !strings.Contains(ctrToast(m), "give one identity file path") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	start()
	_ = m.onContextPrompt(promptKindContextName, "box")
	_ = m.onContextPrompt(promptKindContextHost, "ssh://me@box")
	_ = m.onContextPrompt(promptKindContextTLS, "/id_ed25519")
	if !m.confirmOpen || m.pendingContextSpec.Key != "/id_ed25519" {
		t.Fatalf("podman card %+v", m.pendingContextSpec)
	}
	// A bad spec at the card.
	m.services.containerEngine.Kind = "docker"
	_ = m.askContextCreateCard(containers.ContextSpec{Name: "x"})
	if !strings.Contains(ctrToast(m), "Not created") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// No engine yet.
	m.services.containerDetected = false
	_ = m.askContextCreate()
	if !strings.Contains(ctrToast(m), "Container engine is not ready") {
		t.Fatalf("toast %s", ctrToast(m))
	}
}

// Remove refuses the default and the context this session uses; switch
// refuses an unknown one and can go back to podman's local socket.
func TestCovContainerContextGuards(t *testing.T) {
	m := contextTestModel(t)
	hostFake(t)
	m.services.contextRows = append(m.services.contextRows, containers.Context{Name: "edge"})
	m.services.contextRows[1].Current, m.services.contextRows[0].Current = true, false
	_ = m.askContextRemove("prod")
	if !strings.Contains(ctrToast(m), "This context is the default") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	m.services.sessionContext = "edge"
	_ = m.askContextRemove("edge")
	if !strings.Contains(ctrToast(m), "This session uses edge") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.askContextRemove("-bad")
	if !strings.Contains(ctrToast(m), "Cannot remove") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if m.switchContext("") != nil {
		t.Fatal("empty name")
	}
	_ = m.switchContext("nope")
	if !strings.Contains(ctrToast(m), "Unknown context") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	t.Cleanup(func() { m.services.stopHostWatch(); m.services.stopWatch() })
	m.services.containerEngine.Kind = "podman"
	m.services.hostLoaded["contexts"] = true
	m.services.sessionContext = "edge"
	_ = m.switchContext(podmanLocal)
	if m.services.sessionContext != "" {
		t.Fatalf("session %q", m.services.sessionContext)
	}
	// The palette: hidden CONTEXTS says why.
	m2 := contextTestModel(t)
	m2.services.contextRows = m2.services.contextRows[:1]
	m2.services.containerEngine.Kind = "podman"
	m2.services.hostLoaded["contexts"] = false
	m2.services.cursor = 0
	if name, cmd := m2.contextForPalette(); name != "" || cmd == nil || !strings.Contains(ctrToast(m2), "Fewer than two podman connections") {
		t.Fatalf("podman palette %q %s", name, ctrToast(m2))
	}
}
