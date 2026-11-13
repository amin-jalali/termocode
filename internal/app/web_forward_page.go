package app

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Port preview from the page (WEB-54): a click on a localhost link asks
// web.forward with the port only. It is the laptop's Web: Forward Port…
// card, word for word (the page approves it, or the laptop with
// web_approve_on_laptop); after the yes the page that asked gets
// fwd.ready with the address the hub built. With the panel on this
// computer nothing is forwarded: the page's own link sheet was the
// confirm, and fwd.ready comes at once. A port forwarded already is not
// asked again: the page gets its address.

func init() {
	webAskOps[webproto.OpWebForward] = (*Model).webForwardAsk
}

// webForwardAsk checks a page's web.forward and returns its card (nil:
// answered here, refused or at once).
func (m *Model) webForwardAsk(call *webCall) *webAsk {
	port := call.args.(*webproto.WebForwardArgs).Port
	st := m.webPanel
	if !webSettings().On || st == nil || !st.up {
		call.fail(webproto.CodeConflict, "The web panel is stopping.")
		return nil
	}
	have, why := m.webForwardCheck(port)
	if why != "" {
		call.fail(webproto.CodeBadRequest, why)
		return nil
	}
	switch {
	case have != nil && have.mode == "ssh":
		call.fail(webproto.CodeConflict, "Port "+strconv.Itoa(port)+" is forwarded already: see the tab termocode ssh opened.")
		return nil
	case have != nil:
		webForwardReadySend(call.conn, have)
		call.reply(webproto.AskRef{})
		return nil
	case st.sshPort == 0 && st.url == "": // local: nothing to forward
		f, _ := m.webForwardStart(port, "")
		webForwardReadySend(call.conn, f)
		call.reply(webproto.AskRef{})
		return nil
	}
	title, body := m.webForwardCard(port)
	c := call.conn
	return &webAsk{
		summary: "port " + strconv.Itoa(port),
		title:   title,
		body:    body,
		action:  "Open port",
		run: func(m *Model) tea.Cmd {
			f, why := m.webForwardStart(port, "confirmed_web")
			if why != "" {
				return m.webToast(toast.Warn, why)
			}
			webForwardReadySend(c, f)
			return nil
		},
	}
}

// webForwardReadySend tells page c that f is open, with its address.
func webForwardReadySend(c *webConn, f *webFwd) {
	if c == nil || f == nil || c.ctx.Err() != nil {
		return
	}
	r := webproto.FwdReady{Port: f.port, Mode: f.mode}
	if f.mode != "ssh" {
		r.URL = f.url
	}
	data, err := webData(r)
	if err != nil {
		recordError("[web] fwd.ready: " + err.Error())
		return
	}
	c.send(&webproto.Event{T: webproto.KindEv, Kind: webproto.EventFwdReady, Data: data})
}
