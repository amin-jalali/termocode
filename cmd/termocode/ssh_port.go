//go:build unix

package main

import (
	"fmt"
	"strconv"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/session"
)

// Port preview (WEB-54): the far UI asked, after the user's yes on its
// card, to see one port of the far host's loopback in the browser here.
// One more ssh with the user's own arguments forwards a free 127.0.0.1
// port of this computer to 127.0.0.1:<port> there; the browser opens
// http://127.0.0.1:<free>/, its own origin, never the web panel's. Only a
// session whose web panel was handed off (termocode ssh --web) takes
// requests, at most maxPortFwds at a time; every forward ends with
// termocode ssh.

// maxPortFwds bounds the open port previews of one termocode ssh.
const maxPortFwds = 8

// msgPortFwdFailed is said when a port preview's ssh could not forward.
const msgPortFwdFailed = "termocode: SSH could not forward port %d for the preview. The server may block it " +
	"(AllowTcpForwarding), or nothing listens on that port there."

// portFwd is one running port preview.
type portFwd struct {
	proc  webFwdProc
	local int
	port  int
}

// handlePort opens or closes one port preview.
func (f *webForward) handlePort(r session.PortForward) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	if !r.Open {
		if p := f.ports[r.ID]; p != nil {
			p.proc.Kill()
			delete(f.ports, r.ID)
		}
		return
	}
	switch {
	case f.key == "":
		return // no web panel was handed off: nobody here asked for this
	case f.ports[r.ID] != nil:
		return // already open: the far UI asked twice
	case len(f.ports) >= maxPortFwds:
		f.notes = append(f.notes, "termocode: too many port previews at once; stop one in Web: Forwarded Ports…")
		return
	}
	local, err := f.free()
	if err != nil {
		f.notes = append(f.notes, "termocode: no port preview: "+err.Error())
		return
	}
	proc, err := f.start(f.ssh, session.SSHPortForwardArgv(f.args, local, r.Port))
	if err != nil {
		f.notes = append(f.notes, "termocode: no port preview: "+err.Error())
		return
	}
	p := &portFwd{proc: proc, local: local, port: r.Port}
	f.ports[r.ID] = p
	id := r.ID
	crash.Go("termocode ssh --web: a port preview", func() {
		if !f.ready(local, webFwdReady) {
			f.mu.Lock()
			if f.ports[id] == p {
				p.proc.Kill()
				delete(f.ports, id)
				f.notes = append(f.notes, fmt.Sprintf(msgPortFwdFailed, p.port))
			}
			f.mu.Unlock()
			return
		}
		f.mu.Lock()
		live := !f.closed && f.ports[id] == p
		f.mu.Unlock()
		if !live {
			return
		}
		if err := f.open(portPreviewURL(local)); err != nil {
			f.note("termocode: could not open a browser here. Open " + portPreviewURL(local) + " in this computer's browser.")
		}
	})
	crash.Go("termocode ssh --web: a port preview ends", func() {
		_ = proc.Wait()
		f.mu.Lock()
		if f.ports[id] == p {
			delete(f.ports, id)
		}
		f.mu.Unlock()
	})
}

// portPreviewURL is the browser's address of a port preview here.
func portPreviewURL(local int) string {
	return "http://127.0.0.1:" + strconv.Itoa(local) + "/"
}
