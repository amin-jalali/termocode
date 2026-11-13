package dbclient

import "testing"

// A default with no value is refused, not kept as part of the type.
func TestDesignColumnDefaultNeedsAValue(t *testing.T) {
	for _, spec := range []string{"table t\ncolumn c int default", "add column c int default"} {
		if _, _, _, err := ParseDesignSpec(spec); err == nil {
			t.Errorf("%q: want an error for a default without a value", spec)
		}
	}
}
