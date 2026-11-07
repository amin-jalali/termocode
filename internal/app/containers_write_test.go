package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

// cursorOnContainer puts the cursor on the row of container id.
func cursorOnContainer(t *testing.T, m *Model, id string) {
	t.Helper()
	for i, r := range m.services.rows() {
		if r.kind == svcRowHostItem && r.host == hostContainer && r.idx >= 0 && m.services.containerRows[r.idx].ID == id {
			m.services.cursor = i
			return
		}
	}
	t.Fatalf("no row for container %s", id)
}

func writeTestModel(t *testing.T) *Model {
	t.Helper()
	st := svcHostState()
	st.containerRows[4].Networks = []string{"bridge"} // nginx
	mm := svcTestModel(st, 120, 50)
	return &mm
}

func TestContainerKillKeyOpensCancelFirstCard(t *testing.T) {
	m := writeTestModel(t)
	cursorOnContainer(t, m, "c5")
	next, _ := m.handleServicesSidebarKey(keyMsg("K"))
	*m = next
	if !m.confirmOpen || m.confirmKind != confirmKindContainerWrite {
		t.Fatal("no kill card")
	}
	b := m.confirm.Buttons()
	if len(b) < 2 || b[0].ID != "cancel" {
		t.Fatalf("Cancel is not first: %+v", b)
	}
	if card := cardText(m); !strings.Contains(card, "docker kill --signal KILL c5") {
		t.Fatalf("card: %s", card)
	}
	if cmd := m.runContainerWrite("cancel"); cmd != nil || m.pendingContainerWrite.verb != "" {
		t.Fatal("cancel ran something or kept the pending action")
	}
}

func TestContainerKillNeedsARunningContainer(t *testing.T) {
	m := writeTestModel(t)
	cursorOnContainer(t, m, "c3") // exited
	next, _ := m.handleServicesSidebarKey(keyMsg("K"))
	*m = next
	if m.confirmOpen {
		t.Fatal("kill card for a stopped container")
	}
}

func TestContainerRenameKeyOpensPrompt(t *testing.T) {
	m := writeTestModel(t)
	cursorOnContainer(t, m, "c5")
	next, _ := m.handleServicesSidebarKey(keyMsg("N"))
	*m = next
	if !m.promptOpen || m.promptKind != promptKindContainerRename || m.pendingContainerWrite.id != "c5" {
		t.Fatal("no rename prompt")
	}
	if cmd := m.onContainerWritePrompt(promptKindContainerRename, "bad name"); cmd == nil {
		t.Fatal("a bad name gives no toast")
	}
}

func TestContainerRestartPolicyPickThenCard(t *testing.T) {
	m := writeTestModel(t)
	items := containerPolicyItems("c5")
	if len(items) < 4 || !strings.HasPrefix(items[0].ID, "svc:ctr-policy:c5|") {
		t.Fatalf("policy items: %+v", items)
	}
	m.handleServicesPickerSelect("svc:ctr-policy:c5|unless-stopped")
	if !m.confirmOpen || !strings.Contains(cardText(m), "docker update --restart unless-stopped c5") {
		t.Fatalf("policy card: %s", cardText(m))
	}
	if m.confirm.Buttons()[0].ID != "cancel" {
		t.Fatal("Cancel is not first")
	}
}

func TestContainerResourcesAnswer(t *testing.T) {
	r, err := parseResourcesAnswer("1.5 512m -")
	if err != nil || r != (containers.Resources{CPUs: "1.5", Memory: "512m"}) {
		t.Fatalf("parse: %+v %v", r, err)
	}
	if _, err := parseResourcesAnswer("- - -"); err == nil {
		t.Fatal("nothing to change accepted")
	}
	if _, err := parseResourcesAnswer("1 2 3 4"); err == nil {
		t.Fatal("four values accepted")
	}
	m := writeTestModel(t)
	m.pendingContainerWrite = containerWrite{id: "c5"}
	m.onContainerWritePrompt(promptKindContainerResources, "2 1g")
	if !m.confirmOpen || !strings.Contains(cardText(m), "docker update --cpus 2 --memory 1g c5") {
		t.Fatalf("resources card: %s", cardText(m))
	}
}

func TestContainerNetworkItems(t *testing.T) {
	m := writeTestModel(t)
	c := m.services.containerRows[4]
	conn := m.containerNetworkItems(c, true)
	if len(conn) != 1 || !strings.HasSuffix(conn[0].ID, "|termocode_default") {
		t.Fatalf("connect items: %+v", conn)
	}
	disc := m.containerNetworkItems(c, false)
	if len(disc) != 1 || !strings.HasSuffix(disc[0].ID, "|bridge") {
		t.Fatalf("disconnect items: %+v", disc)
	}
	m.handleServicesPickerSelect(disc[0].ID)
	if !m.confirmOpen || !strings.Contains(cardText(m), "docker network disconnect bridge c5") {
		t.Fatalf("disconnect card: %s", cardText(m))
	}
}

func TestContainerWriteKeysInCardPaletteAndMenu(t *testing.T) {
	_, rows := hostKeysHelp(svcRow{kind: svcRowHostItem, host: hostContainer})
	var all []string
	for _, r := range rows {
		all = append(all, r.Key+" "+r.What)
	}
	text := strings.Join(all, "\n")
	for _, want := range []string{"K kill", "N rename", "w change"} {
		if !strings.Contains(text, want) {
			t.Errorf("key card lacks %q: %s", want, text)
		}
	}
	ids := map[string]bool{}
	for _, it := range servicesPaletteItems() {
		ids[it.ID] = true
	}
	for _, id := range []string{"containers-kill", "containers-rename", "containers-restart-policy", "containers-resources", "containers-network-connect", "containers-network-disconnect"} {
		if !ids[id] {
			t.Errorf("palette lacks %s", id)
		}
	}
	m := writeTestModel(t)
	cursorOnContainer(t, m, "c5")
	r, _ := m.servicesCurrentRow()
	m.openContainerMenu(r, 1, 1)
	got := map[string]bool{}
	for _, it := range containerWriteMenuItems() {
		got[it.ID] = true
	}
	for _, id := range []string{"ctr-kill", "ctr-rename", "ctr-policy", "ctr-resources", "ctr-net-connect", "ctr-net-disconnect"} {
		if !got[id] {
			t.Errorf("menu lacks %s", id)
		}
	}
}
