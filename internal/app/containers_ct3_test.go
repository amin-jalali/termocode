package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
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
