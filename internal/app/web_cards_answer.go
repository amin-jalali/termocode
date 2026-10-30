package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// cards.answer (WEB-15). The answer runs on Update like the laptop's: the
// first answer wins (the second finds the card gone), the laptop's dialog
// for that card closes, the laptop shows "Answered on <device>", and the
// AI audit channel gets one line with by=web:<device>. A web answer never
// has more power than a laptop answer: only the choices the card lists
// for the web, never "Always allow", never a trust or edit choice. The
// card itself is the confirmation, so no extra laptop card is asked.

func init() {
	webOps[webproto.OpCardsAnswer] = (*Model).webCardsAnswer
}

func (m *Model) webCardsAnswer(call *webCall) tea.Cmd {
	a := call.args.(*webproto.CardsAnswerArgs)
	p := &m.web.cards
	var ref *webCardRef
	refs := m.webCards(p, false)
	for i := range refs {
		if refs[i].card.ID == a.ID {
			ref = &refs[i]
		}
	}
	if ref == nil {
		call.fail(webproto.CodeGone, "This card was answered already.")
		return nil
	}
	if !webCardAllows(ref.card, a.Choice) {
		call.fail(webproto.CodeForbidden, "Answer this on the laptop.")
		return nil
	}
	device := call.conn.device
	if device == "" {
		device = "web"
	}
	if ask, ok := ref.obj.(*webAsk); ok { // the web may only cancel it
		cmd := m.webAskDone(ask, webproto.ChoiceCancel, device)
		var t tea.Cmd
		m.toast, t = m.toast.PushDetail(toast.Info, "Cancelled on "+device, ask.title)
		call.reply(nil)
		return tea.Batch(cmd, t)
	}
	cmd := m.webAnswerCard(ref, a.Choice)
	if p.by == nil {
		p.by = map[string]string{}
	}
	p.by[a.ID] = device
	m.aiAudit(webNow(), "card:"+ref.card.Kind, webAuditArgs(ref), a.Choice, "web:"+device, webAnswerWord(a.Choice))
	var t tea.Cmd
	m.toast, t = m.toast.PushDetail(toast.Info, "Answered on "+device, webAnswerWord(a.Choice)+": "+ref.card.Title)
	call.reply(nil)
	return tea.Batch(cmd, t)
}

// webCardAllows: choice is one of the card's choices the web may pick.
func webCardAllows(c webproto.Card, choice string) bool {
	if c.ReadOnly {
		return false
	}
	for _, ch := range c.Choices {
		if ch.ID == choice {
			return !ch.NeedsLaptop
		}
	}
	return false
}

// webAnswerCard gives the answer to what waits, as the laptop's dialog
// would, and closes that dialog.
func (m *Model) webAnswerCard(ref *webCardRef, choice string) tea.Cmd {
	switch obj := ref.obj.(type) {
	case *aiPendingConfirm:
		if m.confirmOpen && m.confirmKind == confirmKindAITool {
			m.confirmOpen = false
		}
		return m.answerAIToolConfirm(choice) // "allow" or "deny" only: never "always"
	case *mcpServeAsk:
		st := m.mcpServe
		if st.active == obj {
			if m.confirmOpen && m.confirmKind == confirmKindMCPServe {
				m.confirmOpen = false
			}
			return m.answerMCPServeConfirm(choice)
		}
		for i, q := range st.queue { // not on show yet: answer it in the line
			if q == obj {
				st.queue = append(st.queue[:i], st.queue[i+1:]...)
				obj.reply <- choice == webproto.ChoiceAllow
				break
			}
		}
		return m.mcpServePump()
	case *aiPlanWait:
		if m.confirmOpen && m.confirmKind == confirmKindAIPlan {
			m.confirmOpen = false
		}
		return m.answerAIPlan(choice) // "approve" or "deny"
	case *aiReviewItem:
		r := m.aiS().review
		for i, it := range r.items {
			if it != obj {
				continue
			}
			acc := map[int]bool{}
			if choice == webproto.ChoiceAccept {
				if d := r.card.Diff(i); d != nil && len(d.Hunks) > 0 {
					for _, h := range d.Hunks {
						acc[h.Index] = true
					}
				} else {
					acc[0] = true // an end-of-line-only change
				}
			}
			return m.onAIReviewFinish(diffview.FinishMsg{File: i, Accepted: acc})
		}
	}
	return nil
}

// webAuditArgs names what the card was about, short and without secrets.
func webAuditArgs(ref *webCardRef) string {
	switch obj := ref.obj.(type) {
	case *aiPendingConfirm:
		return "tool=" + obj.tool.Name
	case *mcpServeAsk:
		return "tool=" + obj.tool.Name + " client=" + obj.client
	case *aiReviewItem:
		return "file=" + ref.card.File
	}
	return ""
}

func webAnswerWord(choice string) string {
	switch choice {
	case webproto.ChoiceAllow:
		return "Allowed once"
	case webproto.ChoiceApprove:
		return "Approved"
	case webproto.ChoiceAccept:
		return "Accepted"
	case webproto.ChoiceReject:
		return "Rejected"
	}
	return "Denied"
}
