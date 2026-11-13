package debounce

import "testing"

// A nil Debouncer is safe: it never drops a call, counts nothing, and
// Cancel does nothing.
func TestNilDebouncerIsSafe(t *testing.T) {
	var d *Debouncer
	d.Cancel("k")
	if !d.Current("k", 7) {
		t.Fatal("a nil debouncer must treat every call as current")
	}
	if d.Next("k") != 0 {
		t.Fatal("a nil debouncer has no generations")
	}
}
