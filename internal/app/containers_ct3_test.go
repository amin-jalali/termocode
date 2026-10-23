package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
)

// CT-3: the clean-up and remove cards use plain words, the title and the
// button name the same action, and the exact command comes last.
func TestCT3PruneCardsPlainWords(t *testing.T) {
	st := svcHostState()
	st.imageRows = append(st.imageRows, containers.Image{ID: "sha256:ddd", Size: "10MB"})
	mm := svcTestModel(st, 120, 50)
	m := &mm
	long := strings.Repeat("0123456789abcdef", 4)
	cases := []struct {
		kind, title, button string
		want                []string
	}{
		{"system", "Clean up unused items?", "Clean up", []string{"untagged image", "old build files"}},
		{"images", "Remove untagged images?", "Remove", []string{"1 untagged image"}},
		{"volumes", "Remove unused volumes?", "Remove", []string{"data is deleted", "Unnamed volumes", long[:12]}},
	}
	for _, c := range cases {
		m.confirmOpen = false
		m.askPrune(c.kind)
		if c.kind == "volumes" {
			m.showPrune("volumes", []string{long})
		}
		card := cardText(m)
		if !strings.Contains(card, c.title) {
			t.Errorf("%s: title %q missing: %s", c.kind, c.title, card)
		}
		bs := m.confirm.Buttons()
		if len(bs) != 2 || bs[1].Title != c.button {
			t.Errorf("%s: buttons %+v, want %q", c.kind, bs, c.button)
		}
		for _, w := range c.want {
			if !strings.Contains(card, w) {
				t.Errorf("%s: card lacks %q: %s", c.kind, w, card)
			}
		}
		for _, bad := range []string{"dangling", "build cache", "Prune", long, "anonymous"} {
			if strings.Contains(card, bad) {
				t.Errorf("%s: card still says %q: %s", c.kind, bad, card)
			}
		}
		if i := strings.Index(card, "Command:"); i < 0 || strings.Contains(card[i:], "Volumes and") {
			t.Errorf("%s: the command is not last: %s", c.kind, card)
		}
	}
}

func TestCT3RemoveCardsShortIDsAndShortPaths(t *testing.T) {
	st := svcHostState()
	mm := svcTestModel(st, 120, 50)
	m := &mm
	c := containers.Container{ID: strings.Repeat("ab12", 16), Names: []string{"migrate"}, State: "exited"}
	m.askRemoveContainer(c)
	card := cardText(m)
	if !strings.Contains(card, "docker rm ab12ab12ab12") || strings.Contains(card, c.ID) {
		t.Errorf("remove card does not use the short id: %s", card)
	}
	if bs := m.confirm.Buttons(); bs[1].Title != "Remove" {
		t.Errorf("buttons %+v", bs)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := displayCommand([]string{"docker", "compose", "--project-directory", home + "/proj/docs", "-f", "compose.yaml", "down"}, "")
	if got != "docker compose --project-directory ~/proj/docs -f compose.yaml down" {
		t.Errorf("displayCommand = %q", got)
	}
}

// CT-7: one container, one name: the one the list shows. A compose
// container "termocode-api-1" is "api-1" in the list, so it is "api-1" in
// the busy message and in the AI control card too.
func TestCT7OneNameEverywhere(t *testing.T) {
	st := svcHostState()
	mm := svcTestModel(st, 120, 50)
	m := &mm
	c := st.containerRows[0] // termocode-postgres-1, project termocode
	want := containerDisplayName(c, c.Project)
	if want != "postgres-1" {
		t.Fatalf("the list name is %q", want)
	}
	st.containerEngine.Reachable = true
	st.busy[containerBusyPrefix+c.ID] = services.Stopping
	m.containerAction("stop", c)
	toasts := strings.Join(m.toast.View(120, 40), " ")
	if !strings.Contains(toasts, "postgres-1") || strings.Contains(toasts, "termocode-postgres-1") {
		t.Errorf("busy toast: %q", toasts)
	}
	m.askRemoveContainer(c)
	if card := cardText(m); !strings.Contains(card, "postgres-1") || strings.Contains(card, "termocode-postgres-1") {
		t.Errorf("remove card: %s", card)
	}
}

// CT-11: no Docker or Podman is said the same way in PROJECT (compose
// problem) and in CONTAINERS: one note, one two-line hint.
func TestCT11NoEngineOneText(t *testing.T) {
	st := svcHostState()
	st.problem = services.Problem{Kind: services.ProblemNoDocker}
	got := st.problemRows()
	want := []string{"No container engine", "Install Docker or Podman,", "then press R to retry"}
	if len(got) != len(want) {
		t.Fatalf("PROJECT rows: %+v", got)
	}
	for i, r := range got {
		if r.text != want[i] {
			t.Errorf("PROJECT row %d = %q, want %q", i, r.text, want[i])
		}
	}
	st.containerEngine = containers.Engine{}
	var host []string
	for _, r := range st.hostSectionBody("containers") {
		host = append(host, r.text)
	}
	if strings.Join(host, "|") != strings.Join(want, "|") {
		t.Errorf("CONTAINERS rows: %q", host)
	}
	if strings.Contains(services.Problem{Kind: services.ProblemNoDocker}.Message(), "not found") {
		t.Error("the AI still hears the old text")
	}
}
