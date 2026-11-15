package app

import (
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/blocks"
)

// The db_connections card (owner 2026-11-15: "there are no actions on the
// results"): one row per connection with its status badge and the
// actions of the Databases view. The card is built from the text the
// model got (like every tool view), so it shows nothing the model did
// not see. The chat (desktop and web page) draws it; a row's tap opens
// the Databases view on that connection, and each row points at its
// connection (Ref) so it can be added to the chat.

// dbCardRefKind is a row's ref kind: the web page's item attach kind of
// a database connection (webproto.ItemDBConn).
const dbCardRefKind = "db_conn"

// The ops of the card's buttons. db.connect, db.tables and db.test are the
// page's own ops (the same policy and laptop flow as its Databases view);
// the others open a place of the view and run nothing by themselves.
const (
	dbCardOpConnect = "db.connect"
	dbCardOpTables  = "db.tables"
	dbCardOpTest    = "db.test"
	dbCardOpOpen    = "db.open"      // the Databases view on the connection
	dbCardOpQuery   = "db.new_query" // a new SQL console on it
	dbCardOpFix     = "db.fix_"      // + the field: its edit form at that field
)

// dbCardRow is one connection line of the db_connections text.
type dbCardRow struct {
	id, name, target, status, where string
	remote                          bool
}

// dbCardRows reads the connection lines of the db_connections text.
func dbCardRows(out string) []dbCardRow {
	var rows []dbCardRow
	for _, l := range strings.Split(out, "\n") {
		if !strings.Contains(l, " · status: ") || strings.HasPrefix(l, " ") {
			continue
		}
		parts := strings.Split(l, " · ")
		r := dbCardRow{id: parts[0]}
		for i, p := range parts[1:] {
			switch {
			case strings.HasPrefix(p, "name "):
				r.name = strings.TrimPrefix(p, "name ")
			case strings.HasPrefix(p, "status: "):
				r.status = strings.TrimPrefix(p, "status: ")
			case strings.HasPrefix(p, "where: "):
				r.where = strings.TrimPrefix(p, "where: ")
			case p == "remote: yes":
				r.remote = true
			case r.target == "" && (i == 0 || i == 1 && r.name != ""):
				r.target = p
			}
		}
		if r.id != "" && r.status != "" {
			rows = append(rows, r)
		}
	}
	return rows
}

// dbConnectionsView is db_connections' card.
func dbConnectionsView(_ map[string]any, out string) *blocks.Block {
	rows := dbCardRows(out)
	if len(rows) == 0 {
		return nil
	}
	b := toolBlock(blocks.KindList, "Databases · "+strconv.Itoa(len(rows))+" connection"+plural(len(rows)))
	for i, r := range rows {
		if len(b.Items) == blocks.MaxItems {
			b.Total, b.Truncated = len(rows), true
			break
		}
		b.Items = append(b.Items, dbCardItem(i, r))
	}
	b.ForYou = "Tap a row to open it in Databases."
	return ai.ViewDone(b)
}

// dbCardItem is the row of one connection.
func dbCardItem(i int, r dbCardRow) blocks.BlockItem {
	kind, field := dbStatusFromLabel(r.status)
	id := r.id
	if len(id) > blocks.MaxID || strings.ContainsFunc(id, func(c rune) bool {
		return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("_.:-", c))
	}) {
		id = ai.ViewItemID(i)
	}
	title := r.name
	if title == "" {
		title = r.id
	}
	sub := []string{}
	if _, detail, ok := strings.Cut(r.status, ": "); ok && kind != dbStatusReady {
		sub = append(sub, detail)
	}
	if r.target != "" {
		sub = append(sub, r.target)
	}
	if r.where != "" {
		sub = append(sub, r.where)
	}
	ref := &blocks.BlockRef{Kind: dbCardRefKind, ID: ai.ViewText(r.id, blocks.MaxTitle)}
	it := blocks.BlockItem{ID: id, Title: ai.ViewText(title, blocks.MaxTitle), Sub: ai.ViewText(strings.Join(sub, " · "), blocks.MaxSub), Ref: ref}
	it.Badge, it.Tone = dbCardBadge(kind, field)
	act := func(id, label, op string) blocks.BlockAction {
		return blocks.BlockAction{ID: id, Label: label, Kind: blocks.ActOp, Op: op, Ref: ref}
	}
	switch kind {
	case dbStatusMissing:
		it.Actions = append(it.Actions, act("fix", "Fix "+field+"…", dbCardOpFix+strings.ReplaceAll(field, " ", "_")))
	case dbStatusProblem:
		it.Actions = append(it.Actions, act("fix", "Fix…", dbCardOpFix+"file"))
	}
	if kind != dbStatusConnected && kind != dbStatusConnecting {
		it.Actions = append(it.Actions, act("connect", "Connect", dbCardOpConnect))
	}
	it.Actions = append(it.Actions, act("open", "Open in Databases", dbCardOpOpen), act("tables", "List tables", dbCardOpTables),
		act("query", "New query", dbCardOpQuery), act("test", "Test", dbCardOpTest))
	if len(it.Actions) > blocks.MaxActions {
		it.Actions = it.Actions[:blocks.MaxActions]
	}
	return it
}

// dbCardBadge is a status's badge and tone (the page draws a shape too).
func dbCardBadge(kind, field string) (string, string) {
	switch kind {
	case dbStatusConnected:
		return "connected", blocks.ToneOK
	case dbStatusConnecting:
		return "connecting", blocks.ToneBusy
	case dbStatusMissing:
		return ai.ViewText("missing "+field, blocks.MaxID), blocks.ToneWarn
	case dbStatusUnreachable:
		return "unreachable", blocks.ToneBad
	case dbStatusTunnelDown:
		return "tunnel down", blocks.ToneBad
	case dbStatusProblem:
		return "problem", blocks.ToneBad
	}
	return "ready", blocks.ToneIdle
}
