package app

import (
	"encoding/json"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/blocks"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The db_connections card in the desktop chat (owner 2026-11-15: "bring
// Connect, Query, List tables … as buttons for each"). It is drawn under
// the tool line from the block the tool's View built (db_conn_card.go):
// a row per connection with its status shape and badge, its target, and
// buttons. A click on the row opens Databases on the connection; each
// button runs the Databases view's own step, with its own cards and
// prompts (a moved password, an unknown host key, a production write
// keep asking there).

// dbCardButton prefixes the card's button ids: dbcard:<act>:<conn>.
const dbCardButton = "dbcard:"

// aiIsDBCard: b is a db_connections card.
func aiIsDBCard(b *blocks.Block) bool {
	return b != nil && b.Source == "db_connections" && b.Kind == blocks.KindList && len(b.Items) > 0
}

// dbCardAct is the button act of a card action (its op without "db.").
func dbCardAct(a blocks.BlockAction) string {
	switch a.Op {
	case dbCardOpConnect:
		return "connect"
	case dbCardOpOpen:
		return "open"
	case dbCardOpTables:
		return "tables"
	case dbCardOpQuery:
		return "query"
	case dbCardOpTest:
		return "test"
	}
	if f, ok := strings.CutPrefix(a.Op, dbCardOpFix); ok && f != "" {
		return "fix_" + f
	}
	return ""
}

// dbCardTUILabel is a button's short label in the chat.
func dbCardTUILabel(act string, a blocks.BlockAction) string {
	switch act {
	case "open":
		return "Open"
	case "tables":
		return "Tables"
	}
	return a.Label
}

// dbCardShape is a tone's shape (never colour alone) and colour.
func dbCardShape(tone string) (string, theme.Color256) {
	switch tone {
	case blocks.ToneOK:
		return "✓", theme.GitAdded
	case blocks.ToneWarn:
		return "●", theme.DiagWarning
	case blocks.ToneBad:
		return "✗", theme.DiagError
	case blocks.ToneBusy:
		return "◐", theme.DiagInfo
	}
	return "○", theme.TextDim
}

// aiDBCardLines are the card's rows at width.
func aiDBCardLines(b *blocks.Block, width int) []aiLine {
	ind := strings.Repeat(" ", aiIndent+2)
	x0 := aiIndent + 2
	lim := max(width-x0-1, 12)
	cut := func(s string, n int) string {
		if runewidth.StringWidth(s) > n {
			return runewidth.Truncate(s, n, "…")
		}
		return s
	}
	dim := aiStyle(theme.Fg(theme.TextDim))
	pad := theme.Bg(theme.BgPanel)
	out := []aiLine{{text: ind + cut("≡ "+aiCleanText(b.Title), lim), style: aiStyle(theme.Fg(theme.TextSecondary))}}
	for _, it := range b.Items {
		if it.Ref == nil || it.Ref.ID == "" {
			continue
		}
		conn := it.Ref.ID
		shape, col := dbCardShape(it.Tone)
		title := cut(aiCleanText(it.Title), max(lim-runewidth.StringWidth(it.Badge)-4, 6))
		row := pad.Render(ind) + theme.Fg(col).Render(shape) + pad.Render(" ") + theme.Fg(theme.TextPrimary).Bold(true).Render(title) +
			pad.Render("  ") + theme.Fg(col).Render(it.Badge)
		out = append(out, aiLine{styled: row, buttons: []aiLineButton{{x0, x0 + 2 + runewidth.StringWidth(title) + 2 + runewidth.StringWidth(it.Badge), dbCardButton + "open:" + conn}}})
		if it.Sub != "" {
			out = append(out, aiLine{text: ind + "  " + cut(aiCleanText(it.Sub), lim-2), style: dim})
		}
		type chip struct{ label, id string }
		var chips []chip
		for _, a := range it.Actions {
			if act := dbCardAct(a); act != "" {
				chips = append(chips, chip{dbCardTUILabel(act, a), dbCardButton + act + ":" + conn})
			}
		}
		chips = append(chips, chip{"+ Chat", dbCardButton + "chat:" + conn})
		bg := lipgloss.NewStyle().Background(theme.Mix(theme.BgPanel, theme.AIAccent, 0.16)).Foreground(theme.LG(theme.TextPrimary))
		line, x := pad.Render(ind+"  "), x0+2
		var btns []aiLineButton
		for _, c := range chips {
			w := runewidth.StringWidth(c.label) + 2
			if x+w > width-1 && len(btns) > 0 {
				out = append(out, aiLine{styled: line, buttons: btns})
				line, x, btns = pad.Render(ind+"  "), x0+2, nil
			}
			line += bg.Render(" "+c.label+" ") + pad.Render(" ")
			btns = append(btns, aiLineButton{x, x + w, c.id})
			x += w + 1
		}
		out = append(out, aiLine{styled: line, buttons: btns})
	}
	if b.ForYou != "" {
		out = append(out, aiLine{text: ind + cut(aiCleanText(b.ForYou), lim), style: dim})
	}
	return out
}

// onAIDBCardButton runs one card button: dbcard:<act>:<conn>.
func (m *Model) onAIDBCardButton(id string) tea.Cmd {
	act, conn, ok := strings.Cut(strings.TrimPrefix(id, dbCardButton), ":")
	if !ok || conn == "" {
		return nil
	}
	m.ensureDatabasesLoaded()
	s := m.databases
	c, found := s.connection(conn)
	if !found {
		return m.pushToastDetail(toast.Info, "Connection not found", sanitizeDBText(conn)+" is no longer in Databases.")
	}
	switch act {
	case "chat":
		spec, _ := json.Marshal(webproto.ItemSpec{Item: webproto.ItemDBConn, Conn: c.ID})
		text, err := webItemText(string(spec))
		if err != nil {
			return nil
		}
		st := m.aiS()
		st.attachments = append(st.attachments, aiAttachment{Label: "db " + sanitizeDBText(dbConnName(c)) + " (" + c.ID + ")", Text: text})
		return nil
	case "query":
		if m.nvim == nil {
			return m.pushToastDetail(toast.Info, "The editor is not ready", "A new query opens in the editor.")
		}
		return m.databaseNewConsole(c.ID)
	}
	reveal := m.revealDatabasesView(act == "open" || strings.HasPrefix(act, "fix_"))
	s.databaseSelectPath(c.ID)
	switch act {
	case "open":
		return reveal
	case "connect":
		if s.sessions[c.ID] != nil {
			return reveal
		}
		return tea.Batch(reveal, m.databaseConnect(c.ID))
	case "tables":
		if s.sessions[c.ID] == nil {
			return tea.Batch(reveal, m.databaseConnect(c.ID)) // the tree unfolds once connected
		}
		delete(s.folded, c.ID)
		s.touch()
		if !s.nodes[c.ID].loaded {
			return tea.Batch(reveal, m.databaseExpand(c.ID))
		}
		return reveal
	case "test":
		return tea.Batch(reveal, m.databaseOpen(c.ID, true))
	}
	if field, ok := strings.CutPrefix(act, "fix_"); ok {
		return tea.Batch(reveal, m.dbCardFix(c, strings.ReplaceAll(field, "_", " ")))
	}
	return reveal
}

// dbCardFix opens the place that sets field: the password prompt, the
// edit form at that field (your own connections), else the file.
func (m *Model) dbCardFix(c dbclient.Conn, field string) tea.Cmd {
	if field == "password" {
		return m.databaseSetPasswordPrompt(c.ID)
	}
	steps := map[string]int{"host": 1, "port": 2, "user": 3, "database": 4}
	if st, ok := steps[field]; ok && c.Source == dbclient.SourceUser && c.Driver != "sqlite" {
		ms := m.dbManage()
		*ms = dbManageState{id: c.ID, step: st,
			edit: dbclient.ConnEdit{Name: c.Name, Comment: c.Comment, Host: c.Host, Port: c.Port, User: c.User, Database: c.Database}}
		return m.dbEditAsk()
	}
	return m.databaseEdit(c.ID)
}
