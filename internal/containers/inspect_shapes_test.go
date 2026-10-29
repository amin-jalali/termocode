package containers

import (
	"context"
	"reflect"
	"testing"

	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// Inspect JSON differs between engines and versions (podman, old docker,
// odd images). Masking must hold for every shape, not only the usual one.
func TestMaskInspectOddShapes(t *testing.T) {
	in := map[string]any{
		"Config": map[string]any{
			"Env":    map[string]any{"DB_PASSWORD": "hunter2"}, // a map, not a list
			"Labels": []any{"api_token=abc123"},                // a list, not a map
		},
		"HostConfig": map[string]any{
			"Env":    []any{"A=1", 42, map[string]any{"password": "pw"}},
			"Labels": map[string]any{"nested": map[string]any{"password": "pw2"}, "count": 3.0},
		},
		"AuthConfig": map[string]any{"password": "pw3"}, // a secret field that is an object
		"Size":       1234.0,
		"Running":    true,
	}
	out, n := MaskInspect(in)
	want := map[string]any{
		"Config": map[string]any{
			"Env":    map[string]any{"DB_PASSWORD": redacted},
			"Labels": []any{"api_token=" + redacted},
		},
		"HostConfig": map[string]any{
			"Env":    []any{"A=" + redacted, 42, map[string]any{"password": redacted}},
			"Labels": map[string]any{"nested": map[string]any{"password": redacted}, "count": 3.0},
		},
		"AuthConfig": map[string]any{"password": redacted},
		"Size":       1234.0,
		"Running":    true,
	}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("masked = %#v", out)
	}
	if n != 6 {
		t.Fatalf("masked %d values, want 6", n)
	}
}

// A search that fails with nothing on stderr names the exit code.
func TestSearchSilentFailure(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "search.err", "")
	if _, err := fakeEngine(f).Search(context.Background(), "nginx", 5); err == nil || err.Error() != "search exited 1" {
		t.Fatalf("err = %v", err)
	}
}

// A podman that prints a broken connection list is an error, not "no
// connections".
func TestParseConnectionsBrokenArray(t *testing.T) {
	if _, err := ParseConnections([]byte(`[{"Name":"a"`)); err == nil {
		t.Fatal("broken JSON array accepted")
	}
}
