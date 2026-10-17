package services

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestProbeTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if !ProbeTCP("127.0.0.1", port, 300*time.Millisecond) {
		t.Error("open port reads closed")
	}
	ln.Close()
	if ProbeTCP("127.0.0.1", port, 300*time.Millisecond) {
		t.Error("closed port reads open")
	}
	if ProbeTCP("127.0.0.1", 0, 300*time.Millisecond) {
		t.Error("port 0 must be closed")
	}
}

func TestProbeHTTP(t *testing.T) {
	code := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if code == 0 {
			time.Sleep(2 * time.Second)
		}
		w.WriteHeader(max(code, 200))
	}))
	defer srv.Close()
	if !ProbeHTTP(srv.URL, time.Second) {
		t.Error("200 must be ok")
	}
	code = 302
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	if !ProbeHTTP(srv.URL, time.Second) {
		t.Error("204 must be ok")
	}
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	if ProbeHTTP(srv.URL, time.Second) {
		t.Error("500 must fail")
	}
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(time.Second) })
	if ProbeHTTP(srv.URL, 100*time.Millisecond) {
		t.Error("timeout must fail")
	}
}

func TestProbeService(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	// compose: running, no healthcheck → the published port is probed.
	s := Service{Name: "redis", Backend: "compose"}
	st := Status{State: Running, Ports: []Port{{Host: "0.0.0.0", Published: port, Target: 6379}}}
	if ok := ProbeService(s, st, 300*time.Millisecond); ok == nil || !*ok {
		t.Errorf("open = %v", ok)
	}
	// With a healthcheck docker knows better: no probe.
	s.HasHealthcheck = true
	if ok := ProbeService(s, st, 300*time.Millisecond); ok != nil {
		t.Error("healthcheck services are not probed")
	}
	// Not running: no probe.
	s.HasHealthcheck = false
	st.State = Exited
	if ok := ProbeService(s, st, 300*time.Millisecond); ok != nil {
		t.Error("stopped services are not probed")
	}
	// local: the configured probe.
	l := Service{Name: "web", Backend: "local", Health: Probe{TCP: port}}
	if ok := ProbeService(l, Status{State: Running}, 300*time.Millisecond); ok == nil || !*ok {
		t.Error("local tcp probe")
	}
	_ = strconv.Itoa
}
