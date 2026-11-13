package bench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// EnergyMeter reads an energy counter in joules. Battery meters count what
// is left (it falls); RAPL counts what was used (it rises).
type EnergyMeter interface {
	Name() string
	Joules() (float64, error)
}

func readNum(p string) (float64, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
}

type batteryMeter struct{ dir string }

func (b *batteryMeter) Name() string { return "battery" }
func (b *batteryMeter) Joules() (float64, error) {
	st, _ := os.ReadFile(filepath.Join(b.dir, "status"))
	if s := strings.TrimSpace(string(st)); s != "Discharging" {
		return 0, fmt.Errorf("battery is %s, not discharging: unplug the charger (a charging laptop gives no valid reading)", strings.ToLower(s))
	}
	if uwh, err := readNum(filepath.Join(b.dir, "energy_now")); err == nil {
		return uwh * 3600 / 1e6, nil
	}
	uah, err1 := readNum(filepath.Join(b.dir, "charge_now"))
	uv, err2 := readNum(filepath.Join(b.dir, "voltage_now"))
	if err1 != nil || err2 != nil {
		return 0, errors.New("battery has neither energy_now nor charge_now and voltage_now")
	}
	return uah * uv / 1e12 * 3600, nil
}

type raplMeter struct {
	mu   sync.Mutex
	path string
	max  float64
	last float64
	acc  float64
	init bool
}

func (r *raplMeter) Name() string { return "rapl" }
func (r *raplMeter) Joules() (float64, error) {
	uj, err := readNum(r.path)
	if err != nil {
		return 0, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.init {
		r.init, r.acc = true, uj
	} else if uj >= r.last {
		r.acc += uj - r.last
	} else { // the counter wrapped
		r.acc += r.max - r.last + uj
	}
	r.last = uj
	return r.acc / 1e6, nil
}

// DetectEnergy finds a meter under root (/sys in real use): a battery first,
// then the CPU package RAPL counter. If none works it says why.
func DetectEnergy(root string) (EnergyMeter, string) {
	bats, _ := filepath.Glob(filepath.Join(root, "power_supply", "BAT*"))
	for _, d := range bats {
		if t, _ := os.ReadFile(filepath.Join(d, "type")); strings.TrimSpace(string(t)) == "Battery" {
			return &batteryMeter{dir: d}, ""
		}
	}
	p := filepath.Join(root, "powercap", "intel-rapl:0")
	if _, err := os.ReadFile(filepath.Join(p, "energy_uj")); err == nil {
		max, err := readNum(filepath.Join(p, "max_energy_range_uj"))
		if err != nil || max <= 0 {
			max = 1 << 40
		}
		return &raplMeter{path: filepath.Join(p, "energy_uj"), max: max}, ""
	}
	return nil, "energy measure skipped: no battery (needs a laptop, unplugged) and no readable RAPL counter (/sys/class/powercap, often root only)"
}

// EnergyResult is the energy used while a workload ran.
type EnergyResult struct {
	Meter   string
	Seconds float64
	Joules  float64
	Watts   float64
}

// MeasureEnergy runs workload for d (it must return after stop closes) and
// returns the energy used in that time.
func MeasureEnergy(m EnergyMeter, d time.Duration, workload func(stop <-chan struct{})) (EnergyResult, error) {
	start, err := m.Joules()
	if err != nil {
		return EnergyResult{}, err
	}
	t0 := time.Now()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() { workload(stop) })
	time.Sleep(d)
	close(stop)
	wg.Wait()
	end, err := m.Joules()
	if err != nil {
		return EnergyResult{}, err
	}
	el := time.Since(t0).Seconds()
	used := end - start
	if m.Name() == "battery" {
		used = start - end
	}
	if used < 0 {
		return EnergyResult{}, errors.New("energy counter went the wrong way")
	}
	return EnergyResult{Meter: m.Name(), Seconds: el, Joules: used, Watts: used / el}, nil
}
