package app

import (
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Itermoc: the overview ────────────────────────────────────────────────
//
// The list's top row "Overview" shows one calm page:
//
//	✦ AI  profiles, models, services and outside agents     listening auto
//	╭ ✦ PROFILES ─ 1 ╮ ╭ ϟ MODELS ─ 2 ╮ ╭ ⌁ SERVICES ─ 3 ╮ ╭ ⇄ OUTSIDE ─ 4 ╮
//	NEEDS YOU  2 things
//	! linear        sign-in expired · the chat lost 12 tools   ▐ Sign in (i) ▌
//	⚠ house-review  a profile from this project · not trusted  ▐ Trust… (t) ▌
//	ACTIVE FLOW  ✦ review · Review then fix
//	you ──▶ 1 Reviewer ──▶ 2 Fixer ──▶ answer
//
// 1–4 (or a click on a tile) go into that group; Tab picks a "needs you"
// line and Enter (or a click on it) opens its page; its pill runs the fix.

// imNeed is one thing that waits for the user.
type imNeed struct {
	row        imRow
	glyph      string
	tok        theme.Color256
	name, what string
	fix        imPill
}

// needs lists what waits for the user, the most urgent first: services
// (the chat lost their tools), then models without a key, then project
// profiles not trusted yet. The same rules as the group badges.
func (sn itermocSnap) needs() []imNeed {
	var out []imNeed
	for _, s := range sn.services {
		if !itermocServiceNeeds(s) {
			continue
		}
		g, tok := imGlyph(itermocServiceGlyph(s))
		n := imNeed{row: imRow{kind: imRowService, sec: imSecServices, name: s.Name}, glyph: g, tok: tok, name: s.Name}
		switch {
		case s.State == mcp.StateFailed || s.State == mcp.StateInvalid:
			n.what = "does not start · the chat has none of its tools"
			n.fix = imPill{verb: "log", label: "Log", key: "l", warn: true}
		default:
			n.what = "waits for a sign-in"
			if s.Expired {
				n.what = "sign-in expired"
			}
			if s.Tools > 0 {
				n.what += " · the chat lost " + strconv.Itoa(s.Tools) + " tools"
			}
			n.fix = imPill{verb: "signin", label: "Sign in", key: "i", warn: true}
		}
		out = append(out, n)
	}
	for _, s := range sn.slots {
		if s.key != imKeyMissing && s.key != imKeyUnset {
			continue
		}
		g, tok := imGlyph("needs")
		n := imNeed{row: imRow{kind: imRowSlot, sec: imSecModels, name: s.name}, glyph: g, tok: tok, name: s.name,
			what: "no key · this model cannot answer yet", fix: imPill{verb: "setkey", label: "Set key", key: "k", warn: true}}
		if s.provider == "" {
			n.what = "no provider set"
		}
		if users := sn.slotUsers(s.name); len(users) > 0 {
			n.what += " · used by " + strings.Join(users, ", ")
		}
		out = append(out, n)
	}
	for _, p := range sn.profiles {
		if p.trust != aiTrustAsk {
			continue
		}
		g, tok := imGlyph("untrusted")
		out = append(out, imNeed{row: imRow{kind: imRowProfile, sec: imSecProfiles, name: p.p.Name}, glyph: g, tok: tok,
			name: p.p.Name, what: "a profile from this project · not trusted yet",
			fix: imPill{verb: "trust", label: "Trust…", key: "t", warn: true}})
	}
	return out
}
