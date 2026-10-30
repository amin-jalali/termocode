//go:build unix

package webgw

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"sync"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The control connection (WEB-8): one hub connection that starts with
// {"t":"ctl"} instead of an "open" line. The hub uses it to end the web
// side: a bye with 4009 is the kill switch on the laptop. When it ends
// without a bye the UI process is gone, and the gateway goes with it.
// The hub counts the browsers itself (one hub connection each), so the
// gateway reports nothing on it.

// Ctl is a live control connection.
type Ctl struct {
	nc   net.Conn
	once sync.Once
	done chan struct{}
	code webproto.CloseCode
}

// DialCtl opens the control connection. The error is for the gateway's
// start: without the hub there is nothing to serve.
func DialCtl(ctx context.Context, dial func(context.Context) (net.Conn, error)) (*Ctl, error) {
	nc, err := dial(ctx)
	if err != nil {
		return nil, err
	}
	first, err := webproto.Encode(&webproto.Ctl{T: webproto.KindCtl})
	if err == nil {
		_, err = nc.Write(append(first, '\n'))
	}
	if err != nil {
		_ = nc.Close()
		return nil, err
	}
	c := &Ctl{nc: nc, done: make(chan struct{})}
	go c.read()
	return c, nil
}

// Done is closed when the hub ended the web side; Code says how.
func (c *Ctl) Done() <-chan struct{} { return c.done }

// Code is why the control connection ended: the code of the hub's bye
// (4009 for the kill switch), or 4012 when the hub went away. Valid
// after Done.
func (c *Ctl) Code() webproto.CloseCode { return c.code }

// Close ends the connection from the gateway's side.
func (c *Ctl) Close() { c.finish(webproto.CloseNormal) }

func (c *Ctl) finish(code webproto.CloseCode) {
	c.once.Do(func() {
		c.code = code
		_ = c.nc.Close()
		close(c.done)
	})
}

func (c *Ctl) read() {
	br := bufio.NewReaderSize(c.nc, 4<<10)
	for {
		line, err := readLine(br, 64<<10)
		if err != nil {
			c.finish(webproto.CloseHubGone)
			return
		}
		var head struct {
			T    string          `json:"t"`
			Code json.RawMessage `json:"code"`
		}
		if json.Unmarshal(line, &head) != nil || head.T != webproto.KindBye {
			continue // pongs and anything newer: not for v1
		}
		var code webproto.CloseCode
		if json.Unmarshal(head.Code, &code) != nil || code.Reason() == "" {
			code = webproto.CloseStopped
		}
		c.finish(code)
		return
	}
}
