package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// CTX-31: image and container archive — save, load and import images,
// commit and export containers, the container diff, the image layer
// sizes and which containers use an image.

const (
	promptKindContainerSave   promptKindEnum = 6132
	promptKindContainerLoad   promptKindEnum = 6133
	promptKindContainerImport promptKindEnum = 6134
	promptKindContainerCommit promptKindEnum = 6135
	promptKindContainerExport promptKindEnum = 6136
)

// archiveJobTimeout bounds a save, load, import or export.
const archiveJobTimeout = 30 * time.Minute

func containersArchivePaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "containers-image-save", Title: "Containers: Save Selected Image to File…"},
		{ID: "containers-image-load", Title: "Containers: Load Images from File…"},
		{ID: "containers-image-import", Title: "Containers: Import Image from File System Tar…"},
		{ID: "containers-image-layers", Title: "Containers: Show Selected Image Layer Sizes"},
		{ID: "containers-image-used-by", Title: "Containers: Show Containers Using Selected Image"},
		{ID: "containers-commit", Title: "Containers: Commit Selected Container to Image…"},
		{ID: "containers-export", Title: "Containers: Export Selected Container to File…"},
		{ID: "containers-diff", Title: "Containers: Show Selected Container Changes (Diff)"},
	}
}

func (m *Model) dispatchContainersArchive(id string) (tea.Cmd, bool) {
	var f func(m *Model) tea.Cmd
	switch id {
	case "containers-image-save", "containers-image-layers", "containers-image-used-by":
		f = func(m *Model) tea.Cmd {
			im, ref, ok := m.selectedImage()
			if !ok {
				return m.servicesToast(toast.Info, "Select an image row first", "")
			}
			switch id {
			case "containers-image-save":
				m.services.pendingArchive = ref
				m.openArchivePrompt(promptKindContainerSave, "Save image", "File to write (docker save):", archiveFileName(ref))
				return nil
			case "containers-image-layers":
				return m.imageLayersPage(ref)
			}
			return m.servicesToastOrPage("used by: "+ref, usedByText(ref, containers.ImageUsedBy(im, m.services.containerRows)))
		}
	case "containers-image-load":
		f = func(m *Model) tea.Cmd {
			m.openArchivePrompt(promptKindContainerLoad, "Load images", "Tar file made by docker save:", "")
			return nil
		}
	case "containers-image-import":
		f = func(m *Model) tea.Cmd {
			m.openArchivePrompt(promptKindContainerImport, "Import image", "File system tar, then the new image name (optional):", "")
			return nil
		}
	case "containers-commit", "containers-export", "containers-diff":
		f = func(m *Model) tea.Cmd {
			c, ok := m.selectedContainer()
			if !ok {
				return m.servicesToast(toast.Info, "Select a container row first", "")
			}
			m.services.pendingArchive = c.ID
			name := archiveContainerName(c)
			switch id {
			case "containers-commit":
				m.openArchivePrompt(promptKindContainerCommit, "Commit "+name, "New image name, then a message (optional):", name+":snapshot")
				return nil
			case "containers-export":
				m.openArchivePrompt(promptKindContainerExport, "Export "+name, "File to write (docker export):", name+".tar")
				return nil
			}
			return m.containerDiffPage(c.ID, name)
		}
	default:
		return nil, false
	}
	return m.servicesAfterLoad(m.revealServicesView(false), f), true
}

func (m *Model) openArchivePrompt(kind promptKindEnum, title, label, initial string) {
	m.prompt = prompt.New(title, label, initial)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, kind
}

// servicesToastOrPage opens a read-only page with text.
func (m *Model) servicesToastOrPage(title, text string) tea.Cmd {
	return m.onContainersPage(containersPageMsg{title: title, text: text})
}

func archiveContainerName(c containers.Container) string {
	for _, n := range c.Names {
		if n = strings.TrimPrefix(n, "/"); n != "" {
			return n
		}
	}
	return shortImageID(c.ID)
}

// archivePath makes a typed path absolute (relative to the workspace).
func archivePath(root, typed string) string {
	p := strings.TrimSpace(typed)
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	return filepath.Clean(p)
}

// archiveFileName is the default tar name for an image ref.
func archiveFileName(ref string) string {
	r := strings.NewReplacer("/", "-", ":", "-", "@", "-")
	return r.Replace(ref) + ".tar"
}

// commitLine splits "image:tag message words" into the ref and message.
func commitLine(line string) (string, string) {
	line = strings.TrimSpace(line)
	ref, msg, _ := strings.Cut(line, " ")
	return ref, strings.TrimSpace(msg)
}

// handleContainersExtraPrompt routes the extra containers prompt kinds.
func (m *Model) handleContainersExtraPrompt(value string) (tea.Cmd, bool) {
	switch m.promptKind {
	case promptKindContainerSave, promptKindContainerLoad, promptKindContainerImport, promptKindContainerCommit, promptKindContainerExport:
		return m.onArchivePrompt(m.promptKind, value), true
	case promptKindVolumeBrowse, promptKindVolumeBackup, promptKindVolumeRestore, promptKindVolumeClone, promptKindVolumeCreate, promptKindNetworkCreate:
		return m.handleVolumePrompt(m.promptKind, value), true // containers_volumes.go
	}
	return nil, false
}

func (m *Model) onArchivePrompt(kind promptKindEnum, value string) tea.Cmd {
	root := m.services.root
	target := m.services.pendingArchive
	m.services.pendingArchive = ""
	var (
		args []string
		err  error
		job  containersExtraJob
	)
	switch kind {
	case promptKindContainerSave:
		path := archivePath(root, value)
		args, err = containers.SaveArgs(path, target)
		job = containersExtraJob{label: "save " + target + " to " + path, args: args, timeout: archiveJobTimeout}
		if err == nil && fileExists(path) {
			return m.askContainersExtra("Replace file?", path+" exists. Saving replaces it.\n\nCommand: "+m.containersCommandLine(args),
				[]confirm.Button{{ID: "run", Title: "Replace", Style: confirm.StyleDestructive}}, map[string]containersExtraJob{"run": job})
		}
	case promptKindContainerLoad:
		path := archivePath(root, value)
		args, err = containers.LoadArgs(path)
		if err == nil {
			job = containersExtraJob{label: "load " + path, section: "images", args: args, timeout: archiveJobTimeout}
			return m.askContainersExtra("Load images?", "This loads the images in "+path+". An image with the same tag is replaced.\n\nCommand: "+m.containersCommandLine(args),
				[]confirm.Button{{ID: "run", Title: "Load", Style: confirm.StylePrimary}}, map[string]containersExtraJob{"run": job})
		}
	case promptKindContainerImport:
		file, ref := commitLine(value)
		path := archivePath(root, file)
		args, err = containers.ImportArgs(path, ref)
		job = containersExtraJob{label: "import " + path, section: "images", args: args, timeout: archiveJobTimeout}
	case promptKindContainerCommit:
		ref, msg := commitLine(value)
		args, err = containers.CommitArgs(target, ref, msg)
		job = containersExtraJob{label: "commit " + shortImageID(target) + " to " + ref, section: "images", args: args, timeout: 10 * time.Minute}
	case promptKindContainerExport:
		path := archivePath(root, value)
		args, err = containers.ExportArgs(target, path)
		job = containersExtraJob{label: "export " + shortImageID(target) + " to " + path, args: args, timeout: archiveJobTimeout}
		if err == nil && fileExists(path) {
			return m.askContainersExtra("Replace file?", path+" exists. Exporting replaces it.\n\nCommand: "+m.containersCommandLine(args),
				[]confirm.Button{{ID: "run", Title: "Replace", Style: confirm.StyleDestructive}}, map[string]containersExtraJob{"run": job})
		}
	}
	if err != nil {
		return m.servicesToast(toast.Info, "Not run", err.Error())
	}
	return m.runContainersExtraJob(job)
}

// containerDiffText is the diff page of one container.
func containerDiffText(name string, entries []containers.DiffEntry) string {
	var b strings.Builder
	b.WriteString("Changes in " + name + " (compared with its image)\n\n")
	if len(entries) == 0 {
		b.WriteString("No changes.\n")
		return b.String()
	}
	b.WriteString(fmt.Sprintf("%d paths  (+ added  ~ changed  - deleted)\n\n", len(entries)))
	for _, e := range entries {
		mark := "~"
		switch e.Kind {
		case "added":
			mark = "+"
		case "deleted":
			mark = "-"
		}
		b.WriteString(mark + " " + e.Path + "\n")
	}
	return b.String()
}

func (m *Model) containerDiffPage(id, name string) tea.Cmd {
	e, t, ok := m.containersEngineOrToast()
	if !ok {
		return t
	}
	args, err := containers.DiffArgs(id)
	if err != nil {
		return m.servicesToast(toast.Info, "Not shown", err.Error())
	}
	execFor := m.services.hostExec()
	argv := append([]string{e.Binary}, args...)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		out, stderr, code, err := execFor.Capture(ctx, "", argv...)
		if err == nil && code != 0 {
			err = errString(errLine(stderr))
		}
		return containersPageMsg{title: "diff: " + name, text: containerDiffText(name, containers.ParseDiff(out)), err: err}
	}
}

// layerSizesText is the layer size analysis page.
func layerSizesText(ref string, r containers.LayerReport) string {
	var b strings.Builder
	b.WriteString("Layers of " + ref + ", largest first\n\n")
	if len(r.Layers) == 0 {
		b.WriteString("No layer adds bytes.\n")
	}
	for _, l := range r.Layers {
		by := strings.Join(strings.Fields(l.CreatedBy), " ")
		if len(by) > 100 {
			by = by[:99] + "…"
		}
		b.WriteString(fmt.Sprintf("%9s  %5.1f%%  %s\n", humanBytes(int64(l.Bytes)), l.Percent, by))
	}
	b.WriteString(fmt.Sprintf("\nTotal %s", humanBytes(int64(r.Total))))
	switch r.Empty {
	case 0:
	case 1:
		b.WriteString(" · 1 empty layer (ENV, LABEL …)")
	default:
		b.WriteString(fmt.Sprintf(" · %d empty layers (ENV, LABEL …)", r.Empty))
	}
	b.WriteString("\n")
	return b.String()
}

func (m *Model) imageLayersPage(ref string) tea.Cmd {
	e, t, ok := m.containersEngineOrToast()
	if !ok {
		return t
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		h, err := e.History(ctx, ref)
		return containersPageMsg{title: "layers: " + ref, text: layerSizesText(ref, containers.LayerSizes(h)), err: err}
	}
}

// usedByText lists the containers made from an image.
func usedByText(ref string, cs []containers.Container) string {
	if len(cs) == 0 {
		return "No container uses " + ref + ".\n"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%d containers use %s\n\n", len(cs), ref))
	for _, c := range cs {
		b.WriteString("  " + archiveContainerName(c) + "  " + c.State + "  " + shortImageID(c.ID) + "\n")
	}
	return b.String()
}
