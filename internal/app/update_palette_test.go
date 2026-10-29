package app

import (
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/update"
)

func paletteHas(items []string, id string) bool {
	for _, it := range items {
		if it == id {
			return true
		}
	}
	return false
}

func updPaletteIDs() []string {
	var ids []string
	for _, it := range updatePaletteFilter(paletteItems()) {
		ids = append(ids, it.ID)
	}
	return ids
}

// Check for Updates asks GitHub now, even with update_check off or a
// check today, and shows the card (also for a skipped version).
func TestUpdatePaletteCheck(t *testing.T) {
	f := newUpdFake(t, "v0.2.0")
	useUpdFake(t, f, "v0.1.0", updNow)
	updInstalled(t, "")
	m := &Model{w: 160, h: 40}
	updRun(t, m, m.updateInitCmd())
	m.openUpdateCard()
	m.confirmOpen = false
	m.handleConfirmSelect("skip")
	m.toast = m.toast.Dismiss("")
	if !paletteHas(updPaletteIDs(), "update-check") {
		t.Fatal("no Check for Updates in the palette")
	}
	updRun(t, m, m.dispatchPaletteAction("update-check"))
	if f.hits.Load() != 2 {
		t.Fatalf("GitHub asked %d times, want 2", f.hits.Load())
	}
	if !m.confirmOpen || m.confirmKind != confirmKindUpdateNotice {
		t.Fatal("no card for the skipped version on a manual check")
	}
	// Up to date: a toast says so.
	f.tag.Store("v0.1.0")
	m2 := &Model{w: 160, h: 40}
	updRun(t, m2, m2.dispatchPaletteAction("update-check"))
	if m2.confirmOpen || !strings.Contains(toastText(*m2), "up to date") {
		t.Fatalf("toast %q", toastText(*m2))
	}
	// GitHub down: a manual check says why.
	f.fail.Store(true)
	m3 := &Model{w: 160, h: 40}
	updRun(t, m3, m3.dispatchPaletteAction("update-check"))
	if !strings.Contains(toastText(*m3), "Could not check for updates") {
		t.Fatalf("toast %q", toastText(*m3))
	}
}

// A dev build explains instead of asking GitHub.
func TestUpdatePaletteCheckDev(t *testing.T) {
	f := newUpdFake(t, "v0.2.0")
	useUpdFake(t, f, "dev", updNow)
	m := &Model{w: 160, h: 40}
	m.dispatchPaletteAction("update-check")
	if f.hits.Load() != 0 || !strings.Contains(toastText(*m), "built from source") {
		t.Fatalf("hits %d toast %q", f.hits.Load(), toastText(*m))
	}
}

// Roll Back Update shows only with termocode.old; it asks first (Cancel
// focused) and puts the old binary back.
func TestUpdatePaletteRollback(t *testing.T) {
	f := newUpdFake(t, "v0.2.0")
	useUpdFake(t, f, "v0.2.0", updNow)
	exe := updInstalled(t, "")
	if paletteHas(updPaletteIDs(), "update-rollback") {
		t.Fatal("Roll Back shown without termocode.old")
	}
	if err := os.WriteFile(update.OldPath(exe), []byte("#!/bin/sh\necho 'termocode v0.0.9'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !paletteHas(updPaletteIDs(), "update-rollback") {
		t.Fatal("Roll Back missing with termocode.old")
	}
	m := &Model{w: 160, h: 40}
	m.dispatchPaletteAction("update-rollback")
	if !m.confirmOpen || m.confirmKind != confirmKindUpdateRollback || buttonIDs(m.confirm) != "cancel,rollback" {
		t.Fatalf("card kind %d buttons %s", m.confirmKind, buttonIDs(m.confirm))
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if !update.CanRollback(exe) {
		t.Fatal("Cancel rolled back")
	}
	m.dispatchPaletteAction("update-rollback")
	m.confirmOpen = false
	m.handleConfirmSelect("rollback")
	if b, _ := os.ReadFile(exe); !strings.Contains(string(b), "v0.0.9") || update.CanRollback(exe) {
		t.Fatal("rollback did not put termocode.old back")
	}
	if m.confirmKind != confirmKindUpdateRestart || !m.confirmOpen {
		t.Fatal("no restart card after the rollback")
	}
}
