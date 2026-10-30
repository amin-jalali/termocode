package app

import (
	"encoding/json"
	"reflect"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The "bufs" topic (WEB-17): the files open on the laptop, in its tab
// order, so the phone opens "what the laptop is looking at" in one tap.
// Only files inside the workspace are listed. The list comes from the
// state the TUI already refreshes (m.bufs); a change goes out as the
// whole list.

func init() {
	webTopics["bufs"] = webTopicDef{snapshot: webBufsSnapshot, apply: webBufsApply}
}

// webBufsNow builds the list from the Model.
func (m *Model) webBufsNow() webproto.BufsSnap {
	out := webproto.BufsSnap{Bufs: []webproto.BufItem{}}
	for _, b := range m.bufs {
		rel, ok := m.webRel(b.Path)
		if !ok || rel == "" {
			continue
		}
		out.Bufs = append(out.Bufs, webproto.BufItem{Buf: b.ID, Path: webName(rel), Dirty: b.Modified, Active: b.ID == m.activeBuf})
	}
	return out
}

func webBufsSnapshot(m *Model, _ string) (any, error) {
	s := m.webBufsNow()
	if m.web != nil {
		m.web.filesState().bufsList = &s
	}
	return s, nil
}

// webBufsApply: the delta is the whole list.
func webBufsApply(_, delta json.RawMessage) (json.RawMessage, error) {
	var s webproto.BufsSnap
	if err := json.Unmarshal(delta, &s); err != nil {
		return nil, err
	}
	return json.Marshal(s)
}

// webTrackBufs publishes the list when it changed and someone follows it.
func (m *Model) webTrackBufs(h *webHub, f *webFiles) {
	if !h.webListens("bufs") {
		f.bufsList = nil
		return
	}
	now := m.webBufsNow()
	if f.bufsList != nil && reflect.DeepEqual(*f.bufsList, now) {
		return
	}
	f.bufsList = &now
	m.webPublish("bufs", now)
}
