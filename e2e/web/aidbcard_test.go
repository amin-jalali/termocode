//go:build browser

package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Owner 2026-11-15: "Which remote databases do we have connected?" got a
// plain table with no actions and no real state. The chat draws the
// db_connections card: one row per connection with its status badge,
// Connect, Open in Databases, List tables, New query, Test and Fix… when
// info is missing; a tap on the row opens Databases on it; + Chat puts
// the row into the next message. Phone (390×664) and desktop alike.

// phoneSmall is the phone the owner asked about: 390×664.
var phoneSmall = Viewport{Name: "phone", W: 390, H: 664, Scale: 3, Mobile: true}

func dbCardItem(id, title, badge, tone, sub string, fix bool) map[string]any {
	ref := map[string]any{"kind": "db_conn", "id": id}
	act := func(aid, label, op string) map[string]any {
		return map[string]any{"id": aid, "label": label, "kind": "op", "op": op, "ref": ref}
	}
	var acts []any
	if fix {
		acts = append(acts, act("fix", "Fix password…", "db.fix_password"))
	}
	acts = append(acts, act("connect", "Connect", "db.connect"), act("open", "Open in Databases", "db.open"),
		act("tables", "List tables", "db.tables"), act("query", "New query", "db.new_query"), act("test", "Test", "db.test"))
	return map[string]any{"id": id, "title": title, "badge": badge, "tone": tone, "sub": sub, "ref": ref, "actions": acts}
}

// dbCardBlock is the owner's three connections (fake data).
func dbCardBlock(id string) map[string]any {
	return map[string]any{"id": id, "kind": "list", "title": "Databases · 3 connections", "source": "db_connections",
		"for_you": "Tap a row to open it in Databases.",
		"items": []any{
			dbCardItem("baaytonadb", "BaaytonaDB", "unreachable", "bad", "SSH to deploy@65.109.212.14 works, but nothing answers at 127.0.0.1:5432 on that server (connection refused). · postgres 127.0.0.1:5432/baaytona · via ssh deploy@65.109.212.14", false),
			dbCardItem("baaytona-db-1", "baaytona-db-1", "ready", "idle", "postgres baaytona-db-1:5432/baaytona · container baaytona-db-1 on context Baaytona (ssh://deploy@65.109.212.14)", false),
			dbCardItem("rentcar-db-1", "rentcar-db-1", "missing password", "warn", "nothing saved · postgres rentcar-db-1:5432/rentcar · container rentcar-db-1 on context Baaytona", true),
		}}
}

// dbCardChat is the owner's question and the answer of a one-step
// profile whose step called db_connections.
func dbCardChat() map[string]any {
	reply := map[string]any{"i": 1, "role": "assistant", "model": "gpt-6-sol",
		"md":   md("doc", mdPara("None of them is connected now. rentcar-db-1 has no password: set it with Fix…, then Connect.")),
		"meta": map[string]any{"elapsed_ms": 21000, "step": "Step 1", "cli": "Codex"},
		"steps": []any{map[string]any{"id": "step-1", "title": "Step 1", "model": "Codex · gpt-6-sol", "state": "completed",
			"tools": []any{"⚙ db_connections → 9 lines"}, "blocks": []any{dbCardBlock("call_1")}}}}
	return chatWith(false, userMsg(0, "Which remote databases do we have connected?"), reply)
}

// dbCardShot writes WEB_SHOTS_DIR/ai-db-card-<name>.png when it is set.
func dbCardShot(t *testing.T, p *Page, name, ready string) {
	t.Helper()
	if dir := os.Getenv(shotsEnv); dir != "" {
		snap(t, p, filepath.Join(dir, "ai-db-card-"+name+".png"), ready)
	}
}

const dbCard = `document.querySelector('[data-view="chat"] .dbcard')`

func dbCardRow(id string) string {
	return dbCard + `?.querySelector('.dbcard-row[data-conn="` + id + `"]')`
}

func dbCardAct(id, label string) string {
	return `[...(` + dbCardRow(id) + `?.querySelectorAll(".dbcard-act") ?? [])].find((b) => b.textContent === ` + jsq(label) + `)`
}

func TestAIDBCardDrawsTheTrueState(t *testing.T) {
	for _, vp := range []Viewport{phoneSmall, Desktop} {
		t.Run(vp.Name, func(t *testing.T) {
			p, _ := openChat(t, vp, dbCardChat())
			p.WaitJS(dbCard + `?.querySelectorAll(".dbcard-row").length === 3`)
			for id, badge := range map[string]string{"baaytonadb": "unreachable", "baaytona-db-1": "ready", "rentcar-db-1": "missing password"} {
				if got := Eval[string](p, dbCardRow(id)+`.querySelector(".dbcard-badge")?.textContent ?? ""`); got != badge {
					t.Errorf("%s badge %q, want %q", id, got, badge)
				}
			}
			if !Eval[bool](p, dbCardRow("baaytonadb")+`.textContent.includes("nothing answers at 127.0.0.1:5432")`) {
				t.Error("BaaytonaDB does not say why")
			}
			acts := func(id string) string {
				return Eval[string](p, `[...`+dbCardRow(id)+`.querySelectorAll(".dbcard-act")].map((b) => b.textContent).join("|")`)
			}
			if got := acts("rentcar-db-1"); got != "Fix password…|Connect|Open in Databases|List tables|New query|Test|+ Chat" {
				t.Errorf("rentcar-db-1 buttons %q", got)
			}
			if got := acts("baaytonadb"); strings.Contains(got, "Fix") {
				t.Errorf("BaaytonaDB has a Fix: %q", got)
			}
			if vp.Mobile {
				// 44 px targets and no sideways scroll on the small phone.
				if !Eval[bool](p, `[...`+dbCard+`.querySelectorAll("button")].every((b) => b.getBoundingClientRect().height >= 32)`) {
					t.Error("a card button is under 32 px high")
				}
				if Eval[bool](p, `document.documentElement.scrollWidth > innerWidth + 1`) {
					t.Error("the page scrolls sideways")
				}
			}
			dbCardShot(t, p, vp.Name, dbCard)
			if probs := p.Problems(); len(probs) > 0 {
				t.Errorf("problems (CSP, console): %v", probs)
			}
		})
	}
}

// Connect and Test ask the laptop with the page's own ops (the Databases
// view's: its confirm rules hold there) and say the answer.
func TestAIDBCardConnectAndTest(t *testing.T) {
	for _, vp := range []Viewport{phoneSmall, Desktop} {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openChat(t, vp, dbCardChat())
			p.WaitJS(dbCardRow("baaytona-db-1"))
			p.TapJS(dbCardAct("baaytona-db-1", "Connect"))
			reply(t, c, expectOp(t, c, webproto.OpDBConnect, `{"conn":"baaytona-db-1"}`), webproto.DBDone{Text: "Connecting baaytona-db-1 on the laptop."})
			p.WaitJS(`[...document.querySelectorAll(".toast")].some((e) => e.textContent.includes("Connecting baaytona-db-1"))`)
			p.TapJS(dbCardAct("baaytonadb", "Test"))
			dbFail(t, c, expectOp(t, c, webproto.OpDBTest, `{"conn":"baaytonadb"}`), "nothing answers at 127.0.0.1:5432", nil)
			p.WaitJS(`[...document.querySelectorAll(".toast")].some((e) => e.textContent.includes("nothing answers at 127.0.0.1:5432"))`)
		})
	}
}

// A tap on the row (or Open, List tables, Fix…) opens Databases on that
// connection.
func TestAIDBCardRowOpensDatabases(t *testing.T) {
	for _, vp := range []Viewport{phoneSmall, Desktop} {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openChat(t, vp, dbCardChat())
			p.WaitJS(dbCardRow("rentcar-db-1"))
			p.TapJS(dbCardRow("rentcar-db-1") + `.querySelector(".dbcard-head")`)
			if vp.Mobile {
				p.WaitJS(`location.hash === "#/more/db/rentcar-db-1"`)
				return
			}
			p.WaitJS(`location.hash === "#/more/db"`)
			reply(t, c, expectOp(t, c, webproto.OpDBConns, `{}`), webproto.DBConns{Conns: []webproto.DBConn{{ID: "rentcar-db-1", Driver: "postgres", SQL: true}}})
			replyLocalHosts(t, c)
			expectOp(t, c, webproto.OpDBTables, `{"conn":"rentcar-db-1"}`)
		})
	}
}

// + Chat puts the row into the next message: on the desktop as the
// connection (an item chip), on the phone as a reference chip.
func TestAIDBCardAddsTheRowToTheChat(t *testing.T) {
	for _, vp := range []Viewport{phoneSmall, Desktop} {
		t.Run(vp.Name, func(t *testing.T) {
			p, _ := openChat(t, vp, dbCardChat())
			p.WaitJS(dbCardRow("rentcar-db-1"))
			p.TapJS(dbCardAct("rentcar-db-1", "+ Chat"))
			p.WaitJS(`document.querySelector('[data-view="chat"] .composer')?.textContent.includes("rentcar-db-1")`)
		})
	}
}
