package app

import (
	"path/filepath"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Quick open of the desktop workbench (files.list): the workspace's file
// paths as the laptop's "Go to file" lists them (the same walk, the same
// skipped folders), relative and slash-separated. The page ranks them
// itself while the user types. A remote workspace has no list here yet.

func init() {
	webOps[webproto.OpFilesList] = webOpFilesList
}

func webOpFilesList(m *Model, call *webCall) tea.Cmd {
	if _, remote := m.remoteFS(); remote {
		call.fail(webproto.CodeNotFound, "Go to file works on a local workspace only.")
		return nil
	}
	root, err := m.rootDir()
	if err != nil {
		call.fail(webproto.CodeNotFound, "No workspace is open on the laptop.")
		return nil
	}
	call.later()
	return func() tea.Msg {
		call.reply(webFilesList(root))
		return nil
	}
}

// webFilesList lists the files under root (at most MaxQuickFiles).
func webFilesList(root string) webproto.FilesListRes {
	items := loadFilesIn([]string{root})
	out := webproto.FilesListRes{Paths: make([]string, 0, min(len(items), webproto.MaxQuickFiles))}
	for _, it := range items {
		if len(out.Paths) >= webproto.MaxQuickFiles {
			out.Truncated = true
			break
		}
		out.Paths = append(out.Paths, filepath.ToSlash(it.ID))
	}
	sort.Strings(out.Paths)
	return out
}
