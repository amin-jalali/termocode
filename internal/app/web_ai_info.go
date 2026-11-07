package app

import (
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/aihooks"
	"github.com/amin-jalali/termocode/internal/aimemory"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/webtools"
)

// The laptop's AI screens on the web (web parity, AI screens): ai.info
// builds the facts each laptop screen shows (AI: Usage, Project Rules…,
// Memory…, Skills…, Hooks…, Web Tools…, MCP: Show Log, MCP: Server
// Status…, Copy termocode Server Config, Configure Provider…, Help: Build
// Bug Report…) as read-only rows or text. Nothing opens on the laptop and
// nothing changes. Every string passes the registered-secret redaction;
// keys are only "set" or "not set"; a base URL shows its host and path.

func init() {
	webOps[webproto.OpAIInfo] = (*Model).webAIInfo
}

// webAIInfoBuild is one screen's builder.
type webAIInfoBuild func(m *Model) webproto.AIInfo

var webAIInfoBuilders = map[string]webAIInfoBuild{
	webproto.AIInfoUsage:       (*Model).webAIInfoUsage,
	webproto.AIInfoRules:       (*Model).webAIInfoRules,
	webproto.AIInfoMemory:      (*Model).webAIInfoMemory,
	webproto.AIInfoSkills:      (*Model).webAIInfoSkills,
	webproto.AIInfoHooks:       (*Model).webAIInfoHooks,
	webproto.AIInfoWebTools:    (*Model).webAIInfoWebTools,
	webproto.AIInfoMCPLog:      (*Model).webAIInfoMCPLog,
	webproto.AIInfoServeStatus: (*Model).webAIInfoServeStatus,
	webproto.AIInfoServeConfig: (*Model).webAIInfoServeConfig,
	webproto.AIInfoConfigure:   (*Model).webAIInfoConfigure,
	webproto.AIInfoBugReport:   (*Model).webAIInfoBugReport,
}

func (m *Model) webAIInfo(call *webCall) tea.Cmd {
	build := webAIInfoBuilders[call.args.(*webproto.AIInfoArgs).Kind]
	if build == nil {
		call.fail(webproto.CodeBadRequest, "There is no such screen.")
		return nil
	}
	call.reply(webAIInfoClean(build(m)))
	return nil
}

// webAIInfoClean hides registered secrets and caps the screen.
func webAIInfoClean(in webproto.AIInfo) webproto.AIInfo {
	in.Title, in.Note = ai.RedactSecrets(in.Title), ai.RedactSecrets(in.Note)
	if len(in.Text) > webproto.MaxAIInfoText {
		in.Text = in.Text[len(in.Text)-webproto.MaxAIInfoText:]
	}
	in.Text = ai.RedactSecrets(in.Text)
	if len(in.Rows) > webproto.MaxAIInfoRows {
		in.Rows = in.Rows[:webproto.MaxAIInfoRows]
	}
	rows := make([]webproto.AIInfoRow, 0, len(in.Rows))
	for _, r := range in.Rows {
		rows = append(rows, webproto.AIInfoRow{Title: ai.RedactSecrets(r.Title), Hint: ai.RedactSecrets(r.Hint), Header: r.Header})
	}
	in.Rows = rows
	return in
}

// webInfoRows helps build rows.
type webInfoRows []webproto.AIInfoRow

func (r *webInfoRows) head(title string) {
	*r = append(*r, webproto.AIInfoRow{Title: title, Header: true})
}

func (r *webInfoRows) row(title, hint string) {
	*r = append(*r, webproto.AIInfoRow{Title: title, Hint: hint})
}

func webOnOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func webTrustWord(trust string) string {
	switch trust {
	case aiTrustUse:
		return "trusted: the AI uses them"
	case aiTrustRefused:
		return "refused: not used"
	}
	return "not trusted yet: not used until you trust them on the laptop"
}

func (m *Model) webAIInfoUsage() webproto.AIInfo {
	c := m.usageBuild(100)
	lines := make([]string, 0, len(c.head)+len(c.body))
	for _, l := range append(append([]string{}, c.head...), c.body...) {
		lines = append(lines, strings.TrimRight(ansi.Strip(l), " "))
	}
	return webproto.AIInfo{Title: "AI Usage", Note: "Tokens and cost by model and by agent", Rows: []webproto.AIInfoRow{}, Text: strings.Join(lines, "\n")}
}

func (m *Model) webAIInfoRules() webproto.AIInfo {
	r := ai.LoadProjectRules(aiWorkspaceRoot())
	out := webproto.AIInfo{Title: "Project Rules", Rows: []webproto.AIInfoRow{}}
	if r.Hash == "" {
		out.Note = "No AGENTS.md, CLAUDE.md or other rules file in this workspace"
		return out
	}
	out.Note = "Rules " + webTrustWord(aiRulesTrust(r))
	var rows webInfoRows
	rows.head("Files")
	for _, f := range r.Files {
		rows.row(f, "")
	}
	for _, s := range r.Scoped {
		switch s.Mode { // CMP-82
		case ai.RuleModeManual:
			rows.row(s.File, "when you write #"+s.Name)
		case ai.RuleModeAuto:
			rows.row(s.File, "the AI loads it by its description")
		default:
			rows.row(s.File, "for some files only")
		}
	}
	size := humanSize(int64(r.Bytes))
	if r.Truncated {
		size += ", cut"
	}
	rows.row("Size: "+size, "")
	out.Rows, out.Text = rows, ai.Redact(r.Text)
	return out
}

func (m *Model) webAIInfoMemory() webproto.AIInfo {
	out := webproto.AIInfo{Title: "AI Memory", Note: "Memory is " + webOnOff(aiMemoryState() == aiMemoryOn) + " in this workspace", Rows: []webproto.AIInfoRow{}}
	ls, err := m.aiS().aiMemoryLoad(aiWorkspaceRoot())
	if err != nil {
		out.Note = "Could not read " + aiMemoryRel
		return out
	}
	var rows webInfoRows
	if len(ls) > 0 {
		rows.head("Lessons")
	}
	for i := len(ls) - 1; i >= 0; i-- {
		l := ls[i]
		rows.row(l.Date+" · "+aimemory.CleanLine(l.Title, aimemory.MaxTitleRunes), strings.Join(l.Paths, ", "))
	}
	if root := aimemory.SharedRoot(aiWorkspaceRoot()); root != "" {
		if shared, _ := aimemory.LoadForWorktree(root); len(shared) > 0 {
			rows.row("Main checkout lessons (read only)", strconv.Itoa(len(shared))+" shared lessons")
		}
	}
	if len(rows) == 0 {
		rows.row("No lessons in this workspace yet", "")
	}
	out.Rows = rows
	return out
}

func (m *Model) webAIInfoSkills() webproto.AIInfo {
	s := aiLoadSkills(aiWorkspaceRoot())
	if f := m.aiS().skillsFake; f != nil {
		s = *f
	}
	var rows webInfoRows
	group := func(source, title string) {
		first := true
		for _, sk := range s.List {
			if sk.Source != source {
				continue
			}
			if first {
				rows.head(title)
				first = false
			}
			rows.row(sk.Name, agentdag.Clean(sk.Description))
		}
	}
	project := "Project skills"
	if aiSkillsTrust(s) != aiTrustUse {
		project += " · not offered until trusted"
	}
	group(ai.SkillSourceProject, project)
	group(ai.SkillSourceUser, "User skills")
	group(ai.SkillSourceBuiltin, "Built-in skills")
	for i, c := range aiLoadChatCommands(aiWorkspaceRoot()).List {
		if i == 0 {
			rows.head("Chat commands · type /name in the chat")
		}
		rows.row("/"+c.Name, agentdag.Clean(c.Description))
	}
	if len(rows) == 0 {
		rows.row("No skills yet", "add one under "+ai.SkillsDir)
	}
	return webproto.AIInfo{Title: "AI Skills", Rows: rows}
}

func (m *Model) webAIInfoHooks() webproto.AIInfo {
	s := aiLoadHooks(aiWorkspaceRoot())
	out := webproto.AIInfo{Title: "AI Hooks", Note: "Commands that run after AI tool calls", Rows: []webproto.AIInfoRow{}}
	var rows webInfoRows
	for _, e := range s.Errors {
		rows.row("Problem: "+agentdag.Clean(e), "")
	}
	section := func(title string, hooks []webHookRow) {
		if len(hooks) == 0 {
			return
		}
		rows.head(title)
		for _, h := range hooks {
			rows.row(h.run, h.tools)
		}
	}
	var user, project []webHookRow
	for _, h := range s.All() {
		row := webHookRow{run: agentdag.Clean(h.Run), tools: aiHookWhen(h)}
		if h.Source == aihooks.SourceProject {
			project = append(project, row)
		} else {
			user = append(user, row)
		}
	}
	if s.ProjectHash != "" {
		section("Project hooks · "+webTrustWord(aiHooksTrust(s)), project)
	}
	section("User hooks", user)
	if len(rows) == 0 {
		rows.row("No hooks", "add "+aihooks.ProjectFile+" or your user hooks file")
	}
	out.Rows = rows
	return out
}

type webHookRow struct{ run, tools string }

func (m *Model) webAIInfoWebTools() webproto.AIInfo {
	s := loadAIWebSettings()
	var rows webInfoRows
	rows.head("Built in (off until you turn them on)")
	rows.row("Web fetch: "+webOnOff(s.Fetch), "the AI may read web pages (not local addresses)")
	search := "off"
	for _, p := range webtools.Providers {
		hint := "needs a key"
		if k, _ := aiWebSearchKey(p); k != "" {
			hint = "key set"
		}
		if s.Search == p.Name {
			search = p.Title
		}
		rows.row("Search provider: "+p.Title, hint)
	}
	rows.row("Web search: "+search, "")
	rows.head("Servers you can add on the laptop")
	for _, p := range aiWebPresets() {
		rows.row(p.title, p.blurb)
	}
	return webproto.AIInfo{Title: "AI Web Tools", Note: "Searches send your query to the provider; each call is in the AI audit log.", Rows: rows}
}

func (m *Model) webAIInfoMCPLog() webproto.AIInfo {
	lines := m.output.Lines(mcpLogChannel)
	const max = 400
	if len(lines) > max {
		lines = lines[len(lines)-max:]
	}
	out := webproto.AIInfo{Title: "MCP Log", Rows: []webproto.AIInfoRow{}, Text: strings.Join(lines, "\n")}
	if len(lines) == 0 {
		out.Note = "Nothing logged yet"
	}
	return out
}

func (m *Model) webAIInfoServeStatus() webproto.AIInfo {
	var rows webInfoRows
	for _, it := range m.mcpServeStatusItems() {
		if it.Header && it.Title == "Actions" {
			break
		}
		if it.Header {
			rows.head(it.Title)
		} else if !strings.HasPrefix(it.ID, "serve-") || it.ID == "" {
			rows.row(it.Title, "")
		}
	}
	return webproto.AIInfo{Title: "MCP Server Status", Note: "Outside agents that use termocode's MCP server", Rows: rows}
}

func (m *Model) webAIInfoServeConfig() webproto.AIInfo {
	exe, err := mcpServeExecutable()
	if err != nil || exe == "" {
		exe = "termocode"
	}
	return webproto.AIInfo{Title: "termocode Server Config", Note: "Paste it into your agent's MCP settings", Rows: []webproto.AIInfoRow{},
		Text: mcpServeConfigJSON(exe, aiWorkspaceRoot()), Copy: true}
}

// webBaseURLShown is a base URL without its user, password, query and
// fragment ("" when there is none).
func webBaseURLShown(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(set)"
	}
	return u.Scheme + "://" + u.Host + u.Path
}

func (m *Model) webAIInfoConfigure() webproto.AIInfo {
	s := m.aiS()
	var rows webInfoRows
	rows.head("Provider")
	kind := s.webAIKind()
	prov := "not set up"
	if kind != "" {
		prov = ai.KindLabel(kind)
	}
	rows.row("Provider: "+prov, "")
	model := s.cfg.Model
	if model == "" {
		model = "(the provider's default)"
	}
	rows.row("Model: "+webName(model), "")
	if b := webBaseURLShown(s.cfg.BaseURL); b != "" {
		rows.row("Base URL: "+b, "")
	}
	key := "not set"
	if s.cfg.APIKey != "" || s.cfg.Token != nil {
		key = "set (kept on the laptop)"
	}
	rows.row("Key: "+key, "")
	rows.row("Ready: "+webOnOff(m.aiConfigured()), "")
	return webproto.AIInfo{Title: "AI Provider", Note: "Change the provider and keys on the laptop: keys never leave it.", Rows: rows}
}

func (m *Model) webAIInfoBugReport() webproto.AIInfo {
	path, _ := settingsConfigPath()
	logs := string(reportRead(filepath.Join(filepath.Dir(path), "errors.log"), true))
	if logs == "" {
		logs = strings.Join(errorEntries(), "\n")
	}
	body := buildBugReport(reportRead(path, false), logs, "")
	return webproto.AIInfo{Title: "Bug Report", Note: "Review before saving. Check for private text. Nothing is sent.", Rows: []webproto.AIInfoRow{},
		Text: ai.Redact(body), Save: "termocode-report-" + clock.Now().Format("2006-01-02") + ".md"}
}
