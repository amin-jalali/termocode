package app

import (
	"context"
	"errors"
	"sort"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// MCP default views on the phone (MOBUX-14): mcp.views lists every
// configured server's default views from the known tool lists (memory
// or the disk cache, never starting a server); mcp.view_run runs one
// read-only, trusted tool (or reads a resource) and answers the shaped
// data with the raw JSON beside it. Anything the phone may not run
// answers Ask, the one line the page shows beside "Ask the AI". A server
// that is not trusted is never started from the phone (the trust card is
// the laptop's: the phone's answer is always Deny).

func init() {
	webOps[webproto.OpMCPViews] = (*Model).webMCPViews
	webOps[webproto.OpMCPViewRun] = (*Model).webMCPViewRun
}

// webMCPViewWait caps one run (a server start included).
const webMCPViewWait = 45 * time.Second

// webMCPPhoneAsk is the phone's trust answer: never (trust on the laptop).
func webMCPPhoneAsk(context.Context, mcp.Server) mcp.TrustAnswer { return mcp.TrustDeny }

// webMCPViewRule is the read-only run rule of the current chat.
func (m *Model) webMCPViewRule() mcp.ViewRule {
	set := mcp.LoadSettings(mcpConfigPath())
	return mcp.ViewRule{TrustAnnotations: set.TrustAnnotations, Profile: m.mcpActiveProfile(), Off: set.ToolOff}
}

func (m *Model) webMCPViews(call *webCall) tea.Cmd {
	mgr := m.aiMCP()
	rule := m.webMCPViewRule()
	statuses := mgr.Servers()
	call.later()
	return func() tea.Msg {
		sort.Slice(statuses, func(i, j int) bool { return statuses[i].Name < statuses[j].Name })
		out := webproto.MCPViews{Servers: []webproto.MCPViewServer{}}
		for _, st := range statuses {
			if len(out.Servers) == webproto.MaxMCPViewServers {
				break
			}
			cfg, _, _ := mgr.KnownTools(st.Name)
			srv := webproto.MCPViewServer{Name: webName(st.Name), Off: cfg.Disabled, Views: []webproto.MCPViewInfo{}}
			views, ok := mgr.ServerViews(st.Name, rule)
			srv.Known = ok
			for _, v := range views {
				if len(srv.Views) == webproto.MaxMCPViewsPer {
					break
				}
				info := webMCPViewInfo(v)
				if info.Run && (!st.Trusted || st.ToolsChanged) {
					info.Run, info.Why = false, webMCPTrustWhy(st)
				}
				srv.Views = append(srv.Views, info)
			}
			out.Servers = append(out.Servers, srv)
		}
		call.reply(out)
		return nil
	}
}

func webMCPTrustWhy(st mcp.Status) string {
	if st.ToolsChanged {
		return "its tools changed: review them on the laptop"
	}
	return "trust this server on the laptop first"
}

func (m *Model) webMCPViewRun(call *webCall) tea.Cmd {
	a := call.args.(*webproto.MCPViewRunArgs)
	server, tool, ok := mcp.ParseToolViewID(a.View)
	if !ok {
		call.fail(webproto.CodeBadRequest, "That is not an MCP view.")
		return nil
	}
	mgr := m.aiMCP()
	rule := m.webMCPViewRule()
	var trusted, changed bool
	for _, st := range mgr.Servers() {
		if st.Name == server {
			trusted, changed = st.Trusted, st.ToolsChanged
		}
	}
	in := make(map[string]string, len(a.Args))
	for _, x := range a.Args {
		in[x.ID] = x.Value
	}
	compact := a.Compact
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, webMCPViewWait)
		defer cancel()
		views, known := mgr.ServerViews(server, rule)
		if !known {
			call.fail(webproto.CodeNotFound, "The laptop does not know this server's tools yet. Connect it on the laptop first.")
			return nil
		}
		var v mcp.ToolView
		found := false
		for _, x := range views {
			if x.ID == a.View {
				v, found = x, true
			}
		}
		if !found {
			call.fail(webproto.CodeNotFound, "There is no such view.")
			return nil
		}
		out := webproto.MCPViewData{View: v.ID, Title: webName(v.Title), Layout: v.Layout}
		switch {
		case !v.Run:
			out.Ask = webText(v.Why)
		case !trusted || changed:
			out.Ask = webText(webMCPTrustWhy(mcp.Status{ToolsChanged: changed}))
		}
		if out.Ask != "" {
			out.Layout, out.Raw = mcp.LayoutJSON, ""
			call.reply(out)
			return nil
		}
		args, err := mcp.ViewArgs(v, in)
		if err != nil {
			call.fail(webproto.CodeBadRequest, webText(err.Error()))
			return nil
		}
		var d mcp.ViewData
		if tool == mcp.ResourceViewTool {
			uri, _ := args["uri"].(string)
			contents, rerr := mgr.ReadResource(ctx, webMCPPhoneAsk, server, uri)
			if rerr != nil {
				call.fail(webMCPErrCode(rerr), webText(rerr.Error()))
				return nil
			}
			d = mcp.ShapeResource(contents, compact)
		} else {
			res, cerr := mgr.Call(ctx, webMCPPhoneAsk, server, tool, args)
			if cerr != nil {
				call.fail(webMCPErrCode(cerr), webText(cerr.Error()))
				return nil
			}
			if v.Typed == "" {
				mgr.LearnResult(server, tool, res) // typed from the next call on
			}
			d = mcp.ShapeResult(res, v.Fields, compact)
		}
		webMCPFill(&out, d)
		call.reply(out)
		return nil
	}
}

func webMCPErrCode(err error) webproto.ErrCode {
	if errors.Is(err, context.DeadlineExceeded) {
		return webproto.CodeBusy
	}
	return webproto.CodeInternal
}

// webMCPFill puts shaped data on the wire: one block that passes the
// block caps, and the raw JSON within its cap.
func webMCPFill(out *webproto.MCPViewData, d mcp.ViewData) {
	out.Layout, out.Truncated, out.Error = d.Layout, d.Truncated, d.Error
	d.Text = webText(d.Text)
	for _, r := range d.Rows {
		for j := range r {
			r[j].S = webText(r[j].S)
		}
	}
	b := d.Block(out.View, out.Title, out.View)
	out.Block = &b
	out.Truncated = out.Truncated || b.Truncated
	out.Raw = webMCPClip(webText(d.Raw), webproto.MaxMCPViewRaw, &out.Truncated)
}

func webMCPClip(s string, n int, cut *bool) string {
	if len(s) <= n {
		return s
	}
	*cut = true
	r := []rune(s[:n])
	if len(r) > 0 && r[len(r)-1] == '�' {
		r = r[:len(r)-1]
	}
	return string(r)
}

func webMCPFields(fs []mcp.ViewField) []webproto.MCPViewField {
	out := make([]webproto.MCPViewField, 0, len(fs))
	for _, f := range fs {
		if len(out) == webproto.MaxMCPViewFields {
			break
		}
		out = append(out, webproto.MCPViewField{ID: webName(f.ID), Label: webName(f.Label), Kind: f.Kind})
	}
	return out
}

func webMCPViewInfo(v mcp.ToolView) webproto.MCPViewInfo {
	info := webproto.MCPViewInfo{ID: v.ID, Tool: webName(v.Tool), Title: webName(v.Title), Description: webText(v.Description),
		Params: []webproto.MCPViewParam{}, Fields: webMCPFields(v.Fields), Layout: v.Layout, Typed: v.Typed, Run: v.Run, Why: webText(v.Why)}
	for _, p := range v.Params {
		info.Params = append(info.Params, webproto.MCPViewParam{ID: webName(p.ID), Label: webName(p.Label), Kind: p.Kind,
			Enum: p.Enum, Required: p.Required, Hint: webText(p.Hint)})
	}
	for _, r := range v.Resources {
		if len(info.Resources) == webproto.MaxMCPChoices {
			break
		}
		info.Resources = append(info.Resources, webproto.MCPViewChoice{URI: webText(r.URI), Name: webName(r.Name), Template: r.Template, Vars: r.Vars})
	}
	return info
}
