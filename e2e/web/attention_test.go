//go:build browser

package web

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The attention pill and the tab badges (WEB-12, web UX 4.1): a run or
// task event shows the pill until it is tapped, which opens where the
// news is; a waiting card always wins the pill and counts on the Chat tab
// and in the page title; a card the web cannot answer reads "waiting for
// the laptop".

func attention(kind, id string) *webproto.Event {
	return &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAttention, Data: raw(map[string]any{"kind": kind, "id": id})}
}

const pill = `document.querySelector(".attention")`

func toolCard(id, cmd string) map[string]any {
	return map[string]any{"id": id, "kind": "tool", "title": "AI wants to: run_command", "body": cmd, "since": 1700000000000,
		"choices": []any{
			map[string]any{"id": "deny", "label": "Deny", "safe": true},
			map[string]any{"id": "allow", "label": "Allow once"},
		}}
}

func trustCard(id string) map[string]any {
	return map[string]any{"id": id, "kind": "trust", "title": "Trust the project's rules?", "body": "Read it on the laptop first.\nAnswer this on the laptop.",
		"since": 1700000000000, "choices": []any{}, "read_only": true}
}

func cardsData(cards ...map[string]any) map[string]any {
	l := make([]any, len(cards))
	for i, c := range cards {
		l[i] = c
	}
	return map[string]any{"cards": l}
}

func TestAttentionPillFromEvents(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			c := p.Login("c-1")
			c.Send(t, Snap("cards", 1, cardsData()))
			c.Send(t, attention("run_failed", "r-1"))
			p.WaitJS(pill + `?.classList.contains("att-run_failed") && ` + pill + `.textContent.includes("A run failed")`)
			p.Tap(".attention")
			p.WaitJS(`!` + pill + ` && document.querySelector('[data-view="more"]')`)

			c.Send(t, attention("task_done", "build"))
			p.WaitJS(pill + `?.classList.contains("att-task_done")`)
			c.Send(t, Delta("cards", 2, map[string]any{"op": "upsert", "card": toolCard("card-1", "go test ./...")}))
			c.Send(t, attention("card", "card-1"))
			p.WaitJS(pill + `?.classList.contains("att-cards") && ` + pill + `.textContent.includes("1 approval waiting")`)
			p.WaitJS(`document.title === "(1) termocode"`)
			p.WaitJS(`document.querySelector("nav .nav-item:nth-child(1) .badge")?.textContent === "1"`)
			c.Send(t, attention("run_done", "r-2")) // a card still wins
			p.Holds(pill+`.classList.contains("att-cards")`, 300e6)

			c.Send(t, Delta("cards", 3, map[string]any{"op": "remove", "id": "card-1", "by": "laptop"}))
			p.WaitJS(`document.title === "termocode" && !document.querySelector("nav .nav-item:nth-child(1) .badge")`)
		})
	}
}

func TestAttentionPillWaitingForLaptop(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	c := p.Login("c-1")
	c.Send(t, Snap("cards", 1, cardsData(trustCard("card-9"))))
	p.WaitJS(pill + `?.classList.contains("att-waiting_laptop") && ` + pill + `.textContent.includes("1 waiting for the laptop")`)
}
