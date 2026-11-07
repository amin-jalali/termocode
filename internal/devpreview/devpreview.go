// Package devpreview finds the local dev-server addresses that the user
// or an agent started (in terminal output and in services) and checks
// them, so termocode can list the app being built and open it.
//
// It only ever looks at loopback addresses (localhost, 127.0.0.1, ::1;
// 0.0.0.0 and :: are shown as localhost). A probe is one GET with a short
// timeout that reads the status and the page title; it never follows a
// redirect away from loopback and never uses a proxy from the environment.
package devpreview

import (
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Target is one local address worth previewing.
type Target struct {
	URL  string // normalized, e.g. "http://localhost:5173/"
	Port int
	From string // where it was seen: "terminal tab 2 (web)", "service api"
}

// urlRe finds a loopback http(s) address. The host list is closed on
// purpose: a LAN or public address is not "the app on this machine".
var urlRe = regexp.MustCompile(`(?i)\bhttps?://(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1?\])(?::\d{1,5})?(?:/[^\s"'<>\x60)\]]*)?`)

// ansiRe matches CSI and OSC escape sequences (colors, links, titles).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// StripANSI drops terminal escape sequences, so a colored "Local:
// http://localhost:5173/" still reads as one address.
func StripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// Normalize turns a found address into the form termocode shows and
// opens: lower-case host, 0.0.0.0 and [::] as localhost, no fragment,
// "/" for an empty path, no trailing sentence punctuation. ok is false
// for anything that is not a loopback http(s) address with a valid port.
func Normalize(raw string) (norm string, port int, ok bool) {
	raw = strings.TrimRight(strings.TrimSpace(raw), ".,;:!?")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "", 0, false
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	switch host {
	case "0.0.0.0", "::":
		host = "localhost"
	}
	if !IsLoopbackHost(host) {
		return "", 0, false
	}
	p := u.Port()
	switch {
	case p != "":
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return "", 0, false
		}
		port = n
	case u.Scheme == "https":
		port = 443
	default:
		port = 80
	}
	hostport := host
	if strings.Contains(host, ":") {
		hostport = "[" + host + "]"
	}
	if p != "" {
		hostport += ":" + p
	}
	u.Host = hostport
	u.Fragment, u.RawFragment = "", ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), port, true
}

// IsLoopbackHost says whether host (no port, no brackets) is this
// machine: "localhost" or a loopback IP.
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// FindURLs returns the loopback addresses in text, normalized, each once,
// in the order of their LAST appearance (the newest is last).
func FindURLs(text string) []string {
	raws := urlRe.FindAllString(StripANSI(text), -1)
	seen := map[string]bool{}
	var rev []string
	for i := len(raws) - 1; i >= 0; i-- {
		u, _, ok := Normalize(raws[i])
		if !ok || seen[u] {
			continue
		}
		seen[u] = true
		rev = append(rev, u)
	}
	out := make([]string, 0, len(rev))
	for i := len(rev) - 1; i >= 0; i-- {
		out = append(out, rev[i])
	}
	return out
}

// Merge takes targets from oldest to newest and returns them newest
// first, each URL once (the newest sighting wins). Targets whose URL
// does not normalize are dropped.
func Merge(in []Target) []Target {
	seen := map[string]bool{}
	var out []Target
	for i := len(in) - 1; i >= 0; i-- {
		t := in[i]
		u, port, ok := Normalize(t.URL)
		if !ok || seen[u] {
			continue
		}
		seen[u] = true
		t.URL, t.Port = u, port
		out = append(out, t)
	}
	return out
}

// FromText makes the targets found in text, tagged with from.
func FromText(text, from string) []Target {
	var out []Target
	for _, u := range FindURLs(text) {
		_, port, _ := Normalize(u)
		out = append(out, Target{URL: u, Port: port, From: from})
	}
	return out
}

// FromPort makes the target for a published local port.
func FromPort(port int, from string) (Target, bool) {
	if port < 1 || port > 65535 {
		return Target{}, false
	}
	return Target{URL: "http://localhost:" + strconv.Itoa(port) + "/", Port: port, From: from}, true
}
