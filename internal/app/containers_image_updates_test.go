package app

import (
	"slices"
	"strings"
	"testing"
)

func TestImageUpdateTargetsAreRunningTagsOnly(t *testing.T) {
	st := svcHostState()
	st.containerRows[1].Image = "redis@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	got := imageUpdateTargets(st)
	// running: postgres:16, (redis pinned by digest: skipped), shop, nginx:1.27; exited/paused skipped.
	if !slices.Equal(got, []string{"nginx:1.27", "postgres:16", "shop"}) {
		t.Fatalf("targets %q", got)
	}
}

func TestImageUpdateNoticeMarksTheContainers(t *testing.T) {
	m := svcTestModel(svcHostState(), 120, 40)
	m.onImageUpdates(imageUpdatesMsg{updates: map[string]bool{"nginx:1.27": true, "postgres:16": false}, skipped: 1})
	if !m.services.imageUpdates["nginx:1.27"] || m.services.imageUpdates["postgres:16"] {
		t.Fatalf("updates %v", m.services.imageUpdates)
	}
	row := svcRow{kind: svcRowHostItem, host: hostContainer, idx: 4} // c5 nginx
	l1, _ := m.services.hostFooter(row)
	if !strings.Contains(l1, "update available") {
		t.Fatalf("footer %q", l1)
	}
	if !strings.Contains(strings.Join(m.toast.View(120, 40), " "), "nginx:1.27") {
		t.Fatal("the toast must name the image with an update")
	}
}
