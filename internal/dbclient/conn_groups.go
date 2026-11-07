package dbclient

import (
	"sort"
	"strings"
	"time"
)

var connColors = []string{"red", "orange", "yellow", "green", "blue", "purple", "gray"}

// ConnColors are the colours a connection may carry (DBX-21).
func ConnColors() []string { return append([]string(nil), connColors...) }

// normFolderColor cleans c.Folder ("/a//b/" → "a/b") and keeps c.Color
// only when it is one of ConnColors (any case); an unknown colour is
// dropped with a warning.
func normFolderColor(c *Conn) {
	var parts []string
	for _, p := range strings.Split(c.Folder, "/") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	c.Folder = strings.Join(parts, "/")
	if c.Color == "" {
		return
	}
	col := strings.ToLower(strings.TrimSpace(c.Color))
	for _, k := range connColors {
		if k == col {
			c.Color = col
			return
		}
	}
	c.Color = ""
	if c.Warning == "" {
		c.Warning = "unknown colour; use one of " + strings.Join(connColors, ", ")
	}
}

// Folders are the folders of cs, sorted, each once.
func Folders(cs []Conn) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range cs {
		if c.Folder != "" && !seen[c.Folder] {
			seen[c.Folder] = true
			out = append(out, c.Folder)
		}
	}
	sort.Strings(out)
	return out
}

// ReconnectDelay is the wait before auto-reconnect attempt n (0-based):
// 1 s, doubling, at most 30 s.
func ReconnectDelay(n int) time.Duration {
	if n < 0 {
		n = 0
	}
	if n > 5 {
		return 30 * time.Second
	}
	d := time.Second << n
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}
