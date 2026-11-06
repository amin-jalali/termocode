package app

import (
	"errors"
	"runtime"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The language server installer from the web (WEBP-2): the laptop's
// LSP: Manage Language Servers… as a list on the page. An install,
// reinstall or uninstall waits for a laptop card, then runs exactly as
// the laptop's own manager runs it (in the background, with toasts).

func init() {
	webOps[webproto.OpLSPServers] = (*Model).webLSPServers
	webAskOps[webproto.OpLSPServerAsk] = (*Model).webLSPServerAsk
}

// webLSPServers lists the language servers. The install state is read on
// Update; the disk and PATH probes and the workspace scan run in a Cmd.
func (m *Model) webLSPServers(call *webCall) tea.Cmd {
	tools := lspinstall.ByCategory(lspinstall.CategoryLSP)
	snaps := make(map[string]aiInstallSnap, len(tools))
	for _, t := range tools {
		snaps[t.Name] = m.aiInstallLook(t.Name)
	}
	root, _ := m.rootDir()
	return func() tea.Msg {
		call.reply(webLSPServerList(tools, snaps, root))
		return nil
	}
}

// webLSPServerList builds the list (off the UI goroutine).
func webLSPServerList(tools []lspinstall.Tool, snaps map[string]aiInstallSnap, root string) webproto.LSPServerList {
	used := map[string]bool{}
	if root != "" {
		for _, ft := range aiWorkspaceFiletypes(root) {
			used[ft] = true
		}
	}
	out := webproto.LSPServerList{Items: make([]webproto.LSPServer, 0, len(tools))}
	for _, t := range tools {
		s := webproto.LSPServer{Name: t.Name, Title: t.DisplayName(), Langs: append([]string{}, t.Filetypes...)}
		for _, ft := range t.Filetypes {
			if used[ft] {
				s.Used = true
			}
		}
		snap := snaps[t.Name]
		st := lspinstall.StatusOf(t)
		switch {
		case snap.installing:
			s.State, s.Progress = "installing", webText(snap.progress)
		case st.State == lspinstall.Managed:
			s.State, s.Version = "managed", webText(st.Version)
		case st.State == lspinstall.System:
			s.State = "system"
		default:
			s.State = "missing"
			if err := toolInstaller().check(t); err != nil {
				s.Problem = webLSPProblem(err)
			}
		}
		if !snap.installing && snap.result != nil && snap.result.err != nil {
			s.Problem = "the last install failed; its output is on the laptop"
		}
		out.Items = append(out.Items, s)
	}
	sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].Used && !out.Items[j].Used })
	return out
}

// webLSPProblem is why a tool cannot be installed, without laptop paths.
func webLSPProblem(err error) string {
	var mt *lspinstall.MissingToolchainError
	if errors.As(err, &mt) {
		return mt.Bin + " is not on PATH (" + mt.Hint + ")"
	}
	var un *lspinstall.UnsupportedError
	if errors.As(err, &un) {
		return "no automatic install on this laptop"
	}
	var off *lspinstall.OfflineError
	if errors.As(err, &off) {
		return "the laptop is offline"
	}
	return "it cannot be installed on this laptop"
}

// webLSPServerAsk asks the laptop to install or uninstall a server.
func (m *Model) webLSPServerAsk(call *webCall) *webAsk {
	a := call.args.(*webproto.LSPServerAskArgs)
	t, ok := lspinstall.Lookup(a.Name)
	if !ok || t.Category != lspinstall.CategoryLSP {
		call.fail(webproto.CodeNotFound, "There is no language server of that name.")
		return nil
	}
	m.ensureLSPMgr()
	if m.lspMgr.installing[t.Name] {
		call.fail(webproto.CodeConflict, t.Name+" is installing on the laptop now.")
		return nil
	}
	st := lspinstall.StatusOf(t)
	if a.Action == "uninstall" {
		if st.State != lspinstall.Managed {
			call.fail(webproto.CodeConflict, "termocode did not install "+t.Name+"; there is nothing of its own to remove.")
			return nil
		}
		return &webAsk{
			summary: t.Name,
			title:   "Uninstall " + t.Name + "?",
			body:    "Uninstall " + t.Name + "? termocode's own copy is removed; running servers stop on restart.",
			action:  "Uninstall",
			run: func(m *Model) tea.Cmd {
				return func() tea.Msg { return lspUninstallDoneMsg{name: t.Name, err: lspinstall.Uninstall(t)} }
			},
		}
	}
	if err := toolInstaller().check(t); err != nil {
		call.fail(webproto.CodeConflict, t.Name+" cannot be installed here: "+webLSPProblem(err)+".")
		return nil
	}
	runs := t.InstallKind.String()
	if dir, err := lspinstall.ToolDir(t.Name); err == nil {
		if plan, err := lspinstall.BuildPlan(t, dir, runtime.GOOS, runtime.GOARCH); err == nil {
			runs = webText(plan.HumanCommand())
		}
	}
	body := "Install " + t.Name + " on this laptop?\n\nRuns: " + runs + "\nInto termocode's own tools folder."
	if st.State == lspinstall.System {
		body += "\nA copy is already on PATH; termocode's own copy takes precedence inside termocode."
	}
	return &webAsk{
		summary: t.Name,
		title:   "Install " + t.Name + "?",
		body:    body,
		action:  "Install",
		run:     func(m *Model) tea.Cmd { return m.startToolInstall(t) },
	}
}
