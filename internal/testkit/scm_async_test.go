package testkit

import (
	"net"
	"strings"
	"testing"
)

// Spec: Given a remote that accepts the connection and never answers, when
// the user pushes, then the app does not freeze: the step returns at once,
// a "Pushing…" note shows, and the panel keeps working (keys still move and
// stage).
func TestSCMPushToSilentRemoteKeepsUIResponsive(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	conns := make(chan net.Conn, 64)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns <- c // hold it open, say nothing
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		for {
			select {
			case c := <-conns:
				c.Close()
			default:
				return
			}
		}
	})
	d, r := scmDriver(t, func(r *repo) {
		userAllowsPrompts(t)
		r.git("remote", "add", "origin", "http://"+ln.Addr().String()+"/repo.git")
		r.git("config", "branch.main.remote", "origin")
		r.git("config", "branch.main.merge", "refs/heads/main")
		r.write("main.go", "package main\n// edit\n")
	})
	d.Palette("git-push")
	if !strings.Contains(text(d), "Pushing") {
		t.Errorf("no Pushing… note while the push runs:%s", dump(d))
	}
	scmClickText(d, "main.go")
	d.Key("s")
	until(d, func() bool { return inSection(d, "STAGED", "main.go") })
	if got := r.porcelain(); got != "M  main.go" {
		t.Errorf("panel did not work during the push: git status %q", got)
	}
}
