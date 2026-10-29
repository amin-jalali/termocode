package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

// The AI removes a volume by name. A host with more volumes than
// networks must not read past the network list (it panicked).
func TestCovSvcAIVolumeRemoveMoreVolumesThanNetworks(t *testing.T) {
	st := svcHostState()
	st.networkRows = []containers.Network{{Name: "bridge", Driver: "bridge"}}
	st.volumeRows = []containers.Volume{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	m := svcTestModel(st, 100, 40)
	cmd, err := m.dispatchContainerAIAction("containers-volume-remove", "c")
	if err != nil {
		t.Fatalf("volume c: %v", err)
	}
	if cmd == nil {
		t.Fatal("no use check started for volume c")
	}
	if _, err := m.dispatchContainerAIAction("containers-volume-remove", "zz"); err == nil || !strings.Contains(err.Error(), `volume "zz" was not found`) {
		t.Errorf("unknown volume: %v", err)
	}
	if _, err := m.dispatchContainerAIAction("containers-network-remove", "zz"); err == nil || !strings.Contains(err.Error(), `network "zz" was not found`) {
		t.Errorf("unknown network: %v", err)
	}
}
