package app

import (
	"strings"
	"testing"
)

func contextTestModel(t *testing.T) *Model {
	t.Helper()
	st := svcHostState()
	st.collapsed[hostSection("contexts")] = false
	st.contextRows[1].DockerEndpoint = "ssh://me@prod"
	mm := svcTestModel(st, 120, 60)
	return &mm
}

func TestContextRemoveKeyOpensCancelFirstCard(t *testing.T) {
	m := contextTestModel(t)
	selectContext(t, m.services, "prod")
	next, _ := m.handleServicesSidebarKey(keyMsg("x"))
	*m = next
	if !m.confirmOpen || m.confirmKind != confirmKindContextManage {
		t.Fatal("no remove card")
	}
	if b := m.confirm.Buttons(); b[0].ID != "cancel" {
		t.Fatalf("Cancel is not first: %+v", b)
	}
	if card := cardText(m); !strings.Contains(card, "docker context rm prod") {
		t.Fatalf("card: %s", card)
	}
	if cmd := m.runContextManage("cancel"); cmd != nil || m.pendingContextSpec.Name != "" {
		t.Fatal("cancel ran something")
	}
}

func TestContextRemoveRefusesTheCurrentOne(t *testing.T) {
	m := contextTestModel(t)
	selectContext(t, m.services, "default")
	next, _ := m.handleServicesSidebarKey(keyMsg("x"))
	*m = next
	if m.confirmOpen {
		t.Fatal("card for the default context")
	}
}

func TestContextCreateStepsEndOnCard(t *testing.T) {
	m := contextTestModel(t)
	selectContext(t, m.services, "prod")
	next, _ := m.handleServicesSidebarKey(keyMsg("n"))
	*m = next
	if !m.promptOpen || m.promptKind != promptKindContextName {
		t.Fatal("no name prompt")
	}
	m.onContextPrompt(promptKindContextName, "stage")
	if m.promptKind != promptKindContextHost {
		t.Fatal("no host prompt")
	}
	m.onContextPrompt(promptKindContextHost, "tcp://h:2376")
	if m.promptKind != promptKindContextTLS {
		t.Fatal("no TLS prompt for tcp")
	}
	m.onContextPrompt(promptKindContextTLS, "/ca.pem /cert.pem /key.pem")
	want := "docker context create stage --docker host=tcp://h:2376,ca=/ca.pem,cert=/cert.pem,key=/key.pem"
	if !m.confirmOpen || !strings.Contains(cardText(m), want) {
		t.Fatalf("create card: %s", cardText(m))
	}
	if m.confirm.Buttons()[0].ID != "cancel" {
		t.Fatal("Cancel is not first")
	}
}

func TestContextCreateSSHSkipsTLSAndBadHostStops(t *testing.T) {
	m := contextTestModel(t)
	m.askContextCreate()
	m.onContextPrompt(promptKindContextName, "edge")
	m.onContextPrompt(promptKindContextHost, "ssh://me@edge")
	if !m.confirmOpen || !strings.Contains(cardText(m), "docker context create edge --docker host=ssh://me@edge") {
		t.Fatalf("ssh card: %s", cardText(m))
	}
	m.confirmOpen = false
	m.askContextCreate()
	m.onContextPrompt(promptKindContextName, "edge")
	if cmd := m.onContextPrompt(promptKindContextHost, "http://x"); cmd == nil || m.confirmOpen {
		t.Fatal("a bad host gives a card or no toast")
	}
	if cmd := m.onContextPrompt(promptKindContextName, "-bad"); cmd == nil {
		t.Fatal("a bad name gives no toast")
	}
}

func TestContextInspectKeysCardAndPalette(t *testing.T) {
	m := contextTestModel(t)
	selectContext(t, m.services, "prod")
	if _, cmd := m.handleServicesSidebarKey(keyMsg("i")); cmd == nil {
		t.Fatal("i on a context runs nothing")
	}
	_, rows := hostKeysHelp(svcRow{kind: svcRowHostItem, host: hostContext})
	var all []string
	for _, r := range rows {
		all = append(all, r.Key+" "+r.What)
	}
	text := strings.Join(all, "\n")
	for _, want := range []string{"i inspect", "n create", "x remove"} {
		if !strings.Contains(text, want) {
			t.Errorf("key card lacks %q: %s", want, text)
		}
	}
	ids := map[string]bool{}
	for _, it := range servicesPaletteItems() {
		ids[it.ID] = true
	}
	for _, id := range []string{"containers-context-create", "containers-context-inspect", "containers-context-remove"} {
		if !ids[id] {
			t.Errorf("palette lacks %s", id)
		}
	}
}
