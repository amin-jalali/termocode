package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Bundled nvim plugins (multi-cursor, debugger, bracket-pair colours) are
// git-cloned into pluginsDir() on first run. The clone runs in the
// background after nvim setup (pluginBootstrapCmds): startup and typing
// never wait for the network. Until a plugin is there its feature is off;
// when its clone finishes the plugin is set up in the running nvim, and a
// failed clone shows a toast (next start tries again).

// plugin is one bootstrap-cloned nvim plugin.
type plugin struct {
	name   string // checkout dir under pluginsDir()
	repo   string
	branch string // tag or branch; "" = default branch
	marker string // path inside the checkout that a complete clone has
	what   string // the feature, for the failure toast
}

var (
	pluginMultiCursor = plugin{name: "vim-visual-multi", repo: vimVisualMultiRepo,
		marker: ".git", what: "Multi-cursor"}
	pluginDap = plugin{name: "nvim-dap", repo: nvimDapRepo,
		marker: ".git", what: "The debugger"}
	pluginBracketPair = plugin{name: "rainbow-delimiters.nvim",
		repo: "https://github.com/HiPhish/rainbow-delimiters.nvim.git", branch: "v0.10.0",
		marker: "lua/rainbow-delimiters", what: "Bracket pair colours"}
	bootstrapPlugins = []plugin{pluginMultiCursor, pluginDap, pluginBracketPair}
)

// pluginCloneTimeout caps one clone, so a hung network does not leave a
// git process behind for the whole session.
const pluginCloneTimeout = 3 * time.Minute

// dir is the plugin's checkout path ("" when the plugins dir is unknown).
func (p plugin) dir() string {
	root, err := pluginsDir()
	if err != nil {
		return ""
	}
	return filepath.Join(root, p.name)
}

// installedDir returns the checkout path when a complete clone is there,
// else "".
func (p plugin) installedDir() string {
	d := p.dir()
	if d == "" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(d, p.marker)); err != nil {
		return ""
	}
	return d
}

// pluginClonedMsg reports a finished background clone.
type pluginClonedMsg struct {
	p    plugin
	path string
	err  error
}

// clonePlugin clones p into a temp dir next to its checkout and renames it
// into place, so an interrupted clone never leaves a half checkout behind.
func clonePlugin(p plugin) (string, error) {
	dest := p.dir()
	if dest == "" {
		return "", fmt.Errorf("no plugins directory")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("git is not installed")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	// A dir without the marker is a half checkout from an older version.
	if _, err := os.Stat(dest); err == nil {
		_ = os.RemoveAll(dest)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dest), "."+p.name+".clone-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	out := filepath.Join(tmp, p.name)
	args := []string{"clone", "--depth=1", "--quiet"}
	if p.branch != "" {
		args = append(args, "--branch", p.branch)
	}
	args = append(args, p.repo, out)
	ctx, cancel := context.WithTimeout(context.Background(), pluginCloneTimeout)
	defer cancel()
	cmd := spawn.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(b))
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	if err := os.Rename(out, dest); err != nil {
		if p.installedDir() != "" { // another termocode won the race
			return dest, nil
		}
		return "", err
	}
	return dest, nil
}

// pluginBootstrapCmds starts a background clone for every plugin that is
// not downloaded yet.
func pluginBootstrapCmds() []tea.Cmd {
	var cmds []tea.Cmd
	for _, p := range bootstrapPlugins {
		if p.installedDir() != "" {
			continue
		}
		p := p
		cmds = append(cmds, func() tea.Msg {
			path, err := clonePlugin(p)
			return pluginClonedMsg{p: p, path: path, err: err}
		})
	}
	return cmds
}

// setupPlugin runs p's setup Lua in nvim. path is its checkout, or "" when
// it is not downloaded (the setup then only defines stubs where needed).
func (m *Model) setupPlugin(p plugin, path string) {
	if m.nvim == nil {
		return
	}
	switch p.name {
	case pluginMultiCursor.name:
		if path != "" {
			_ = m.nvim.ExecLua(multiCursorSetupLua(path))
		}
	case pluginDap.name:
		_ = m.nvim.ExecLua(dapSetupLua(path))
		// Adapters installed by the managed installer (tools/bin shims).
		_ = m.nvim.ExecLua(dapManagedLua)
		m.debugLoadBreakpoints() // Group D: persisted breakpoints (m.debug is shared)
	case pluginBracketPair.name:
		_ = m.nvim.ExecLua(bracketPairLua(path))
	}
}

// onPluginCloned sets up a freshly cloned plugin, or tells the user the
// download failed.
func (m *Model) onPluginCloned(msg pluginClonedMsg) tea.Cmd {
	if msg.err != nil {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, "Plugin download failed",
			fmt.Sprintf("%s: %v. %s is off; termocode tries again on the next start.", msg.p.name, msg.err, msg.p.what))
		return c
	}
	m.setupPlugin(msg.p, msg.path)
	return nil
}
