package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containerhost"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Container file browser (CTX-9): F on a running container lists its
// folders in a picker. A file opens a card: View (read-only page) or
// Download (a new file in Downloads, never replacing one). "Upload a
// file here…" asks for a local path, then a Cancel-first card; a file
// that is already there is replaced only after a second card.

const (
	pickerKindContainerFiles       pickerKindEnum  = 4901
	promptKindContainerFileUpload  promptKindEnum  = 4902
	confirmKindContainerFile       confirmKindEnum = 4903
	confirmKindContainerFileUpload confirmKindEnum = 4904
)

// containerFileViewMax caps the text a read-only file page shows.
const containerFileViewMax = 1 << 20

// containerFilesState is the open browser: one container at a time.
type containerFilesState struct {
	host    *containerhost.Host
	name    string // shown container name
	dir     string // the folder in the picker
	file    string // the file of the open card
	upload  string // the local file waiting for its card
	replace bool   // the upload card replaces an existing file
}

// containerUIDoneMsg carries the result of a container UI job back to
// the model (apply runs in Update).
type containerUIDoneMsg struct{ apply func(*Model) tea.Cmd }

// containerFileItems are the picker rows of dir.
func containerFileItems(dir string, ents []containerhost.BrowseEntry) []picker.Item {
	var items []picker.Item
	if path.Clean("/"+dir) != "/" {
		items = append(items, picker.Item{ID: "up", Title: "..", Hint: "folder above"})
	}
	items = append(items, picker.Item{ID: "upload", Title: "Upload a file here…", Hint: "asks first"})
	for _, e := range ents {
		name := sanitizeOutputLine(e.Name)
		switch {
		case e.Dir:
			items = append(items, picker.Item{ID: "d:" + e.Name, Title: name + "/"})
		case e.Link:
			items = append(items, picker.Item{ID: "f:" + e.Name, Title: name, Hint: "link"})
		default:
			items = append(items, picker.Item{ID: "f:" + e.Name, Title: name, Hint: humanBytes(e.Size)})
		}
	}
	return items
}

// containerUploadLocalPath is the typed local path, absolute (~ = home).
func containerUploadLocalPath(s, home string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "~" || strings.HasPrefix(s, "~/") {
		s = filepath.Join(home, strings.TrimPrefix(s, "~"))
	}
	if !filepath.IsAbs(s) {
		return "", errors.New("type a full path like ~/notes.txt or /tmp/a.conf")
	}
	return filepath.Clean(s), nil
}

// openContainerFiles connects to a running container and lists "/".
func (m *Model) openContainerFiles(c containers.Container) tea.Cmd {
	if !strings.EqualFold(c.State, "running") {
		return m.servicesToast(toast.Info, "Start the container first", "Files can be browsed only while it runs")
	}
	e := m.services.containerEngine
	name := containerShownName(c)
	cfg := containerhost.Config{Binary: e.Binary, Kind: e.Kind, Context: e.Context, Container: c.ID, Root: "/", X: e.X}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		h, err := containerhost.New(ctx, cfg)
		return containerUIDoneMsg{apply: func(m *Model) tea.Cmd {
			if err != nil {
				return m.servicesToast(toast.Errr, "Cannot open the files of "+name, errLine(err.Error()))
			}
			m.services.files = containerFilesState{host: h, name: name, dir: "/"}
			return m.listContainerFiles("/")
		}}
	}
}

// listContainerFiles reads dir and shows it in the picker.
func (m *Model) listContainerFiles(dir string) tea.Cmd {
	h := m.services.files.host
	if h == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		ents, err := containerhost.BrowseList(ctx, h.FS(), dir)
		return containerUIDoneMsg{apply: func(m *Model) tea.Cmd {
			if m.services.files.host != h {
				return nil
			}
			if err != nil {
				return m.servicesToast(toast.Errr, "Cannot list "+dir, errLine(err.Error()))
			}
			m.services.files.dir = path.Clean("/" + dir)
			m.picker = picker.NewItems(" "+sanitizeOutputLine(m.services.files.name)+": "+sanitizeOutputLine(m.services.files.dir)+" ", containerFileItems(dir, ents))
			m.picker.SetSize(m.w, m.h)
			m.pickerOpen, m.pickerKind = true, pickerKindContainerFiles
			return nil
		}}
	}
}

// onContainerFilePicked handles one picker row.
func (m *Model) onContainerFilePicked(id string) tea.Cmd {
	st := &m.services.files
	if st.host == nil {
		return nil
	}
	switch {
	case id == "up":
		return m.listContainerFiles(containerhost.BrowseParent(st.dir))
	case id == "upload":
		m.prompt = prompt.New("Upload to "+st.dir, "Local file (full path):", "~/")
		m.prompt.SetNote("The card asks before anything is written in " + st.name)
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen, m.promptKind = true, promptKindContainerFileUpload
		return nil
	case strings.HasPrefix(id, "d:"):
		return m.listContainerFiles(path.Join(st.dir, strings.TrimPrefix(id, "d:")))
	case strings.HasPrefix(id, "f:"):
		st.file = path.Join(st.dir, strings.TrimPrefix(id, "f:"))
		m.confirm = confirm.New(sanitizeOutputLine(st.file), "In container "+sanitizeOutputLine(st.name)+".\n\nView opens it read-only. Download saves a copy in your Downloads folder (an existing file is never replaced).", []confirm.Button{
			{ID: "cancel", Title: "Cancel"}, {ID: "view", Title: "View"}, {ID: "download", Title: "Download"},
		})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen, m.confirmKind = true, confirmKindContainerFile
	}
	return nil
}

// onContainerFileUploadPrompt checks the local path and asks first.
func (m *Model) onContainerFileUploadPrompt(value string) tea.Cmd {
	st := &m.services.files
	if st.host == nil {
		return nil
	}
	home, _ := os.UserHomeDir()
	local, err := containerUploadLocalPath(value, home)
	if err != nil {
		return m.servicesToast(toast.Info, "Cannot upload", err.Error())
	}
	if info, err := os.Stat(local); err != nil || !info.Mode().IsRegular() {
		return m.servicesToast(toast.Info, "Cannot upload", local+" is not a file")
	}
	st.upload, st.replace = local, false
	m.askContainerUpload()
	return nil
}

func (m *Model) askContainerUpload() {
	st := &m.services.files
	target := path.Join(st.dir, filepath.Base(st.upload))
	title, button, style := "Upload file?", "Upload", confirm.StyleDefault
	body := "This copies " + st.upload + " into container " + st.name + " as " + target + "."
	if st.replace {
		title, button, style = "Replace file?", "Replace", confirm.StyleDestructive
		body = target + " is already in container " + st.name + ".\n\nReplace it with " + st.upload + "?"
	}
	m.confirm = confirm.New(title, sanitizeOutputLine(body), []confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "upload", Title: button, Style: style}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerFileUpload
}

// handleContainerFilesConfirm answers the two file cards.
func (m *Model) handleContainerFilesConfirm(id string) (tea.Cmd, bool) {
	st := &m.services.files
	switch m.confirmKind {
	case confirmKindContainerFile:
		p, h := st.file, st.host
		st.file = ""
		if h == nil || p == "" {
			return nil, true
		}
		switch id {
		case "view":
			return m.viewContainerFile(h, p), true
		case "download":
			return m.downloadContainerFile(h, p), true
		}
		return nil, true
	case confirmKindContainerFileUpload:
		local, replace, h, dir := st.upload, st.replace, st.host, st.dir
		st.upload, st.replace = "", false
		if id != "upload" || h == nil || local == "" {
			return nil, true
		}
		return m.uploadContainerFile(h, local, dir, replace), true
	}
	return nil, false
}

func (m *Model) viewContainerFile(h *containerhost.Host, p string) tea.Cmd {
	name := m.services.files.name
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		f, err := containerhost.BrowseView(ctx, h.FS(), p, containerFileViewMax)
		return containerUIDoneMsg{apply: func(m *Model) tea.Cmd {
			switch {
			case err != nil:
				return m.servicesToast(toast.Errr, "Cannot open "+p, errLine(err.Error()))
			case f.Binary:
				return m.servicesToast(toast.Info, "Binary file", "Download it to look at it")
			}
			if err := m.openReadOnlyPage("file: "+name+":"+f.Path, f.Text, f.Path, ""); err != nil {
				return m.servicesToast(toast.Errr, "Cannot open the file page", err.Error())
			}
			if f.Truncated {
				return m.servicesToast(toast.Info, "Showing the first "+humanBytes(containerFileViewMax), "Download the file to see all "+humanBytes(f.Size))
			}
			return nil
		}}
	}
}

func (m *Model) downloadContainerFile(h *containerhost.Host, p string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		dest, err := downloadsDir()
		var saved string
		if err == nil {
			saved, err = containerhost.BrowseDownload(ctx, h.FS(), p, dest)
		}
		return containerUIDoneMsg{apply: func(m *Model) tea.Cmd {
			if err != nil {
				return m.servicesToast(toast.Errr, "Cannot download "+p, errLine(err.Error()))
			}
			return m.servicesToast(toast.Info, "Downloaded "+path.Base(p), "Saved as "+saved)
		}}
	}
}

func (m *Model) uploadContainerFile(h *containerhost.Host, local, dir string, replace bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		target, err := containerhost.BrowseUpload(ctx, h.FS(), local, dir, replace)
		return containerUIDoneMsg{apply: func(m *Model) tea.Cmd {
			if errors.Is(err, containerhost.ErrBrowseExists) && m.services.files.host == h {
				m.services.files.upload, m.services.files.replace = local, true
				m.askContainerUpload()
				return nil
			}
			if err != nil {
				return m.servicesToast(toast.Errr, "Cannot upload "+filepath.Base(local), errLine(err.Error()))
			}
			return tea.Batch(m.servicesToast(toast.Info, "Uploaded "+filepath.Base(local), fmt.Sprintf("to %s", target)), m.listContainerFiles(dir))
		}}
	}
}
