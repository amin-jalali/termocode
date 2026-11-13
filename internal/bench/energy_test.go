package bench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func put(t *testing.T, root, rel, val string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(val+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBatteryMeterReadsEnergyAndRefusesWhenCharging(t *testing.T) {
	root := t.TempDir()
	put(t, root, "power_supply/BAT0/type", "Battery")
	put(t, root, "power_supply/BAT0/status", "Discharging")
	put(t, root, "power_supply/BAT0/energy_now", "40000000") // 40 Wh in uWh
	m, skip := DetectEnergy(root)
	if m == nil {
		t.Fatalf("skip: %s", skip)
	}
	j, err := m.Joules()
	if err != nil || j < 143000 || j > 145000 { // 40 Wh = 144000 J
		t.Fatalf("joules %v err %v", j, err)
	}
	put(t, root, "power_supply/BAT0/status", "Charging")
	if _, err := m.Joules(); err == nil || !strings.Contains(err.Error(), "charging") {
		t.Fatalf("a charging laptop gives no valid reading: %v", err)
	}
}

func TestBatteryMeterUsesChargeTimesVoltage(t *testing.T) {
	root := t.TempDir()
	put(t, root, "power_supply/BAT1/type", "Battery")
	put(t, root, "power_supply/BAT1/status", "Discharging")
	put(t, root, "power_supply/BAT1/charge_now", "3000000")   // 3 Ah in uAh
	put(t, root, "power_supply/BAT1/voltage_now", "12000000") // 12 V in uV
	m, _ := DetectEnergy(root)
	j, err := m.Joules()
	if err != nil || j < 129000 || j > 130000 { // 36 Wh = 129600 J
		t.Fatalf("joules %v err %v", j, err)
	}
}

func TestRAPLMeterHandlesCounterWrap(t *testing.T) {
	root := t.TempDir()
	put(t, root, "powercap/intel-rapl:0/energy_uj", "900")
	put(t, root, "powercap/intel-rapl:0/max_energy_range_uj", "1000")
	m, skip := DetectEnergy(root)
	if m == nil {
		t.Fatalf("skip: %s", skip)
	}
	if m.Name() != "rapl" {
		t.Fatal(m.Name())
	}
	a, _ := m.Joules()
	put(t, root, "powercap/intel-rapl:0/energy_uj", "100") // wrapped
	b, _ := m.Joules()
	if d := b - a; d < 0.00019 || d > 0.00021 { // 200 uJ forward
		t.Fatalf("a wrap must count forward: %v", d)
	}
}

func TestDetectEnergyWithNothingSaysWhy(t *testing.T) {
	m, skip := DetectEnergy(t.TempDir())
	if m != nil || !strings.Contains(skip, "battery") {
		t.Fatalf("%v %q", m, skip)
	}
}

func TestMeasureEnergyGivesJoulesAndWatts(t *testing.T) {
	m := &fakeMeter{step: 5}
	r, err := MeasureEnergy(m, 100*time.Millisecond, func(stop <-chan struct{}) { <-stop })
	if err != nil {
		t.Fatal(err)
	}
	if r.Joules != 5 || r.Watts < 40 || r.Watts > 60 {
		t.Fatalf("%+v", r)
	}
}

type fakeMeter struct {
	step float64
	n    float64
}

func (f *fakeMeter) Name() string { return "fake" }
func (f *fakeMeter) Joules() (float64, error) {
	v := f.n
	f.n += f.step
	return v, nil
}
