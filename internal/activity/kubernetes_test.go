package activity

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The Kubernetes view has no icon of its own: it lives under Containers,
// so the Containers icon is lit while it shows, and a click on that icon
// goes back to the Containers view (not a sidebar toggle).
func TestKubernetesLightsTheContainersIcon(t *testing.T) {
	for _, it := range topItems {
		if it.view == ViewKubernetes {
			t.Fatal("Kubernetes must not add an icon to the bar")
		}
	}
	m := New()
	m.SetHeight(40)
	m.SetActive(ViewServices)
	want := m.View()
	m.SetActive(ViewKubernetes)
	if got := m.View(); got != want {
		t.Errorf("bar with Kubernetes active differs from Containers active:\n%s\nwant\n%s", got, want)
	}
	y := -1
	for _, it := range m.Items() {
		if it.View == ViewServices {
			y = it.Y
		}
	}
	_, cmd := m.HandleMouse(1, y, tea.MouseLeft)
	if cmd == nil {
		t.Fatal("no message")
	}
	if sw, ok := cmd().(SwitchMsg); !ok || sw.View != ViewServices {
		t.Errorf("click = %#v, want a switch to Containers", cmd())
	}
}
