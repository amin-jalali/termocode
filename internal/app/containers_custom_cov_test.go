package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ctrCustomAll = `{"commands":[
 {"name":"Img","view":"images","key":"J","argv":["docker","image","inspect","{{id}}"]},
 {"name":"Net","view":"networks","argv":["docker","network","inspect","{{name}}"]},
 {"name":"Vol","view":"volumes","argv":["docker","volume","inspect","{{name}}"]},
 {"name":"Proj","view":"compose","argv":["docker","compose","-p","{{name}}","ps"]},
 {"name":"Pod","view":"pods","argv":["kubectl","describe","pod","{{name}}"]},
 {"name":"Bad","view":"containers","argv":["docker","top","{{nope}}"]}
]}`

// Each kind of row has its own custom commands: images, networks,
// volumes, compose groups and services, and Kubernetes rows.
func TestCovContainerCustomRows(t *testing.T) {
	writeCustomCommands(t, ctrCustomAll)
	m := ctrStorageModel(t)
	t.Setenv("XDG_CONFIG_HOME", os.Getenv("XDG_CONFIG_HOME"))
	writeCustomCommands(t, ctrCustomAll)
	check := func(view string) {
		t.Helper()
		m.pickerOpen = false
		_ = m.runCustomFromPalette()
		if !m.pickerOpen || m.services.customRow.view != view {
			t.Fatalf("%s: picker %v row %+v (%s)", view, m.pickerOpen, m.services.customRow, ctrToast(m))
		}
	}
	ctrImageRow(t, m)
	check("images")
	if !strings.Contains(m.picker.View(), "J · ") {
		t.Fatal("no key hint")
	}
	if cmd, ok := m.customCommandKey("J", m.services.customRow, true); !ok || cmd != nil || !m.confirmOpen {
		t.Fatal("J did not open the card")
	}
	if _, ok := m.customCommandKey("", customRow{}, true); ok {
		t.Fatal("empty key")
	}
	if _, ok := m.customCommandKey("Q", m.services.customRow, true); ok {
		t.Fatal("unbound key")
	}
	ctrStorageRow(t, m, hostNetwork, "bridge")
	check("networks")
	ctrStorageRow(t, m, hostVolume, "termocode_db")
	check("volumes")
	ctrGroupRow(t, m, "shop")
	check("compose")
	m.services.sections["project"] = true
	m.services.touch()
	selectRow(t, m.services, func(r svcRow) bool { return r.kind == svcRowService && r.section == svcSecCompose })
	check("compose")
	// A section row has none.
	ctrSectionRow(t, m, "volumes")
	_ = m.runCustomFromPalette()
	if !strings.Contains(ctrToast(m), "Select a row first") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// A placeholder that does not exist.
	cursorOnContainer(t, m, "c5")
	_ = m.runCustomFromPalette()
	_ = m.pickCustomCommand("0")
	if !strings.Contains(ctrToast(m), "Cannot run Bad") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// Kubernetes.
	m.kube = newKubeState()
	m.kubeDemo("")
	m.kube.visible = true
	for i, r := range m.kube.rows() {
		if r.kind == kubeRowItem && r.section == "pods" {
			m.kube.cursor = i
			break
		}
	}
	if row, ok := m.kubeCustomRow(); !ok || row.view != "pods" {
		t.Fatalf("kube row %+v", row)
	}
	m.pickerOpen = false
	_ = m.runCustomFromPalette()
	if !m.pickerOpen {
		t.Fatalf("kube: %s", ctrToast(m))
	}
	_ = m.pickCustomCommand("0")
	if !m.confirmOpen || !strings.HasPrefix(m.services.devrun.tab, "kube-custom:") {
		t.Fatalf("kube tab %q", m.services.devrun.tab)
	}
	m.kube.cursor = 0
	if _, ok := m.kubeCustomRow(); ok {
		t.Fatal("kube section row")
	}
	m.kube = nil
	if _, ok := m.kubeCustomRow(); ok {
		t.Fatal("no kube state")
	}
}

// The commands file: a missing one is no commands, a broken one or a
// bad command says so where it is used.
func TestCovContainerCustomFile(t *testing.T) {
	m, _ := ctrModel(t)
	cursorOnContainer(t, m, "c5")
	_ = m.runCustomFromPalette()
	if !strings.Contains(ctrToast(m), "No custom commands for containers") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	for _, body := range []string{`[{"name":`, `{"commands":`, `[{"name":"","view":"containers","argv":["x"]}]`} {
		writeCustomCommands(t, body)
		_ = m.runCustomFromPalette()
		if !strings.Contains(ctrToast(m), "Custom commands not loaded") {
			t.Fatalf("%q: toast %s", body, ctrToast(m))
		}
		_ = m.pickCustomCommand("0")
		if _, ok := m.customCommandKey("J", customRow{view: "containers"}, true); ok {
			t.Fatal("key from a broken file")
		}
	}
	writeCustomCommands(t, `[{"name":"Top","view":"containers","argv":["docker","top","{{id}}"]}]`)
	_ = m.runCustomFromPalette()
	_ = m.pickCustomCommand("7")
	if !strings.Contains(ctrToast(m), "The custom commands changed") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// An unreadable file.
	dir, _ := configDir()
	p := filepath.Join(dir, customCommandsFile)
	_ = os.Chmod(p, 0)
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	if _, err := loadCustomCommands(); err == nil {
		t.Fatal("unreadable file read")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if _, err := loadCustomCommands(); err == nil {
		t.Fatal("no config dir")
	}
	if !customKeyTaken("docker", " ") || !customKeyTaken("docker", "enter") || !customKeyTaken("docker", "s") || customKeyTaken("docker", "J") || customKeyTaken("docker", "ctrl+j") {
		t.Fatal("customKeyTaken")
	}
	if len(customPaletteItems()) != 2 {
		t.Fatal("palette rows")
	}
}
