package app

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/textdiff"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The cards topic (WEB-15, web design 2.2): every question that waits for
// the user (a tool call of the chat's AI, an outside agent's MCP call, a
// plan, the files of a review, a trust question), each with one id that
// the laptop and every browser share. The list is built from the Model's
// own pending state, so a card answered on the laptop disappears for the
// page in the same Update: a remove delta with by "laptop" and an
// answered event to every browser. A new card is an attention event.
//
// What the web may answer is narrower than the laptop: Deny / Allow once,
// Approve / Deny, Accept / Reject. "Always allow" is never offered, Edit
// needs the laptop, trust cards are read-only (web_cards_answer.go).

func init() {
	webTopics["cards"] = webTopicDef{snapshot: webCardsSnapshot, apply: webApplyCards}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishCards)
}

// webCardsPub is the cards topic's state (Update only).
type webCardsPub struct {
	ids   map[any]string      // the pending object → its card id
	since map[string]int64    // card id → when it appeared (Unix ms)
	diffs map[any]*webCardDif // review items' diff rows, built once
	n     int                 // card ids handed out
	last  []webproto.Card     // what the browsers have (cards topic)
	seen  map[string]bool     // the ids of the last build (attention, answered)
	by    map[string]string   // card id → the device that answered it from the web
	chose map[string]string   // card id → the answer of a laptop card (confirm, cancel)
	fresh bool                // seen is a real earlier build
}

type webCardDif struct{ diff *webproto.CardDiff }

// webCardRef is a card and the pending object behind it.
type webCardRef struct {
	card webproto.Card
	obj  any
}

func webCardsSnapshot(m *Model, _ string) (any, error) {
	if m.web == nil {
		return webproto.CardsSnap{Cards: webCardList(m.webCards(&webCardsPub{}, true))}, nil
	}
	p := &m.web.cards
	p.last = webCardList(m.webCards(p, true))
	return webproto.CardsSnap{Cards: p.last}, nil
}

func webCardList(refs []webCardRef) []webproto.Card {
	out := make([]webproto.Card, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.card)
	}
	return out
}

// webPublishCards sends the changes of the cards topic, the answered
// events of cards that went away and the attention events of new ones.
// It runs while any browser is connected (the events go to all of them).
func (m *Model) webPublishCards(tea.Msg) tea.Cmd {
	h := m.web
	p := &h.cards
	listens := h.webListens("cards")
	if !listens {
		p.last = nil
	}
	if len(h.live) == 0 && !listens {
		p.seen, p.fresh, p.by = nil, false, nil
		return nil
	}
	refs := m.webCards(p, listens)
	now := make(map[string]bool, len(refs))
	for _, r := range refs {
		now[r.card.ID] = true
	}
	gone := map[string]string{} // card id → who answered it
	for id := range p.seen {
		if !now[id] {
			gone[id] = p.byFor(id)
			h.answered(id, gone[id], p.chose[id])
		}
	}
	for _, r := range refs {
		// A laptop card is the browser's own request: no attention.
		if p.fresh && !p.seen[r.card.ID] && r.card.Kind != webproto.CardLaptop {
			h.attention(webproto.AttentionCard, r.card.ID)
		}
	}
	p.seen, p.fresh = now, true
	if listens {
		cur := webCardList(refs)
		old := make(map[string]int, len(p.last))
		for i, c := range p.last {
			old[c.ID] = i
		}
		for _, c := range p.last {
			if !now[c.ID] {
				by, ok := gone[c.ID]
				if !ok {
					by = p.byFor(c.ID)
				}
				h.publish("cards", webproto.CardsDelta{Op: webproto.CardsRemove, ID: c.ID, By: by})
			}
		}
		for _, c := range cur {
			if i, ok := old[c.ID]; !ok || !webSameCard(p.last[i], c) {
				c := c
				h.publish("cards", webproto.CardsDelta{Op: webproto.CardsUpsert, Card: &c})
			}
		}
		p.last = cur
	}
	p.prune(refs)
	return nil
}

// byFor says who answered a card that went away: the browser that did,
// "" when it expired (an outside agent's call that timed out or left),
// else the laptop.
func (p *webCardsPub) byFor(id string) string {
	if by, ok := p.by[id]; ok {
		return by
	}
	for obj, oid := range p.ids {
		if a, ok := obj.(*mcpServeAsk); ok && oid == id && a.ctx.Err() != nil {
			return ""
		}
	}
	return "laptop"
}

// decided records the answer of a laptop card: the choice, and the
// device when a browser gave it.
func (p *webCardsPub) decided(id, choice, by string) {
	if p.chose == nil {
		p.chose = map[string]string{}
	}
	p.chose[id] = choice
	if by != "laptop" {
		if p.by == nil {
			p.by = map[string]string{}
		}
		p.by[id] = by
	}
}

// answered tells every browser that a card went away, who answered and,
// for a laptop card, what.
func (h *webHub) answered(id, by, choice string) {
	data, err := webData(webproto.Answered{ID: id, By: by, Choice: choice})
	if err != nil {
		return
	}
	for c := range h.live {
		c.send(&webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: data})
	}
}

// prune forgets the ids and diffs of objects no longer pending.
func (p *webCardsPub) prune(refs []webCardRef) {
	live := make(map[any]bool, len(refs))
	for _, r := range refs {
		live[r.obj] = true
	}
	for obj, id := range p.ids {
		if !live[obj] {
			delete(p.ids, obj)
			delete(p.since, id)
		}
	}
	for obj := range p.diffs {
		if !live[obj] {
			delete(p.diffs, obj)
		}
	}
	for id := range p.by {
		if p.since[id] == 0 {
			delete(p.by, id)
		}
	}
	for id := range p.chose {
		if p.since[id] == 0 {
			delete(p.chose, id)
		}
	}
}

// id returns the card id of a pending object, handing out a new one the
// first time it is seen.
func (p *webCardsPub) id(obj any) (string, int64) {
	if p.ids == nil {
		p.ids, p.since = map[any]string{}, map[string]int64{}
	}
	id, ok := p.ids[obj]
	if !ok {
		p.n++
		id = fmt.Sprintf("card-%d", p.n)
		p.ids[obj] = id
		p.since[id] = webNow().UnixMilli()
	}
	return id, p.since[id]
}

func webSameCard(a, b webproto.Card) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// webCards lists the waiting cards, oldest kind first: the chat's tool
// call, outside agents' calls (the open one, then the queue), the plan,
// the review's files, the trust questions. diffs: build review rows.
func (m *Model) webCards(p *webCardsPub, diffs bool) []webCardRef {
	var out []webCardRef
	add := func(obj any, c webproto.Card) {
		c.ID, c.Since = p.id(obj)
		out = append(out, webCardRef{card: c, obj: obj})
	}
	deny := webproto.CardChoice{ID: webproto.ChoiceDeny, Label: "Deny", Safe: true}
	allow := webproto.CardChoice{ID: webproto.ChoiceAllow, Label: "Allow once"}
	s := m.ai
	if s != nil && s.confirm != nil {
		c := s.confirm
		add(c, webproto.Card{Kind: webproto.CardTool, Title: "AI wants to: " + c.tool.Label(),
			Body: ai.Redact(aiCleanText(c.human)), Choices: []webproto.CardChoice{deny, allow}})
	}
	if st := m.mcpServe; st != nil {
		asks := append([]*mcpServeAsk(nil), st.queue...)
		if st.active != nil {
			asks = append([]*mcpServeAsk{st.active}, asks...)
		}
		for _, a := range asks {
			if a.ctx.Err() != nil {
				continue
			}
			body := ai.Redact(aiCleanText(a.human))
			if a.args != "" {
				body += "\n\nArguments: " + ai.Redact(aiCleanText(a.args))
			}
			body += "\n\nAsked by an outside agent over MCP (tool " + a.tool.Name + "). The name is the agent's own claim."
			add(a, webproto.Card{Kind: webproto.CardMCP, Title: strings.TrimSpace(strings.TrimPrefix(mcpServeCardTitle(a.client, a.tool.Name, a.human), aiSpark())),
				Body: body, Choices: []webproto.CardChoice{deny, allow}})
		}
	}
	if s != nil {
		if rm := s.aiPlanWaiting(); rm != nil {
			title := "Approve this plan?"
			if rm.Run.Approved != nil {
				title = "The plan grew: approve the new steps?"
			}
			add(s.planWait, webproto.Card{Kind: webproto.CardPlan, Title: title, Body: m.aiPlanCardBody(rm),
				Choices: []webproto.CardChoice{deny, {ID: webproto.ChoiceApprove, Label: "Approve"}, {ID: "edit", Label: "Edit", NeedsLaptop: true}}})
		}
		if r := s.review; r != nil && r.card.Review() {
			for i, it := range r.items {
				if it.done || r.card.Done(i) {
					continue
				}
				c := webproto.Card{Kind: webproto.CardReview, Title: "Review AI change: " + it.file.Name, Body: it.file.Tag,
					File: it.file.Name, Choices: []webproto.CardChoice{
						{ID: webproto.ChoiceReject, Label: "Reject", Safe: true}, {ID: webproto.ChoiceAccept, Label: "Accept"}}}
				if diffs {
					c.Diff = p.diff(it, r.card.Diff(i))
				}
				add(it, c)
			}
		}
		trust := func(obj any, title, body string) {
			add(obj, webproto.Card{Kind: webproto.CardTrust, Title: title, Body: body + "\nAnswer this on the laptop.",
				Choices: []webproto.CardChoice{}, ReadOnly: true})
		}
		if t := s.profTrust; t != nil {
			trust(t, "Trust the project's profile "+t.name+"?", "This project brings its own agent profile. Its prompts are shown on the laptop first.")
		}
		if t := s.rulesTrust; t != nil {
			trust(t, "Trust the project's rules?", "This project has a rules file for the AI. Read it on the laptop first.")
		}
		if t := s.skillsTrust; t != nil {
			trust(t, "Trust the project's skills?", "This project brings skills for the AI. Read them on the laptop first.")
		}
		if t := s.mcpTrust; t != nil {
			trust(t, "Trust the MCP server "+t.server+"?", "A service wants to give the AI its tools.")
		}
	}
	if m.web != nil {
		for _, a := range m.web.asks.line {
			add(a, webproto.Card{Kind: webproto.CardLaptop, Title: a.title, Body: a.body, Choices: []webproto.CardChoice{
				{ID: webproto.ChoiceCancel, Label: "Cancel", Safe: true},
				{ID: webproto.ChoiceConfirm, Label: a.action, NeedsLaptop: true}}})
		}
	}
	return out
}

// diff returns a review item's rows (built once), cut at MaxCardDiff.
func (p *webCardsPub) diff(it *aiReviewItem, d *textdiff.Diff) *webproto.CardDiff {
	if p.diffs == nil {
		p.diffs = map[any]*webCardDif{}
	}
	if c, ok := p.diffs[it]; ok {
		return c.diff
	}
	var out *webproto.CardDiff
	if d != nil {
		out = &webproto.CardDiff{Rows: []webproto.DiffRow{}, Added: d.Added, Removed: d.Removed}
		size := 0
		for _, r := range d.Rows {
			if size += len(r.Text) + 16; size > webproto.MaxCardDiff {
				out.Truncated = true
				break
			}
			k := "ctx"
			switch r.Kind {
			case textdiff.Added:
				k = "add"
			case textdiff.Removed:
				k = "del"
			}
			out.Rows = append(out.Rows, webproto.DiffRow{K: k, Text: ai.RedactSecrets(r.Text), Old: r.OldLine, New: r.NewLine})
		}
	}
	p.diffs[it] = &webCardDif{diff: out}
	return out
}

// webApplyCards folds one cards delta into a snapshot (the browser store
// does the same).
func webApplyCards(snap, delta json.RawMessage) (json.RawMessage, error) {
	var s webproto.CardsSnap
	var d webproto.CardsDelta
	if err := json.Unmarshal(snap, &s); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(delta, &d); err != nil {
		return nil, err
	}
	switch d.Op {
	case webproto.CardsUpsert:
		if d.Card == nil {
			return nil, fmt.Errorf("cards: upsert without a card")
		}
		i := 0
		for i < len(s.Cards) && s.Cards[i].ID != d.Card.ID {
			i++
		}
		if i == len(s.Cards) {
			s.Cards = append(s.Cards, *d.Card)
		} else {
			s.Cards[i] = *d.Card
		}
	case webproto.CardsRemove:
		out := s.Cards[:0]
		for _, c := range s.Cards {
			if c.ID != d.ID {
				out = append(out, c)
			}
		}
		s.Cards = out
	default:
		return nil, fmt.Errorf("cards: unknown op %q", d.Op)
	}
	return json.Marshal(s)
}
