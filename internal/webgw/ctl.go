//go:build unix

package webgw

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"sync"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The control connection (WEB-8): one hub connection that starts with
// {"t":"ctl"} instead of an "open" line. The hub uses it to end the web
// side: a bye with 4009 is the kill switch on the laptop. When it ends
// without a bye the UI process is gone, and the gateway goes with it.
// The hub also sends {"t":"ev","kind":"link","data":{"sha256":"<hex>"}}
// when the user opens a new launch link: the digest of the secret only
// the UI knows (NewLinkSecret).
// The hub counts the browsers itself (one hub connection each), so the
// gateway reports nothing on it.

// Ctl is a live control connection.
type Ctl struct {
	nc     net.Conn
	onLink func([sha256.Size]byte)
	once   sync.Once
	done   chan struct{}
	code   webproto.CloseCode
}

// EvLink is the event kind of a new launch link.
const EvLink = "link"

// DialCtl opens the control connection; onLink hears each new launch
// link's digest (on the reader goroutine). The error is for the
// gateway's start: without the hub there is nothing to serve.
func DialCtl(ctx context.Context, dial func(context.Context) (net.Conn, error), onLink func([sha256.Size]byte)) (*Ctl, error) {
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
	c := &Ctl{nc: nc, onLink: onLink, done: make(chan struct{})}
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
			Kind string          `json:"kind"`
			Data struct {
				SHA256 string `json:"sha256"`
			} `json:"data"`
		}
		if json.Unmarshal(line, &head) != nil {
			continue
		}
		if head.T == webproto.KindEv && head.Kind == EvLink {
			if d, err := hex.DecodeString(head.Data.SHA256); err == nil && len(d) == sha256.Size && c.onLink != nil {
				c.onLink([sha256.Size]byte(d))
			}
			continue
		}
		if head.T != webproto.KindBye {
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
