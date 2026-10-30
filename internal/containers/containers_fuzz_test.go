package containers

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// addFixtures seeds f with every engine fixture under testdata.
func addFixtures(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join("testdata", "*", "*.json"))
	files = append(files, filepath.Join("testdata", "registry-config.json"))
	for _, p := range files {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(b)
		}
	}
	f.Add([]byte("{\"ID\":\"x\"}\n\nnot json\n"))
	f.Add([]byte("[]"))
	f.Add([]byte(""))
}

// FuzzEngineJSON feeds any bytes to every docker/podman output parser.
// None may panic.
func FuzzEngineJSON(f *testing.F) {
	addFixtures(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseContainers(data)
		_, _ = ParseImages(data)
		_, _ = ParseNetworks(data)
		_, _ = ParseVolumes(data)
		_, _ = ParseContexts(data)
		_, _ = ParseEvents(data)
		_, _ = ParseHistory(data)
		_, _ = ParseVersion(data)
		_, _ = ParseInspect(data)
		_, _ = ParseSearch(data)
		_, _ = ParseConnections(data)
		stats, err := ParseStats(data)
		if err != nil {
			return
		}
		for _, s := range stats {
			_ = SampleOf(s)
		}
	})
}

// FuzzRegistryConfig reads any bytes as ~/.docker/config.json: no panic
// and no auth value ever comes back.
func FuzzRegistryConfig(f *testing.F) {
	addFixtures(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := DecodeRegistryConfig(bytes.NewReader(data))
		if err != nil {
			return
		}
		got := fmt.Sprintf("%+v", cfg)
		for _, secret := range []string{"c2VjcmV0LW5ldmVyLXJldHVybg==", "another-secret-value"} {
			if strings.Contains(string(data), secret) && strings.Contains(got, secret) {
				t.Fatalf("auth value %q came back: %s", secret, got)
			}
		}
	})
}

// FuzzStatNumbers reads any stats cell. A number that is accepted is
// finite and not negative.
func FuzzStatNumbers(f *testing.F) {
	for _, s := range []string{"0.52%", "--", "48.2MiB / 7.7GiB", "1.2kB / 0B", "7.7 GiB", "12.3MB", "1,234 kB", "5", "", "1.2.3MB"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		bad := func(v float64) bool { return math.IsNaN(v) || math.IsInf(v, 0) || v < 0 }
		if v, ok := ParsePercent(s); ok && bad(v) {
			t.Fatalf("ParsePercent(%q) = %v", s, v)
		}
		if v, ok := ParseBytes(s); ok && bad(v) {
			t.Fatalf("ParseBytes(%q) = %v", s, v)
		}
		if a, b, ok := ParsePair(s); ok && (bad(a) || bad(b)) {
			t.Fatalf("ParsePair(%q) = %v, %v", s, a, b)
		}
		_ = ParsePortsText(s)
		_, _ = ParseRemoteRepo(s)
	})
}
