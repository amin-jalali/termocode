package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The view registry on the hub (mobile plan 3.11.3, MOB-9): view.run runs
// a spec, view.stop ends a live one, view.catalog lists the views the
// connection may run. Rows come from the view's source as on the wire: a
// topic's snapshot (marshalled like a snap) or the source op's result,
// from that op's own handler, its own policy row and its own audit (the
// call goes through the same handler table with the connection's role).
// webproto.ApplyView alone applies the spec.
//
// A live view (topic-backed, live: true) re-runs when its topic
// publishes, coalesced to at most two runs a second, and sends ev
// view_update only when the block changed. At most MaxLiveViews per
// connection; they end with the connection or view.stop.

func init() {
	webOps[webproto.OpViewRun] = (*Model).webViewRun
	webOps[webproto.OpViewStop] = (*Model).webViewStop
	webOps[webproto.OpViewCatalog] = (*Model).webViewCatalog
}

// webViewCoalesce is the wait between a topic change and the re-run of
// its live views (≤ 2 runs a second); tests lower it.
var webViewCoalesce = 500 * time.Millisecond

// webViewsState is the hub's live views (Update only).
type webViewsState struct {
	seq     uint64
	live    map[string]*webLiveView
	pending bool // a tick is on its way
}

// webLiveView is one live view of one connection.
type webLiveView struct {
	id    string
	conn  *webConn
	def   webproto.ViewDef
	spec  webproto.ViewSpec
	last  []byte // the block last sent, as JSON
	dirty bool
}

// webViewTickMsg re-runs the dirty live views.
type webViewTickMsg struct{ hub *webHub }

func (h *webHub) nextViewID(prefix string) string {
	h.views.seq++
	return prefix + strconv.FormatUint(h.views.seq, 36)
}

// viewsTouch marks the live views of topic dirty and plans one tick
// (Update only; called from publish).
func (h *webHub) viewsTouch(topic string) {
	hit := false
	for _, v := range h.views.live {
		if v.def.Source.Topic == topic {
			v.dirty, hit = true, true
		}
	}
	if !hit || h.views.pending {
		return
	}
	h.views.pending = true
	wait := webViewCoalesce
	crash.Go("web: the live views tick", func() {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-t.C:
			_ = h.send(h.ctx, webViewTickMsg{hub: h})
		case <-h.done:
		}
	})
}

// viewsDrop ends the live views of a connection that went away.
func (h *webHub) viewsDrop(c *webConn) {
	for id, v := range h.views.live {
		if v.conn == c {
			delete(h.views.live, id)
		}
	}
}

// onWebViewTick re-runs each dirty live view (one snapshot per topic) and
// sends ev view_update where the block changed.
func (m *Model) onWebViewTick(msg webViewTickMsg) {
	h := msg.hub
	h.views.pending = false
	snaps := map[string]json.RawMessage{}
	for _, v := range h.views.live {
		if !v.dirty {
			continue
		}
		v.dirty = false
		if !h.live[v.conn] {
			continue
		}
		src, ok := snaps[v.def.Source.Topic]
		if !ok {
			var err error
			if src, err = m.webViewTopicData(v.def.Source.Topic); err != nil {
				recordError("[web] live view " + v.def.ID + ": " + err.Error())
				continue
			}
			snaps[v.def.Source.Topic] = src
		}
		b, err := webViewBlock(v.def, v.spec, src)
		if err != nil {
			continue
		}
		b.ID = v.id
		data, err := webData(webproto.ViewUpdate{ID: v.id, Block: b})
		if err != nil || bytes.Equal(data, v.last) {
			continue
		}
		v.last = data
		v.conn.send(&webproto.Event{T: webproto.KindEv, Kind: webproto.EventViewUpdate, Data: data})
	}
}

// webViewTopicData is a topic's snapshot as on the wire.
func (m *Model) webViewTopicData(topic string) (json.RawMessage, error) {
	def, ok := m.web.topics[topic]
	if !ok {
		return nil, errors.New("topic " + topic + " is not served")
	}
	// A snapshot also tells its publisher "the browsers have this now";
	// a view is not a subscriber, so the publishers' books are put back.
	defer m.web.pubsKeep()()
	snap, err := webSnapshot(m, def, "")
	if err != nil {
		return nil, err
	}
	return webData(snap)
}

// pubsKeep saves the books of the publishers whose topics views read and
// returns the func that puts them back (Update only).
func (h *webHub) pubsKeep() func() {
	git, wt, runs, wfx, tasks, projects, inbox, cards, chats, mcp := h.git, h.wt, h.runs, h.wfx, h.tasks, h.projects, h.inbox, h.cards, h.chats, h.more.mcp
	return func() {
		// A card's id and since are its identity, not a book: keep what the
		// snapshot handed out, or the next build names it again (new
		// since, a live view sent with no change).
		ids, since, diffs, n := h.cards.ids, h.cards.since, h.cards.diffs, h.cards.n
		h.git, h.wt, h.runs, h.wfx, h.tasks, h.projects, h.inbox, h.cards, h.chats, h.more.mcp = git, wt, runs, wfx, tasks, projects, inbox, cards, chats, mcp
		h.cards.ids, h.cards.since, h.cards.diffs, h.cards.n = ids, since, diffs, n
	}
}

// webViewBlock makes the block of a spec from its source's JSON.
func webViewBlock(def webproto.ViewDef, spec webproto.ViewSpec, src json.RawMessage) (webproto.Block, error) {
	rows, whole, err := webproto.ViewRows(def.ID, webproto.Effective(spec, def).Args, src)
	if err != nil {
		return webproto.Block{}, err
	}
	if whole != nil {
		b := *whole
		b.Source, b.WS, b.Spec = def.ID, spec.WS, &spec
		if spec.Title != "" {
			b.Title = spec.Title
		}
		return b, nil
	}
	return webproto.ApplyView(def, spec, rows), nil
}

// webViewMsg is a refusal's text without the package prefix.
func webViewMsg(err error) string {
	s := strings.TrimPrefix(err.Error(), webproto.ErrInvalid.Error()+": ")
	if s == "" {
		return "That view is not valid."
	}
	return strings.ToUpper(s[:1]) + s[1:] + "."
}

// webViewRun runs one spec (view.run).
func (m *Model) webViewRun(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ViewRunArgs)
	def, ok := webproto.LookupView(a.Spec.View)
	if !ok {
		call.fail(webproto.CodeNotFound, "Unknown view "+strconv.Quote(a.Spec.View)+".")
		return nil
	}
	if def.Role == webproto.RoleController && call.conn.role != webproto.RoleController {
		call.fail(webproto.CodeForbidden, "You may not do that from here.")
		return nil
	}
	spec := a.Spec
	if err := webproto.CheckViewSpec(&spec, def); err != nil {
		call.fail(webproto.CodeBadRequest, webViewMsg(err))
		return nil
	}
	h := m.web
	if def.Source.Topic != "" {
		src, err := m.webViewTopicData(def.Source.Topic)
		if err != nil {
			call.fail(webproto.CodeNotFound, "This view is not available yet.")
			return nil
		}
		b, err := webViewBlock(def, spec, src)
		if err != nil {
			call.fail(webproto.CodeNotFound, "This view is not available yet.")
			return nil
		}
		res := webproto.ViewRunResult{}
		if a.Live {
			n := 0
			for _, v := range h.views.live {
				if v.conn == call.conn {
					n++
				}
			}
			if n >= webproto.MaxLiveViews {
				call.fail(webproto.CodeLimit, "At most "+strconv.Itoa(webproto.MaxLiveViews)+" live views at once. Close one first.")
				return nil
			}
			res.Live = h.nextViewID("lv-")
			b.ID = res.Live
			if h.views.live == nil {
				h.views.live = map[string]*webLiveView{}
			}
			lv := &webLiveView{id: res.Live, conn: call.conn, def: def, spec: spec}
			lv.last, _ = webData(webproto.ViewUpdate{ID: lv.id, Block: b})
			h.views.live[lv.id] = lv
			h.ring(def.Source.Topic) // its publisher runs from now on (webListens)
		} else {
			b.ID = h.nextViewID("v-")
		}
		res.Block = b
		call.reply(res)
		return nil
	}
	if def.Source.Op == "" {
		call.fail(webproto.CodeNotFound, "This view is not available yet.")
		return nil
	}
	return m.webViewRunOp(call, def, spec)
}

// webViewRunOp runs the source op through its own handler for the same
// connection, then applies the spec to its result.
func (m *Model) webViewRunOp(call *webCall, def webproto.ViewDef, spec webproto.ViewSpec) tea.Cmd {
	op := def.Source.Op
	p, _ := webproto.Lookup(op)
	if !webproto.Allowed(call.conn.role, op) || p.LaptopCard || p.Gateway {
		call.fail(webproto.CodeForbidden, "You may not do that from here.")
		return nil
	}
	raw, err := json.Marshal(webproto.SourceArgs(def, webproto.Effective(spec, def).Args))
	if err != nil {
		call.fail(webproto.CodeInternal, "Something went wrong.")
		return nil
	}
	args, err := webproto.DecodeArgs(op, raw)
	if err != nil {
		call.fail(webproto.CodeBadRequest, "That view's params are not valid.")
		return nil
	}
	id := m.web.nextViewID("v-")
	inner := &webCall{ctx: call.ctx, conn: call.conn, id: call.id, op: op, args: args}
	inner.sink = func(msg webproto.Msg) {
		switch r := msg.(type) {
		case *webproto.ErrMsg:
			call.fail(r.Code, r.Msg)
		case *webproto.Res:
			b, err := webViewBlock(def, spec, r.Data)
			if err != nil {
				call.fail(webproto.CodeInternal, "Something went wrong.")
				return
			}
			b.ID = id
			call.reply(webproto.ViewRunResult{Block: b})
		}
	}
	call.later()
	cmd, found := m.webRunCall(m.web, inner)
	if !found {
		call.fail(webproto.CodeNotFound, "This view is not available yet.")
		return nil
	}
	if !inner.deferred && !inner.answered.Load() {
		call.fail(webproto.CodeInternal, "Something went wrong.")
	}
	return cmd
}

// webViewStop ends a live view of this connection (view.stop).
func (m *Model) webViewStop(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ViewStopArgs)
	if v := m.web.views.live[a.ID]; v != nil && v.conn == call.conn {
		delete(m.web.views.live, a.ID)
		m.web.pruneResume()
	}
	call.reply(nil)
	return nil
}

// webViewCatalog lists the views this connection may run (view.catalog).
func (m *Model) webViewCatalog(call *webCall) tea.Cmd {
	call.reply(webproto.ViewCatalog{Features: webproto.ViewFeatures, Views: webproto.ViewsFor(call.conn.role)})
	return nil
}
