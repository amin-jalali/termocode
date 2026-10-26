package app

import (
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Itermoc: the service page (B3) ───────────────────────────────────────
//
//	row 0   ⚙ linear  remote service · MCP                     ! sign in
//	        ╭ ! Sign-in needed ─────────────────────────────╮
//	        │ linear waits for you to sign in.              │
//	        │ The chat lost 12 tools. Sign in to get them…  │
//	        │ [Sign in (i)]  opens the browser · …          │
//	        ╰───────────────────────────────────────────────╯
//	        STATUS   state · where · tools · access · sign-in · trusted
//	        TOOLS    the tool names as chips
//	h-3     Show tools (t)  Disable (d)  Log (l)   ⋯ more (Enter): …
//
// Every verb is a flow that exists without this page (the services
// actions of #3): Disable asks on a card first, Remove… asks on its own
// card, Enable is safe and runs at once. A token or secret is never on the
// page; the tool names are what the server says it offers.

// imSvc is what the service page adds to a service's status. Nothing in
// it is secret.
type imSvc struct {
	where   string   // "remote · api.linear.app" / "local · npx"
	access  string   // "read only" / "writes allowed" ("" = the service decides)
	account string   // "gh · octocat" ("" = none)
	browser bool     // it signs in through the browser (Sign in is offered)
	tools   []string // the known tool names (live or from the last run)
	file    string   // the file the entry lives in
}

// itermocServiceInfo reads what the page shows about one service from
// its mcp.json entry and the tools the manager knows.
func itermocServiceInfo(st mcp.Status, mgr *mcp.Manager) imSvc {
	var in imSvc
	entries := mcpServerEntries(st.Name)
	if len(entries) > 0 {
		e := entries[0]
		in.file = e.SourcePath
		if e.Type == mcp.TypeStdio {
			in.where = "local · " + filepath.Base(e.Command) // the command only: args may hold a token
		} else if u, err := url.Parse(e.URL); err == nil && u.Host != "" {
			in.where = "remote · " + u.Host
		} else {
			in.where = "remote"
		}
		if p, ok := mcpPresetFor(st.Name); ok && (p.ReadOnlyURL != "" || p.WriteScopes != nil) {
			if mcpEntryReadOnly(e, p) {
				in.access = "read only"
			} else {
				in.access = "writes allowed · each write asks first"
			}
		} else if e.ReadOnly {
			in.access = "read only"
		}
		if e.Auth != nil && e.Auth.GH != "" {
			in.account = "GitHub CLI · " + e.Auth.GH
		}
		in.browser = st.Type == mcp.TypeHTTP && e.Auth == nil && !mcpHasAuthHeader(e)
	}
	if mgr != nil {
		if _, tools, ok := mgr.KnownTools(st.Name); ok {
			for _, t := range tools {
				in.tools = append(in.tools, t.Name)
			}
		}
	}
	return in
}

// imSvcSignIn reports whether the service waits for the user to sign in.
func imSvcSignIn(s mcp.Status) bool { return s.Expired || s.State == mcp.StateSignIn }

// imSvcState is the state row in words.
func imSvcState(s mcp.Status) (string, theme.Color256) {
	switch {
	case s.State == mcp.StateDisabled:
		return "off · kept in mcp.json, not started, no tools", theme.TextMuted
	case imSvcSignIn(s):
		return "waits for a sign-in", theme.AccentAmber
	case s.State == mcp.StateFailed:
		return "failed to start", theme.AccentRedCoral
	case s.State == mcp.StateInvalid:
		return "can not run", theme.AccentRedCoral
	case s.State == mcp.StateRunning:
		return "running", theme.AccentGreen
	case s.State == mcp.StateStopped:
		return "stopped · starts when the chat needs it", 0
	}
	return s.State, 0
}

// servicePageContent is the service page: its state, what the chat gets
// from it, and a "needs you" card when it waits for the user.
func servicePageContent(sn itermocSnap, row imRow) imContent {
	s, _ := sn.service(row.name)
	in := sn.svc[s.Name]
	c := imContent{icon: imIconServices.String(), name: s.Name}
	if s.Type == mcp.TypeStdio {
		c.sub = "local service · MCP"
	} else {
		c.sub = "remote service · MCP"
	}
	word, _ := itermocServiceWord(s)
	g, _ := imGlyph(itermocServiceGlyph(s))
	c.right = g + " " + word
	switch {
	case itermocServiceNeeds(s) || !s.Trusted:
		c.rightTok = theme.AccentAmber
	case s.State == mcp.StateRunning:
		c.rightTok = theme.AccentGreen
	default:
		c.rightTok = theme.TextMuted
	}

	// The "needs you" card: one at a time, the most urgent first.
	switch {
	case s.State == mcp.StateDisabled:
	case imSvcSignIn(s):
		title := "! Sign-in needed"
		first := s.Name + " waits for you to sign in."
		if s.Expired {
			title = "! Sign-in ended"
			first = "The sign-in of " + s.Name + " has ended."
		}
		lines := []string{first}
		if s.Tools > 0 {
			lines = append(lines, "The chat lost "+strconv.Itoa(s.Tools)+" tools. Sign in again to get them back.")
		} else {
			lines = append(lines, "Until then the chat can not use it.")
		}
		note := "the AI never sees the token"
		if in.browser {
			note = "opens the browser · " + note
		}
		c.card = &imCard{title: title, lines: lines,
			pills: []imPill{{verb: "signin", label: "Sign in", key: "i", warn: true}}, note: note}
	case s.State == mcp.StateFailed || s.State == mcp.StateInvalid:
		lines := []string{s.Name + " does not start, so the chat has none of its tools."}
		if s.Problem != "" {
			lines = append(lines, s.Problem)
		}
		c.card = &imCard{title: "! It does not start", lines: lines,
			pills: []imPill{{verb: "log", label: "Log", key: "l", warn: true}},
			note:  "or Disable it until you fix the entry"}
	case !s.Trusted:
		c.card = &imCard{title: "! Not trusted yet", lines: []string{
			s.Name + " runs only after you trust this entry.",
			"Trust is in the actions menu (Enter); it asks again when the entry changes.",
		}}
	case s.ToolsChanged:
		c.card = &imCard{title: "! Its tools changed", lines: []string{
			"Until you trust the new list, each of its tools asks first.",
			"Show tools lists them; Trust the new tools is in the actions menu.",
		}}
	}

	// STATUS.
	state, stTok := imSvcState(s)
	rows := [][2]string{{"state", state}}
	toks := []theme.Color256{stTok}
	add := func(label, value string, tok theme.Color256) {
		rows = append(rows, [2]string{label, value})
		toks = append(toks, tok)
	}
	if in.where != "" {
		add("where", in.where, 0)
	}
	tools := strconv.Itoa(s.Tools)
	switch {
	case s.Tools > 0 && imSvcSignIn(s):
		tools += " · back after the sign-in"
	case s.Cached && s.Tools > 0:
		tools += " · from the last run"
	}
	add("tools", tools, 0)
	if in.access != "" {
		add("access", in.access, 0)
	} else if in.file != "" && s.State != mcp.StateDisabled {
		add("access", "what the service allows", 0)
	}
	if in.account != "" {
		add("account", in.account, 0)
	}
	switch {
	case !in.browser:
	case s.Expired:
		add("sign-in", "ended · sign in again", theme.AccentAmber)
	case s.SignedIn:
		add("sign-in", "stored in the secret store", theme.AccentGreen)
	default:
		add("sign-in", "not yet", theme.AccentAmber)
	}
	if s.Trusted {
		add("trusted", "yes", 0)
	} else {
		add("trusted", "not yet", theme.AccentAmber)
	}
	src := "yours"
	if s.Source == mcp.SourceProject {
		src = "this project"
	}
	if in.file != "" {
		src += " · " + imHome(in.file)
	}
	add("entry", src, 0)
	c.facts = []imFactSec{{title: "STATUS", rows: rows, toks: toks}}
	if len(in.tools) > 0 {
		note := strconv.Itoa(len(in.tools)) + " the chat can use"
		switch {
		case s.State == mcp.StateDisabled:
			note = strconv.Itoa(len(in.tools)) + " · none while it is off"
		case imSvcSignIn(s) || s.State == mcp.StateFailed || s.State == mcp.StateInvalid:
			note = strconv.Itoa(len(in.tools)) + " · none until it works again"
		}
		c.facts = append(c.facts, imFactSec{title: "TOOLS", right: note, chips: in.tools})
	}

	// The pills: three at most; the rest is in the menu.
	switch {
	case s.State == mcp.StateDisabled:
		c.pills = []imPill{
			{verb: "enable", label: "Enable", key: "d", primary: true},
			{verb: "remove", label: "Remove…", key: "D"},
		}
	case imSvcSignIn(s):
		c.pills = []imPill{
			{verb: "signin", label: "Sign in", key: "i", warn: true},
			{verb: "tools", label: "Show tools", key: "t"},
			{verb: "disable", label: "Disable…", key: "d"},
		}
	default:
		c.pills = []imPill{
			{verb: "tools", label: "Show tools", key: "t", primary: true},
			{verb: "disable", label: "Disable…", key: "d"},
			{verb: "log", label: "Log", key: "l"},
		}
	}
	var more []string
	if s.State != mcp.StateDisabled {
		more = append(more, "Connect / Refresh")
		if !s.Trusted {
			more = append(more, "Trust")
		}
		if s.ToolsChanged {
			more = append(more, "Trust the new tools")
		}
		switch in.access {
		case "read only":
			more = append(more, "Allow writes")
		case "":
		default:
			more = append(more, "Read only")
		}
		if s.SignedIn {
			more = append(more, "Sign out")
		}
	}
	more = append(more, "Edit in mcp.json")
	if s.State != mcp.StateDisabled {
		more = append(more, "Remove…")
	}
	c.more = strings.Join(more, " · ")
	c.hint = "t tools · d disable / enable · D remove · Enter more · Esc list · ? keys"
	if imSvcSignIn(s) {
		c.hint = "i sign in · " + c.hint
	}
	c.hintNarrow = "Enter more · Esc list · ? keys"
	return c
}

// imHome writes a path under the home folder as ~/….
func imHome(p string) string {
	if h, err := os.UserHomeDir(); err == nil && h != "" && strings.HasPrefix(p, h+string(filepath.Separator)) {
		return "~" + p[len(h):]
	}
	return p
}
