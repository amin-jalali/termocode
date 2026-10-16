package services

import (
	"net"
	"net/http"
	"strconv"
	"time"
)

// ProbeTCP reports whether host:port accepts a connection.
func ProbeTCP(host string, port int, timeout time.Duration) bool {
	if port <= 0 {
		return false
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// ProbeHTTP reports whether GET url answers 2xx or 3xx in time.
func ProbeHTTP(url string, timeout time.Duration) bool {
	cl := http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := cl.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// ProbeService probes a running service that has no Docker healthcheck:
// the first published TCP port (compose) or the configured probe
// (local). nil = not probed.
func ProbeService(s Service, st Status, timeout time.Duration) *bool {
	if !st.State.Up() || s.HasHealthcheck {
		return nil
	}
	var ok bool
	switch {
	case s.Health.HTTP != "":
		ok = ProbeHTTP(s.Health.HTTP, timeout)
	case s.Health.TCP > 0:
		ok = ProbeTCP("", s.Health.TCP, timeout)
	default:
		var p *Port
		for i := range st.Ports {
			if st.Ports[i].Proto == "" || st.Ports[i].Proto == "tcp" {
				p = &st.Ports[i]
				break
			}
		}
		if p == nil {
			return nil
		}
		ok = ProbeTCP(p.Host, p.Published, timeout)
	}
	return &ok
}
