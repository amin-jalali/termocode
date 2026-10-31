package app

// AI: MCP: Import from Other Apps… (MCPX-7): the servers in other apps'
// known MCP files (mcp.ScanOtherApps) are shown on a preview card —
// name, what it runs, the file — with Cancel first and focused. Import
// adds them to your mcp.json the way Paste a config… does: tokens go to
// the secret store, the entries get ${secret:NAME}. The other apps'
// files are only read. UI text names files, never apps.

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/picker"
)

const confirmKindMCPImportOther confirmKindEnum = 2290

const mcpImportOtherID = "mcp-import-other"

func mcpImportOtherPaletteItems() []picker.Item {
	return []picker.Item{{ID: mcpImportOtherID, Title: "AI: MCP: Import from Other Apps…"}}
}

func (m *Model) dispatchMCPImportOtherPalette(id string) (tea.Cmd, bool) {
	if id != mcpImportOtherID {
		return nil, false
	}
	return m.openMCPImportOther(), true
}

// mcpImportOtherMax caps the preview rows; the rest are counted.
const mcpImportOtherMax = 12

// openMCPImportOther scans and opens the preview card (a toast when
// there is nothing new).
func (m *Model) openMCPImportOther() tea.Cmd {
	home, _ := os.UserHomeDir()
	have := map[string]bool{}
	all, _ := mcp.LoadAll(mcpUserPath(), mcp.ProjectPath(aiWorkspaceRoot()))
	for _, s := range all {
		have[s.Name] = true
	}
	found := mcp.ScanOtherApps(mcp.OtherAppFiles(home, runtime.GOOS, os.Getenv("APPDATA")), have)
	var list []mcp.Imported
	var rows []string
	for _, f := range found {
		for _, im := range f.Servers {
			list = append(list, im)
			if len(rows) < mcpImportOtherMax {
				rows = append(rows, fmt.Sprintf("%s  %s  (%s)", im.Name, ai.Redact(mcp.PreviewLine(im)), imHome(f.Path)))
			}
		}
	}
	if len(list) == 0 {
		return m.aiInfo("MCP: no new servers in other apps' config files")
	}
	if extra := len(list) - len(rows); extra > 0 {
		rows = append(rows, fmt.Sprintf("… and %d more", extra))
	}
	secrets := 0
	for _, im := range list {
		secrets += len(im.Secrets)
	}
	msg := "Found in other apps' config files. Import copies them to your mcp.json; those files stay as they are."
	if secrets > 0 {
		msg += fmt.Sprintf(" %d token(s) go to your secret store, not to mcp.json.", secrets)
	}
	msg += " Imported servers run on your computer: only import what you trust.\n" + strings.Join(rows, "\n")
	m.aiS().mcpImport = list
	m.confirm = confirm.New(fmt.Sprintf("Import %d MCP server(s)?", len(list)), msg, []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "import", Title: fmt.Sprintf("Import %d", len(list)), Style: confirm.StylePrimary},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindMCPImportOther
	return nil
}

// answerMCPImportOther: "import" adds the previewed servers; anything
// else (Cancel, Esc) changes nothing.
func (m *Model) answerMCPImportOther(id string) tea.Cmd {
	list := m.aiS().mcpImport
	m.aiS().mcpImport = nil
	m.confirmOpen = false
	if id != "import" || len(list) == 0 {
		return nil
	}
	added, skipped := mcpAddImported(list)
	m.aiMCP().Reload()
	if len(added) == 0 {
		return m.aiToastErr("MCP import", fmt.Errorf("nothing added. %s", mcp.ImportSummary(nil, skipped)))
	}
	return m.aiInfo("MCP: " + mcp.ImportSummary(added, skipped))
}

// mcpAddImported writes imported servers to the user mcp.json and their
// tokens to the secret store; a name that exists is skipped.
func mcpAddImported(list []mcp.Imported) (added, skipped []string) {
	cs := mcp.CredStore{Path: mcpCredPath()}
	for _, im := range list {
		if len(mcpServerEntries(im.Name)) > 0 {
			skipped = append(skipped, im.Name+": already in mcp.json")
			continue
		}
		ok := true
		for k, v := range im.Secrets {
			ai.RegisterSecret(v)
			if err := cs.SetSecret(k, v); err != nil {
				skipped = append(skipped, im.Name+": "+ai.Redact(err.Error()))
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if err := mcp.AddServer(mcpUserPath(), im.Name, im.Entry); err != nil {
			skipped = append(skipped, im.Name+": "+ai.Redact(err.Error()))
			continue
		}
		added = append(added, im.Name)
	}
	return added, skipped
}
