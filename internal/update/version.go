package update

import (
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// version is a parsed vMAJOR.MINOR.PATCH[-pre].
type version struct {
	nums [3]int
	pre  []string // dot parts of the pre-release, nil for a full release
}

// pseudo matches the tail of a Go pseudo-version (time stamp and commit),
// which `go install …@main` records: such a build is not a release.
var pseudo = lazyre.New(`\d{14}-[0-9a-f]{12}$`)

// parseVersion reads v; ok is false for "dev", "" and anything else that
// is not a release version.
func parseVersion(v string) (version, bool) {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if v == "" || pseudo.MatchString(v) {
		return version{}, false
	}
	core, pre, hasPre := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) > 3 {
		return version{}, false
	}
	var out version
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return version{}, false
		}
		out.nums[i] = n
	}
	if hasPre {
		if pre == "" {
			return version{}, false
		}
		out.pre = strings.Split(pre, ".")
	}
	return out, true
}

// IsRelease reports whether v is a release version (not a dev, local or
// go install pseudo-version build). Only release builds look for updates.
func IsRelease(v string) bool {
	_, ok := parseVersion(v)
	return ok
}

// Newer reports whether latest is a newer release than current. A current
// that is not a release (dev builds) never has a newer one.
func Newer(current, latest string) bool {
	c, ok1 := parseVersion(current)
	l, ok2 := parseVersion(latest)
	return ok1 && ok2 && compare(l, c) > 0
}

// compare orders versions like semver: numbers first, then a full
// release above its pre-releases, then the pre-release parts.
func compare(a, b version) int {
	for i := range a.nums {
		if a.nums[i] != b.nums[i] {
			return sign(a.nums[i] - b.nums[i])
		}
	}
	switch {
	case a.pre == nil && b.pre == nil:
		return 0
	case a.pre == nil:
		return 1
	case b.pre == nil:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		if c := comparePart(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	return sign(len(a.pre) - len(b.pre))
}

// comparePart compares two pre-release parts: numbers by value and below
// words, words as text.
func comparePart(a, b string) int {
	numeric := func(s string) bool {
		if s == "" {
			return false
		}
		for _, c := range s {
			if c < '0' || c > '9' {
				return false
			}
		}
		return true
	}
	numA, numB := numeric(a), numeric(b)
	switch {
	case numA && numB:
		// Compare digit strings without an integer size limit.
		a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
		if len(a) != len(b) {
			return sign(len(a) - len(b))
		}
	case numA:
		return -1
	case numB:
		return 1
	}
	return strings.Compare(a, b)
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}
