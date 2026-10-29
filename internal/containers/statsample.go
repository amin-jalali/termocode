package containers

import (
	"strconv"
	"strings"
)

// ParsePercent reads "12.34%". ok is false for "--", "" or garbage.
func ParsePercent(s string) (float64, bool) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	if s == "" || s == "--" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

// ParseBytes reads one engine size such as "48.2MiB", "1.2kB", "7.7 GiB"
// or "0B" into bytes. Decimal (kB, MB, GB) and binary (KiB, MiB, GiB)
// units are both understood. ok is false for "--", "" or garbage.
func ParseBytes(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "--" {
		return 0, false
	}
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	if i == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, false
	}
	mult, ok := byteUnits[strings.ToLower(strings.TrimSpace(s[i:]))]
	if !ok {
		return 0, false
	}
	return v * mult, true
}

var byteUnits = map[string]float64{
	"": 1, "b": 1,
	"kb": 1e3, "mb": 1e6, "gb": 1e9, "tb": 1e12, "pb": 1e15,
	"k": 1024, "m": 1024 * 1024, "g": 1 << 30,
	"kib": 1 << 10, "mib": 1 << 20, "gib": 1 << 30, "tib": 1 << 40, "pib": 1 << 50,
}

// ParsePair reads "a / b" (MemUsage, NetIO, BlockIO) into two sizes.
func ParsePair(s string) (a, b float64, ok bool) {
	l, r, found := strings.Cut(s, "/")
	if !found {
		return 0, 0, false
	}
	a, ok1 := ParseBytes(l)
	b, ok2 := ParseBytes(r)
	return a, b, ok1 && ok2
}
