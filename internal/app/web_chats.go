package app

import (
	"encoding/json"
	"fmt"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The chats topic (WEB-13): the stored chats, newest first, with the one
// open on the laptop live (its title, size, busy). The stored list is
// read from disk off the UI thread: when a browser first listens, and
// again after the open chat was saved or switched (titles, deletes, the
// 50-chat prune). Until the first read the list holds the open chat only.

func init() {
	webTopics["chats"] = webTopicDef{snapshot: webChatsSnapshot, apply: webApplyChats}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishChats)
}

// webChatsPub is the chats topic's state (Update only).
type webChatsPub struct {
	stored  []webproto.ChatItem // from disk; nil: not read yet
	loading bool                // a read runs
	gen     int                 // drops the result of an older read
	saved   string              // the open chat's id and save time at the last read
	last    []webproto.ChatItem // what the browsers have
}

// webChatsListedMsg: a read of the chat store finished.
type webChatsListedMsg struct {
	gen   int
	items []webproto.ChatItem
}

// webChatsList reads the chat store (off the UI thread).
var webChatsList = func(store ai.ChatStore) []webproto.ChatItem {
	list := store.List()
	out := make([]webproto.ChatItem, 0, len(list))
	for _, c := range list {
		out = append(out, webproto.ChatItem{ID: c.ID, Title: webChatTitle(c), Updated: c.UpdatedAtMs, Msgs: len(c.Messages)})
	}
	return out
}

func webChatsSnapshot(m *Model, _ string) (any, error) {
	if m.web == nil {
		return webproto.ChatsSnap{Chats: m.webChatsItems(&webChatsPub{})}, nil
	}
	p := &m.web.chats
	p.last = m.webChatsItems(p)
	return webproto.ChatsSnap{Chats: p.last}, nil
}

// webChatsItems is the list: the stored chats with the open one live,
// newest first, at most webproto.MaxChats.
func (m *Model) webChatsItems(p *webChatsPub) []webproto.ChatItem {
	out := make([]webproto.ChatItem, 0, len(p.stored)+1)
	var open *ai.Conversation
	if m.ai != nil && len(m.ai.conv.Messages) > 0 { // an empty new chat is not stored yet
		open = &m.ai.conv
		out = append(out, webproto.ChatItem{ID: open.ID, Title: webChatTitle(*open), Updated: open.UpdatedAtMs,
			Msgs: len(open.Messages), Open: true, Live: m.ai.streaming})
	}
	for _, c := range p.stored {
		if open == nil || c.ID != open.ID {
			out = append(out, c)
		}
	}
	webChatsSort(out)
	if len(out) > webproto.MaxChats {
		out = out[:webproto.MaxChats]
	}
	return out
}

// webChatsSort orders newest first (id breaks a tie, so the order is the
// same on every side).
func webChatsSort(items []webproto.ChatItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Updated != items[j].Updated {
			return items[i].Updated > items[j].Updated
		}
		return items[i].ID < items[j].ID
	})
}

// webPublishChats reads the store when needed and sends what changed.
func (m *Model) webPublishChats(msg tea.Msg) tea.Cmd {
	h := m.web
	p := &h.chats
	if l, ok := msg.(webChatsListedMsg); ok && l.gen == p.gen {
		p.stored, p.loading = l.items, false
	}
	if !h.webListens("chats") {
		if p.stored != nil || p.loading || p.last != nil {
			*p = webChatsPub{gen: p.gen + 1} // a read still running is not wanted
		}
		return nil
	}
	var read tea.Cmd
	if m.ai != nil {
		saved := m.ai.conv.ID + "@" + fmt.Sprint(m.ai.conv.UpdatedAtMs)
		if (p.stored == nil || saved != p.saved) && !p.loading {
			p.saved, p.loading = saved, true
			p.gen++
			gen, store := p.gen, m.ai.store
			read = func() tea.Msg { return webChatsListedMsg{gen: gen, items: webChatsList(store)} }
		}
	}
	cur := m.webChatsItems(p)
	old := make(map[string]webproto.ChatItem, len(p.last))
	for _, c := range p.last {
		old[c.ID] = c
	}
	now := make(map[string]bool, len(cur))
	for _, c := range cur {
		now[c.ID] = true
	}
	for _, c := range p.last {
		if !now[c.ID] {
			h.publish("chats", webproto.ChatsDelta{Op: webproto.ChatsRemove, ID: c.ID})
		}
	}
	for _, c := range cur {
		if o, ok := old[c.ID]; !ok || o != c {
			c := c
			h.publish("chats", webproto.ChatsDelta{Op: webproto.ChatsUpsert, Chat: &c})
		}
	}
	p.last = cur
	return read
}

// webApplyChats folds one chats delta into a snapshot (the browser store
// does the same): the list stays sorted and capped.
func webApplyChats(snap, delta json.RawMessage) (json.RawMessage, error) {
	var s webproto.ChatsSnap
	var d webproto.ChatsDelta
	if err := json.Unmarshal(snap, &s); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(delta, &d); err != nil {
		return nil, err
	}
	id := d.ID
	switch d.Op {
	case webproto.ChatsUpsert:
		if d.Chat == nil {
			return nil, fmt.Errorf("chats: upsert without a chat")
		}
		id = d.Chat.ID
	case webproto.ChatsRemove:
	default:
		return nil, fmt.Errorf("chats: unknown op %q", d.Op)
	}
	out := s.Chats[:0]
	for _, c := range s.Chats {
		if c.ID != id {
			out = append(out, c)
		}
	}
	if d.Op == webproto.ChatsUpsert {
		out = append(out, *d.Chat)
	}
	webChatsSort(out)
	if len(out) > webproto.MaxChats {
		out = out[:webproto.MaxChats]
	}
	s.Chats = out
	return json.Marshal(s)
}
