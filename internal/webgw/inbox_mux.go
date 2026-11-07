package webgw

import (
	"bytes"
	"encoding/json"
	"errors"
	"sync"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The door's merged inbox (MOB-53). The gateway holds the inbox topic of
// its home hub and of every project hub attached through the door
// (MOB-50), keyed by workspace key, and merges them with
// webproto.MergeInbox. The watch socket (/watch, MOB-23) and push counts
// (MOB-30) read this one stream: one line ("1 thing needs you · shop"),
// the new needs-you items for local notifications, a wake on change. It
// carries no file content and no paths: inbox items name their sources
// by ref only, and project names are cut by webproto.InboxProjectName.

// maxInboxLine caps one inbox snap from a hub (200 items of ≤ 120-byte
// titles fit well below it).
const maxInboxLine = 256 << 10

// maxInboxWatchers caps the watchers at once: two watch sockets for each
// of the viewers the gateway allows, and the push sender.
const maxInboxWatchers = 2*DefaultMaxViewers + 1

// Errors of InboxMux.Set.
var (
	errInboxWS    = errors.New("inbox: not a workspace key")
	errInboxBig   = errors.New("inbox: snap too big")
	errInboxBad   = errors.New("inbox: bad snap")
	errInboxLimit = errors.New("inbox: too many projects")
)

// InboxMux is the merged inbox. Safe for concurrent use.
type InboxMux struct {
	home string

	mu       sync.Mutex
	parts    map[string]webproto.InboxSnap // by ws; home under its key
	names    map[string]string
	merged   webproto.InboxSnap
	taken    webproto.InboxSnap // as of the last TakeNew
	watchers map[chan struct{}]struct{}
}

// NewInboxMux makes the mux of a gateway whose home workspace is home.
func NewInboxMux(home string) *InboxMux {
	m := &InboxMux{
		home:     home,
		parts:    map[string]webproto.InboxSnap{},
		names:    map[string]string{},
		watchers: map[chan struct{}]struct{}{},
	}
	m.merged = webproto.MergeInbox(home, nil)
	m.taken = m.merged
	return m
}

// key maps "" to the home key and refuses anything but a workspace key.
func (m *InboxMux) key(ws string) (string, bool) {
	if ws == "" {
		ws = m.home
	}
	return ws, webproto.InboxWS(ws)
}

// Set takes the data of an inbox snap or delta (the whole list) from the
// hub of ws ("" is home). The data is decoded strictly: an unknown key or
// an oversized line is refused, and the project keeps its last good
// inbox. At most webproto.InboxMaxHubs projects besides home.
func (m *InboxMux) Set(ws string, data json.RawMessage) error {
	k, ok := m.key(ws)
	if !ok {
		return errInboxWS
	}
	if len(data) > maxInboxLine {
		return errInboxBig
	}
	var s webproto.InboxSnap
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&s) != nil || dec.More() {
		return errInboxBad
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, had := m.parts[k]; !had && k != m.home && m.others() >= webproto.InboxMaxHubs {
		return errInboxLimit
	}
	m.parts[k] = s
	m.remerge()
	return nil
}

// others counts the projects besides home. m.mu is held.
func (m *InboxMux) others() int {
	n := len(m.parts)
	if _, ok := m.parts[m.home]; ok {
		n--
	}
	return n
}

// SetName names a project for the line (the projects topic's name).
func (m *InboxMux) SetName(ws, name string) {
	k, ok := m.key(ws)
	if !ok {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.names[k] = webproto.InboxProjectName(name)
}

// Drop forgets a project whose hub went away (ws.gone, project.close).
func (m *InboxMux) Drop(ws string) {
	k, ok := m.key(ws)
	if !ok {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, had := m.parts[k]; !had {
		return
	}
	delete(m.parts, k)
	delete(m.names, k)
	m.remerge()
}

// remerge rebuilds the stream and wakes the watchers. m.mu is held.
func (m *InboxMux) remerge() {
	parts := make([]webproto.InboxPart, 0, len(m.parts))
	for ws, s := range m.parts {
		parts = append(parts, webproto.InboxPart{WS: ws, Snap: s})
	}
	m.merged = webproto.MergeInbox(m.home, parts)
	for ch := range m.watchers {
		select {
		case ch <- struct{}{}:
		default: // a wake is pending already
		}
	}
}

// Snap is the merged inbox now.
func (m *InboxMux) Snap() webproto.InboxSnap {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.merged
}

// Line is the needs-you line of the merged inbox ("" when nothing needs
// the user).
func (m *InboxMux) Line() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return webproto.InboxNeedsYouLine(m.merged, m.names)
}

// TakeNew returns the items that need the user and were not there at the
// last TakeNew: one notification each, never twice for the same item.
func (m *InboxMux) TakeNew() []webproto.InboxItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	news := webproto.InboxNewNeedsYou(m.taken, m.merged)
	m.taken = m.merged
	return news
}

// Watch gives a channel that gets a value after every change (coalesced:
// one pending wake at most) and the func that ends the watch. Past
// maxInboxWatchers the channel comes closed: the caller ends its socket.
func (m *InboxMux) Watch() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.watchers) >= maxInboxWatchers {
		close(ch)
		return ch, func() {}
	}
	m.watchers[ch] = struct{}{}
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			delete(m.watchers, ch)
		})
	}
}
