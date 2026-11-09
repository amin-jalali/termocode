package assets

import (
	"io/fs"
	"strings"
	"testing"
)

// budgetDevice is the desktop's This device page with Pair a Device… and
// Devices… (device.js), gzipped; the workbench loads it when one of them
// opens. About 4 KiB today.
const budgetDevice = 8 << 10

// Pair a Device… and Devices… are in device.js alone: neither the phone's
// page nor workbench.js holds them, and device.js imports nothing (the
// workbench gives it the page's Preact, sheet and link), so it never
// brings a second copy of a module or a shared chunk of its own.
func TestDeviceChunkHoldsThePairing(t *testing.T) {
	if _, err := fs.Stat(FS(), "device.js"); err != nil {
		t.Fatalf("no device.js: %v", err)
	}
	dev, wb, page := readBuilt(t, "device.js"), readBuilt(t, "workbench.js"), readPage(t)
	for _, want := range []string{`"web.pair_open"`, `"web.pair_answer"`, `"web.devices"`, `"web.device_remove"`, `"pair.ask"`, `"pair.end"`} {
		if !strings.Contains(dev, want) {
			t.Errorf("device.js has no %s", want)
		}
		if strings.Contains(wb, want) || strings.Contains(page, want) {
			t.Errorf("workbench.js or the phone's page holds %s; it belongs in device.js", want)
		}
	}
	for _, bad := range []string{"import ", "import(", "app-chunk"} {
		if strings.Contains(dev, bad) {
			t.Errorf("device.js imports something (%q): the workbench gives it what it needs", bad)
		}
	}
	if !strings.Contains(wb, `import("/assets/device.js")`) {
		t.Error("the workbench does not load /assets/device.js")
	}
	if strings.Contains(readBuilt(t, "index.html"), "device.js") {
		t.Error("index.html names device.js: only the workbench loads it, when it opens")
	}
	if n := gzSize(t, "device.js"); n > budgetDevice {
		t.Errorf("device.js is %d B gzipped, budget %d B", n, budgetDevice)
	} else {
		t.Logf("gzipped: device.js %d B (desktop only, on first use)", n)
	}
}
