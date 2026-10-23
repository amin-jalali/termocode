package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
)

// cardText is the card's words on one line. The side borders go, so a
// command wrapped onto the next row (a long temp path) still reads whole.
func cardText(m *Model) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(ansi.Strip(m.confirm.View()), "│", " ")), " ")
}

func pruneToast(m *Model) string { return strings.Join(m.toast.View(120, 40), " ") }

func TestPruneCardsShowCountsAndExactCommand(t *testing.T) {
	st := svcHostState()
	st.imageRows = append(st.imageRows, containers.Image{ID: "sha256:ddd", RepoTags: nil, Size: "10MB"})
	mm := svcTestModel(st, 120, 50)
	m := &mm
	cases := []struct {
		kind   string
		count  string
		cmd    string
		absent []string
	}{
		{"containers", "1 stopped container", "docker container prune -f", []string{" -a ", "--volumes"}},
		{"images", "1 untagged image", "docker image prune -f", []string{" -a ", "--volumes"}},
		{"images-all", "unused", "docker image prune -a -f", nil},
		{"networks", "unused networks", "docker network prune -f", []string{" -a "}},
		{"volumes", "unnamed volumes", "docker volume prune -f", []string{" -a "}},
		{"system", "1 stopped container", "docker system prune -f", []string{" -a ", "--volumes"}},
	}
	for _, c := range cases {
		m.confirmOpen = false
		if cmd := m.askPrune(c.kind); cmd != nil && c.kind != "volumes" {
			t.Fatalf("%s: card step returned a command", c.kind)
		}
		if c.kind == "volumes" {
			m.showPrune("volumes", []string{"abc123"})
		}
		if !m.confirmOpen {
			t.Fatalf("%s: no card", c.kind)
		}
		card := cardText(m)
		if !strings.Contains(card, c.count) || !strings.Contains(card, c.cmd) {
			t.Errorf("%s: card lacks %q / %q: %s", c.kind, c.count, c.cmd, card)
		}
		for _, a := range c.absent {
			if strings.Contains(card+" ", a) {
				t.Errorf("%s: card has %q: %s", c.kind, a, card)
			}
		}
	}
	m.showPrune("images-all", nil)
	if !strings.Contains(cardText(m), "unused images") {
		t.Errorf("incl. unused card does not say so: %s", cardText(m))
	}
}

func TestPruneEscRunsNothingAndConfirmRunsExactArgv(t *testing.T) {
	want := map[string]string{
		"containers": "container prune -f", "images": "image prune -f", "images-all": "image prune -a -f",
		"networks": "network prune -f", "system": "system prune -f",
	}
	for kind, argv := range want {
		m, f := ctxSession(t)
		f.Set(t, "prune.out", "Deleted Things\n\nTotal reclaimed space: 1.2GB\n")
		pumpHost(t, m, m.askPrune(kind))
		if !m.confirmOpen {
			t.Fatalf("%s: no card", kind)
		}
		next, cmd := m.Update(confirm.CloseMsg{})
		*m = next.(Model)
		pumpHost(t, m, cmd)
		if f.Count("prune") != 0 || m.confirmOpen {
			t.Fatalf("%s: Esc ran a command: %q", kind, f.Calls())
		}
		pumpHost(t, m, m.askPrune(kind))
		pumpHost(t, m, m.handleConfirmSelect("prune"))
		var got []string
		for _, c := range f.Calls() {
			if strings.Contains(c, "prune") {
				got = append(got, c)
			}
		}
		if len(got) != 1 || got[0] != argv {
			t.Fatalf("%s: prune calls = %q, want %q", kind, got, argv)
		}
		if !strings.Contains(pruneToast(m), "Pruned") || !strings.Contains(pruneToast(m), "reclaimed 1.2GB") {
			t.Errorf("%s: toast = %q", kind, pruneToast(m))
		}
	}
}

func TestPruneVolumesRunsVolumePrune(t *testing.T) {
	m, f := ctxSession(t)
	pumpHost(t, m, m.askPrune("volumes"))
	if !m.confirmOpen {
		t.Fatal("no card")
	}
	pumpHost(t, m, m.handleConfirmSelect("prune"))
	if f.Count("volume prune -f") != 1 {
		t.Fatalf("calls = %q", f.Calls())
	}
}

func TestPruneErrorShowsEngineTextAndRefreshes(t *testing.T) {
	m, f := ctxSession(t)
	f.Set(t, "prune.err", "Error response from daemon: a prune operation is already running\n")
	pumpHost(t, m, m.askPrune("system"))
	pumpHost(t, m, m.handleConfirmSelect("prune"))
	txt := pruneToast(m)
	if !strings.Contains(txt, "already running") || strings.Contains(txt, "Pruned") {
		t.Fatalf("error toast = %q", txt)
	}
}

func TestPruneCardNamesNonDefaultContextAndRunsThere(t *testing.T) {
	m, f := ctxSession(t)
	st := m.services
	selectContext(t, st, "prod")
	pumpHost(t, m, press(m, "enter"))
	pumpHost(t, m, m.askPrune("system"))
	if !strings.Contains(cardText(m), "context prod") {
		t.Fatalf("card does not name the context: %s", cardText(m))
	}
	pumpHost(t, m, m.handleConfirmSelect("prune"))
	found := false
	for _, c := range f.CtxCalls() {
		if c == "prod|system prune -f" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no prune under DOCKER_CONTEXT=prod: %q", f.CtxCalls())
	}
}

func TestPrunePaletteAndAIPolicy(t *testing.T) {
	titles := map[string]string{}
	for _, e := range PaletteCatalog() {
		titles[e.ID] = e.Title
	}
	for id, title := range map[string]string{
		"containers-prune":         "Containers: Prune Stopped Containers…",
		"containers-image-prune":   "Containers: Prune Dangling Images…",
		"containers-volume-prune":  "Containers: Prune Volumes…",
		"containers-network-prune": "Containers: Prune Networks…",
		"containers-system-prune":  "Containers: Prune System…",
	} {
		if titles[id] != title {
			t.Errorf("palette %s = %q, want %q", id, titles[id], title)
		}
		if p, ok := aiActionPolicies[id]; !ok || p.Gate != aiGateNever || p.Reason == "" {
			t.Errorf("%s: AI policy %+v ok=%v, want never with a reason", id, p, ok)
		}
		m := svcTestModel(svcHostState(), 100, 40)
		if _, err := m.dispatchContainerAIAction(id, "x"); err == nil || m.confirmOpen {
			t.Errorf("the AI dispatcher accepted %s", id)
		}
	}
}

func TestPrunePaletteOpensCards(t *testing.T) {
	for id, want := range map[string]string{
		"containers-prune": "container prune -f", "containers-system-prune": "system prune -f",
		"containers-image-prune": "image prune -f", "containers-network-prune": "network prune -f",
	} {
		m, _ := ctxSession(t)
		cmd, ok := m.dispatchServicesPalette(id)
		if !ok {
			t.Fatalf("%s not handled", id)
		}
		pumpHost(t, m, cmd)
		if !m.confirmOpen || !strings.Contains(cardText(m), want) {
			t.Errorf("%s: card = %q", id, cardText(m))
		}
	}
}

func TestPruneMenusOfferTheItems(t *testing.T) {
	m, _ := ctxSession(t)
	// The ⋯ menu has Prune system… and selecting it opens the system card.
	m.openContainersSectionsMenu("")
	if !strings.Contains(ansi.Strip(m.menu.View()), "Prune system…") {
		t.Fatalf("⋯ menu has no Prune system…:\n%s", ansi.Strip(m.menu.View()))
	}
	pumpHost(t, m, m.handleContainersMenuSelect("prune-system"))
	if !m.confirmOpen || !strings.Contains(cardText(m), "system prune -f") {
		t.Fatalf("no system card: %s", cardText(m))
	}
	m.confirmOpen = false
	// Each section header menu has its prune items.
	for section, items := range map[string][]string{
		"containers": {"Prune stopped containers…"},
		"images":     {"Prune dangling images…", "Prune incl. unused images…"},
		"networks":   {"Prune unused networks…"},
		"volumes":    {"Prune dangling volumes…"},
	} {
		var row svcRow
		for _, r := range m.services.rows() {
			if r.kind == svcRowSection && r.section == hostSection(section) {
				row = r
			}
		}
		m.openSectionMenu(row, 10, 5)
		view := ansi.Strip(m.menu.View())
		for _, it := range items {
			if !strings.Contains(view, it) {
				t.Errorf("%s header menu lacks %q:\n%s", section, it, view)
			}
		}
	}
	// Choosing the images -a item opens the card with -a.
	var row svcRow
	for _, r := range m.services.rows() {
		if r.kind == svcRowSection && r.section == hostSection("images") {
			row = r
		}
	}
	m.openSectionMenu(row, 10, 5)
	cmd := m.handleServicesMenuSelect("prune-images-all")
	pumpHost(t, m, cmd)
	if !strings.Contains(cardText(m), "image prune -a -f") {
		t.Fatalf("card = %s", cardText(m))
	}
}
