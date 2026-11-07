package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The live board of the phone (MOB-18, mobile plan §2.5, §3.8): board.get
// builds the board from what the cards, runs, worktrees and workflows
// topics would send now (the same builders, the same redaction), so the
// board shows nothing those topics do not and adds no write path. The
// page asks again when one of those topics changes; a tap on a tile
// opens that topic's own view, where every action keeps its own rules.

func init() {
	webOps[webproto.OpBoardGet] = (*Model).webBoardGet
}

// webBoardInput is the board's sources now. The cards keep the ids the
// cards topic gave them (and nothing the topic sent is touched).
func (m *Model) webBoardInput() webproto.BoardInput {
	p := &webCardsPub{}
	if m.web != nil {
		p = &m.web.cards
	}
	now := clock.Now()
	return webproto.BoardInput{
		Cards:     webproto.CardsSnap{Cards: webCardList(m.webCards(p, false))},
		Runs:      m.webRunsBuild(now),
		Worktrees: m.webWorktreesBuild(),
		Workflows: m.webWorkflowsBuild(),
		Now:       now,
	}
}

func (m *Model) webBoardGet(call *webCall) tea.Cmd {
	call.reply(webproto.BuildBoard(m.webBoardInput()))
	return nil
}
