package update

import "testing"

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		cur, latest string
		want        bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.10.0", "v0.9.9", false},
		{"v0.9.9", "v0.10.0", true},
		{"v1.0.0-rc1", "v1.0.0", true},
		{"v1.0.0", "v1.0.0-rc2", false},
		{"v1.0.0-rc1", "v1.0.0-rc2", true},
		{"v1.2", "v1.2.1", true},
		{"0.1.0", "v0.2.0", true},
		{"v0.3.0", "v0.2.0", false},
		// Dev and unknown builds never see an update.
		{"dev", "v9.0.0", false},
		{"", "v9.0.0", false},
		{"v0.1.1-0.20260101120000-abcdef123456", "v9.0.0", false}, // go install pseudo-version
		{"v0.1.0", "garbage", false},
	} {
		if got := Newer(c.cur, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.cur, c.latest, got, c.want)
		}
	}
}

func TestIsRelease(t *testing.T) {
	for v, want := range map[string]bool{
		"v0.2.0": true, "v1.0.0-rc1": true, "dev": false, "": false, "(devel)": false,
		"v0.0.0-20260101000000-abcdef123456": false,
	} {
		if got := IsRelease(v); got != want {
			t.Errorf("IsRelease(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestPrereleaseNumbersAndHyphens(t *testing.T) {
	for _, pair := range [][2]string{
		{"5", "-5"},
		{"9223372036854775807", "-5"},
		{"999999999999999999999999999999", "a"},
		{"999999999999999999999999999999", "1000000000000000000000000000000"},
	} {
		a, b := "v1.0.0-a."+pair[0], "v1.0.0-a."+pair[1]
		if !Newer(a, b) || Newer(b, a) {
			t.Errorf("expected %s < %s", a, b)
		}
	}
}
