//go:build unix

package webgw

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The control connection (WEB-8): one hub connection that starts with
// {"t":"ctl"} instead of an "open" line. The hub uses it to end the web
// side: a bye with 4009 is the kill switch on the laptop. When it ends
// without a bye the UI process is gone, and the gateway goes with it.
// The hub also sends {"t":"ev","kind":"link","data":{"sha256":"<hex>"}}
// when the user opens a new launch link: the digest of the secret only
// the UI knows (NewLinkSecret).
// It also sends {"t":"ev","kind":"audit","data":{…}} (webproto.AuditNote)
// for every web write op, for the gateway's audit log.
// The hub counts the browsers itself (one hub connection each), so the
// gateway reports nothing on it.

// The gateway also asks the laptop on it (v2): a req with a rising id
// (web.pair_ask, web.grant_ask, web.totp_check) whose res or err may come
// much later, when the owner answered a card.

// Ctl is a live control connection.
type Ctl struct {
	nc      net.Conn
	onLink  func([sha256.Size]byte)
	onAudit func(webproto.AuditNote)
	onEvent func(string, json.RawMessage)
	once    sync.Once
	done    chan struct{}
	code    webproto.CloseCode

	wmu     sync.Mutex // one writer at a time
	mu      sync.Mutex
	lastID  uint64
	pending map[uint64]chan ctlReply
}

// CtlHandlers hear what the hub sends on the control connection. Each
// runs on the reader goroutine; any may be nil.
type CtlHandlers struct {
	Link  func([sha256.Size]byte)
	Audit func(webproto.AuditNote)
	// Event hears every other event (pair, revoke, role, …).
	Event func(kind string, data json.RawMessage)
}

// CtlError is the hub's err answer to a Call.
type CtlError struct {
	Code webproto.ErrCode
	Msg  string
}

func (e *CtlError) Error() string { return string(e.Code) + ": " + e.Msg }

// ErrCtlGone: the control connection ended before the answer came.
var ErrCtlGone = errors.New("termocode went away")

type ctlReply struct {
	data json.RawMessage
	err  error
}

// ctlWriteWait bounds one write on the control connection.
const ctlWriteWait = 5 * time.Second

// EvLink is the event kind of a new launch link.
const EvLink = "link"

// DialCtl opens the control connection; onLink hears each new launch
// link's digest and onAudit each op summary (both on the reader
// goroutine; either may be nil). The error is for the gateway's start:
// without the hub there is nothing to serve.
func DialCtl(ctx context.Context, dial func(context.Context) (net.Conn, error), onLink func([sha256.Size]byte), onAudit func(webproto.AuditNote)) (*Ctl, error) {
	return DialCtlWith(ctx, dial, CtlHandlers{Link: onLink, Audit: onAudit})
}

// DialCtlWith is DialCtl with every handler.
func DialCtlWith(ctx context.Context, dial func(context.Context) (net.Conn, error), h CtlHandlers) (*Ctl, error) {
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
	c := &Ctl{nc: nc, onLink: h.Link, onAudit: h.Audit, onEvent: h.Event, done: make(chan struct{}),
		pending: map[uint64]chan ctlReply{}}
	// A panic in the reader ends the link like a lost hub.
	crash.GoWith("web gateway: the control link", c.read, func() { c.finish(webproto.CloseHubGone) })
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
		c.mu.Lock()
		for id, ch := range c.pending {
			ch <- ctlReply{err: ErrCtlGone}
			delete(c.pending, id)
		}
		c.mu.Unlock()
	})
}

// Call asks the hub op with args and waits for the answer: the res data,
// or a *CtlError, or ErrCtlGone / ctx's error.
func (c *Ctl) Call(ctx context.Context, op string, args any) (json.RawMessage, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	ch := make(chan ctlReply, 1)
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return nil, ErrCtlGone
	default:
	}
	c.lastID++
	id := c.lastID
	c.pending[id] = ch
	c.mu.Unlock()
	line, err := webproto.Encode(&webproto.Req{T: webproto.KindReq, ID: id, Op: op, Args: raw})
	if err == nil {
		c.wmu.Lock()
		_ = c.nc.SetWriteDeadline(time.Now().Add(ctlWriteWait))
		_, err = c.nc.Write(append(line, '\n'))
		c.wmu.Unlock()
	}
	if err != nil {
		c.drop(id)
		return nil, ErrCtlGone
	}
	select {
	case r := <-ch:
		return r.data, r.err
	case <-ctx.Done():
		c.drop(id)
		return nil, ctx.Err()
	}
}

func (c *Ctl) drop(id uint64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// answer hands a res or err to its waiting Call.
func (c *Ctl) answer(id uint64, r ctlReply) {
	c.mu.Lock()
	ch, ok := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ok {
		ch <- r
	}
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
			ID   uint64          `json:"id"`
			Code json.RawMessage `json:"code"`
			Kind string          `json:"kind"`
			Data json.RawMessage `json:"data"`
			Msg  string          `json:"msg"`
		}
		if json.Unmarshal(line, &head) != nil {
			continue
		}
		switch {
		case head.T == webproto.KindRes && head.ID != 0:
			c.answer(head.ID, ctlReply{data: head.Data})
			continue
		case head.T == webproto.KindErr && head.ID != 0:
			var code webproto.ErrCode
			_ = json.Unmarshal(head.Code, &code)
			c.answer(head.ID, ctlReply{err: &CtlError{Code: code, Msg: head.Msg}})
			continue
		}
		if head.T == webproto.KindEv && head.Kind == EvLink {
			var data struct {
				SHA256 string `json:"sha256"`
			}
			_ = json.Unmarshal(head.Data, &data)
			if d, err := hex.DecodeString(data.SHA256); err == nil && len(d) == sha256.Size && c.onLink != nil {
				c.onLink([sha256.Size]byte(d))
			}
			continue
		}
		if head.T == webproto.KindEv && head.Kind == webproto.EvAudit {
			var n webproto.AuditNote
			if json.Unmarshal(head.Data, &n) == nil && n.Op != "" && c.onAudit != nil {
				c.onAudit(n)
			}
			continue
		}
		if head.T == webproto.KindEv {
			if c.onEvent != nil {
				c.onEvent(head.Kind, head.Data)
			}
			continue
		}
		if head.T != webproto.KindBye {
			continue // pongs and anything newer
		}
		var code webproto.CloseCode
		if json.Unmarshal(head.Code, &code) != nil || code.Reason() == "" {
			code = webproto.CloseStopped
		}
		c.finish(code)
		return
	}
}
