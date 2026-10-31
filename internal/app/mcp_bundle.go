package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/prompt"
)

// ── MCP bundle install (MCPX-31) ───────────────────────────────────────
//
// AI: MCP: Install Bundle (.mcpb)… asks for the file, shows its manifest
// on a Cancel-first card, asks each user_config value (a sensitive one
// masked, kept in memory), then shows the exact command on a
// Cancel-first install card. Install unpacks the files, saves the
// secrets in the store and adds the mcp.json entry.

const (
	promptKindMCPBundlePath   promptKindEnum = 2502
	promptKindMCPBundleField  promptKindEnum = 2503
	promptKindMCPBundleSecret promptKindEnum = 2504
)

const confirmKindMCPBundle confirmKindEnum = 2502

// mcpBundleFlow is the running install.
type mcpBundleFlow struct {
	b       *mcp.Bundle
	keys    []string // user_config keys still to ask
	values  map[string]string
	secrets map[string]string // secret name → value, saved on Install
	final   bool              // the open card is the install card
}

// mcpBundlesDir is where bundles are unpacked.
func mcpBundlesDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "termocode", "mcp-bundles")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "termocode", "mcp-bundles")
}

func (m *Model) openMCPBundle() tea.Cmd {
	m.aiS().mcpBundle = nil
	m.prompt = prompt.New("Install MCP bundle", "Path of the .mcpb file:", "")
	m.prompt.SetCompleter(prompt.CompletePath)
	m.prompt.SetNote("termocode shows what is inside before anything is installed.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindMCPBundlePath
	return nil
}

func (m *Model) onMCPBundlePath(v string) tea.Cmd {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	if strings.HasPrefix(v, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			v = filepath.Join(h, v[2:])
		}
	}
	b, err := mcp.OpenBundle(v)
	if err != nil {
		return m.aiToastErr("MCP bundle", err)
	}
	m.aiS().mcpBundle = &mcpBundleFlow{b: b, keys: b.ConfigKeys(), values: map[string]string{}, secrets: map[string]string{}}
	return m.mcpBundleCard(false)
}

// mcpBundleCard opens the manifest card (final=false) or the install card.
func (m *Model) mcpBundleCard(final bool) tea.Cmd {
	f := m.aiS().mcpBundle
	man := f.b.Manifest
	title := man.DisplayName
	if title == "" {
		title = man.Name
	}
	var s strings.Builder
	if man.Description != "" {
		s.WriteString(man.Description + "\n\n")
	}
	fmt.Fprintf(&s, "Version: %s\n", man.Version)
	if man.Author.Name != "" {
		fmt.Fprintf(&s, "Author: %s (not verified)\n", man.Author.Name)
	}
	if len(man.Tools) > 0 {
		var names []string
		for i, t := range man.Tools {
			if i == 8 {
				names = append(names, fmt.Sprintf("+%d more", len(man.Tools)-8))
				break
			}
			names = append(names, t.Name)
		}
		fmt.Fprintf(&s, "Tools: %s\n", strings.Join(names, ", "))
	}
	f.final = final
	if !final {
		if len(f.keys) > 0 {
			var asks []string
			for _, k := range f.keys {
				fl := man.UserConfig[k]
				t := fl.Title
				if t == "" {
					t = k
				}
				if fl.Sensitive {
					t += " (secret)"
				}
				asks = append(asks, t)
			}
			fmt.Fprintf(&s, "It asks for: %s\n", strings.Join(asks, ", "))
		}
		s.WriteString("Nothing is installed yet.")
		m.confirm = confirm.Risky("MCP bundle "+title+"?", s.String(),
			confirm.Button{ID: "cancel", Title: "Cancel"},
			confirm.Button{ID: "next", Title: "Continue"})
	} else {
		_, entry, err := f.b.EntryNamed(filepath.Join(mcpBundlesDir(), "…"), f.values)
		if err != nil {
			m.aiS().mcpBundle = nil
			return m.aiToastErr("MCP bundle", err)
		}
		if n := len(f.secrets); n > 0 {
			fmt.Fprintf(&s, "%d secret(s) go to the secret store, never into mcp.json.\n", n)
		}
		s.WriteString("Install unpacks it into termocode's data folder and adds \"" + f.b.ServerName() +
			"\" to your mcp.json. It runs when the AI first calls it.")
		m.confirm = confirm.Risky("Install MCP bundle "+title+"?", s.String(),
			confirm.Button{ID: "cancel", Title: "Cancel"},
			confirm.Button{ID: "install", Title: "Install", Style: confirm.StyleDestructive}).
			WithCode(mcpEntryText(entry), "This runs on your computer when the AI uses it.")
	}
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindMCPBundle
	return nil
}

// answerMCPBundle runs a card button (Esc is Cancel).
func (m *Model) answerMCPBundle(id string) tea.Cmd {
	f := m.aiS().mcpBundle
	switch {
	case f == nil || id == "cancel" || id == "":
		m.aiS().mcpBundle = nil
		return nil
	case id == "next" && !f.final:
		return m.mcpBundleNextField()
	case id == "install" && f.final:
		return m.mcpBundleInstall()
	}
	return nil
}

// mcpBundleNextField asks the next user_config value, or opens the
// install card when all are in.
func (m *Model) mcpBundleNextField() tea.Cmd {
	f := m.aiS().mcpBundle
	if len(f.keys) == 0 {
		return m.mcpBundleCard(true)
	}
	k := f.keys[0]
	fl := f.b.Manifest.UserConfig[k]
	label := fl.Title
	if label == "" {
		label = k
	}
	if fl.Required {
		label += " (required)"
	}
	if fl.Sensitive {
		m.prompt = prompt.New("MCP bundle secret", label+":", "")
		m.prompt.SetMasked(true)
		m.prompt.SetNote(secretPromptNote("Saved in the secret store on Install, never in mcp.json. " + fl.Description))
		m.promptKind = promptKindMCPBundleSecret
	} else {
		m.prompt = prompt.New("MCP bundle setting", label+":", fl.DefaultText())
		if fl.Type == "directory" || fl.Type == "file" {
			m.prompt.SetCompleter(prompt.CompletePath)
		}
		m.prompt.SetAllowEmpty(!fl.Required)
		m.prompt.SetNote(fl.Description)
		m.promptKind = promptKindMCPBundleField
	}
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	return nil
}

func (m *Model) onMCPBundleField(v string, secret bool) tea.Cmd {
	f := m.aiS().mcpBundle
	if f == nil || len(f.keys) == 0 {
		return nil
	}
	k := f.keys[0]
	v = strings.TrimSpace(v)
	fl := f.b.Manifest.UserConfig[k]
	if v == "" && fl.Required && (secret || fl.DefaultText() == "") {
		return m.mcpBundleNextField() // asked again
	}
	if secret {
		if v != "" {
			ai.RegisterSecret(v)
			f.secrets[f.b.SecretName(k)] = v
		}
	} else {
		f.values[k] = v
	}
	f.keys = f.keys[1:]
	return m.mcpBundleNextField()
}

func (m *Model) mcpBundleInstall() tea.Cmd {
	f := m.aiS().mcpBundle
	m.aiS().mcpBundle = nil
	root := mcpBundlesDir()
	if root == "" {
		return m.aiToastErr("MCP bundle", fmt.Errorf("no data folder"))
	}
	dir, err := f.b.Install(root)
	if err != nil {
		return m.aiToastErr("MCP bundle", err)
	}
	name, entry, err := f.b.EntryNamed(dir, f.values)
	if err == nil {
		err = mcp.AddServer(mcpUserPath(), name, entry)
	}
	if err != nil {
		return m.aiToastErr("MCP bundle", err)
	}
	store := mcp.CredStore{Path: mcpCredPath()}
	for n, v := range f.secrets {
		if err := store.SetSecret(n, v); err != nil {
			return m.aiToastErr("MCP bundle", err)
		}
	}
	m.aiMCP().Reload()
	return m.aiInfo("Installed MCP " + name + ". It starts on the AI's first call")
}
