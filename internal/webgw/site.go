//go:build unix

package webgw

import "strconv"

// Site is the one origin the gateway serves: the scheme and the exact Host
// header values a browser may send. Everything else is another site.
type Site struct {
	// Scheme is "http" in v1 (loopback, SSH forward); "https" later.
	Scheme string
	// Hosts are the allowed Host header values, port included.
	Hosts []string
}

// LoopbackSite is the site of a gateway the browser reaches on
// 127.0.0.1:port, directly or through an SSH forward.
func LoopbackSite(port int) Site {
	p := strconv.Itoa(port)
	return Site{Scheme: "http", Hosts: []string{"127.0.0.1:" + p, "localhost:" + p}}
}

// hasHost reports whether host is one of the allowed Host values. The
// match is exact: browsers send the host in lower case, and nothing else
// needs to be let in.
func (s Site) hasHost(host string) bool {
	for _, h := range s.Hosts {
		if host == h {
			return true
		}
	}
	return false
}

// Installable reports whether the page may be installed as an app
// (WEB-35): only on an https site, whose origin stays the same between
// starts (the tailnet). A loopback origin changes every start.
func (s Site) Installable() bool { return s.Scheme == "https" }

// wsScheme is the WebSocket scheme that matches the page's scheme.
func (s Site) wsScheme() string {
	if s.Scheme == "https" {
		return "wss"
	}
	return "ws"
}
