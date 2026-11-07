package app

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// CTX-32: volumes — browse files, back up, restore and clone (cards),
// create with driver options; networks — create with IP options.

const (
	promptKindVolumeBrowse  promptKindEnum = 6137
	promptKindVolumeBackup  promptKindEnum = 6138
	promptKindVolumeRestore promptKindEnum = 6139
	promptKindVolumeClone   promptKindEnum = 6140
	promptKindVolumeCreate  promptKindEnum = 6141
	promptKindNetworkCreate promptKindEnum = 6142
)

// volumeJobTimeout bounds a backup, restore or clone.
const volumeJobTimeout = 30 * time.Minute

func containersVolumesPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "containers-volume-browse", Title: "Containers: Browse Selected Volume Files…"},
		{ID: "containers-volume-backup", Title: "Containers: Back Up Selected Volume…"},
		{ID: "containers-volume-restore", Title: "Containers: Restore Selected Volume from Backup…"},
		{ID: "containers-volume-clone", Title: "Containers: Clone Selected Volume…"},
		{ID: "containers-volume-create-options", Title: "Containers: Create Volume with Options…"},
		{ID: "containers-network-create-options", Title: "Containers: Create Network with Options…"},
	}
}

// selectedVolume is the name of the selected VOLUMES row.
func (m *Model) selectedVolume() (string, bool) {
	r, ok := m.servicesCurrentRow()
	if !ok || r.kind != svcRowHostItem || r.host != hostVolume || r.idx < 0 || r.idx >= len(m.services.volumeRows) {
		return "", false
	}
	return m.services.volumeRows[r.idx].Name, true
}

func (m *Model) dispatchContainersVolumes(id string) (tea.Cmd, bool) {
	var f func(m *Model) tea.Cmd
	switch id {
	case "containers-volume-browse", "containers-volume-backup", "containers-volume-restore", "containers-volume-clone":
		f = func(m *Model) tea.Cmd {
			name, ok := m.selectedVolume()
			if !ok {
				return m.servicesToast(toast.Info, "Select a volume row first", "")
			}
			m.services.pendingArchive = name
			switch id {
			case "containers-volume-browse":
				m.openArchivePrompt(promptKindVolumeBrowse, "Browse "+name, "Folder inside the volume:", "/")
			case "containers-volume-backup":
				m.openArchivePrompt(promptKindVolumeBackup, "Back up "+name, "Folder for the .tar.gz backup:", m.services.root)
			case "containers-volume-restore":
				m.openArchivePrompt(promptKindVolumeRestore, "Restore "+name, "Backup file (.tar.gz made by Back Up):", "")
			default:
				m.openArchivePrompt(promptKindVolumeClone, "Clone "+name, "New volume name:", name+"-copy")
			}
			return nil
		}
	case "containers-volume-create-options":
		f = func(m *Model) tea.Cmd {
			m.openArchivePrompt(promptKindVolumeCreate, "Create volume", "name driver=… opt.key=value label.key=value:", "")
			return nil
		}
	case "containers-network-create-options":
		f = func(m *Model) tea.Cmd {
			m.openArchivePrompt(promptKindNetworkCreate, "Create network", "name driver=… subnet=… gateway=… ip-range=… internal attachable ipv6 label.key=value:", "")
			return nil
		}
	default:
		return nil, false
	}
	return m.servicesAfterLoad(m.revealServicesView(false), f), true
}

// optionWords splits a create line: the name, then key=value or flag words.
func optionWords(line string) (string, []string, error) {
	words, err := svcShellWords(line)
	if err != nil {
		return "", nil, err
	}
	if len(words) == 0 || strings.Contains(words[0], "=") {
		return "", nil, errors.New("start with a name")
	}
	return words[0], words[1:], nil
}

func putKey(m map[string]string, key, value string) map[string]string {
	if m == nil {
		m = map[string]string{}
	}
	m[key] = value
	return m
}

// volumeCreateLine reads "name driver=x opt.k=v label.k=v".
func volumeCreateLine(line string) (containers.VolumeCreateOptions, error) {
	name, rest, err := optionWords(line)
	if err != nil {
		return containers.VolumeCreateOptions{}, err
	}
	o := containers.VolumeCreateOptions{Name: name}
	for _, w := range rest {
		k, v, ok := strings.Cut(w, "=")
		switch {
		case !ok:
			return o, fmt.Errorf("%q: use key=value", w)
		case k == "driver":
			o.Driver = v
		case strings.HasPrefix(k, "opt."):
			o.Options = putKey(o.Options, strings.TrimPrefix(k, "opt."), v)
		case strings.HasPrefix(k, "label."):
			o.Labels = putKey(o.Labels, strings.TrimPrefix(k, "label."), v)
		default:
			return o, fmt.Errorf("unknown option %q (driver, opt.key, label.key)", k)
		}
	}
	if _, err := containers.VolumeCreateArgs(o); err != nil {
		return o, err
	}
	return o, nil
}

// networkCreateLine reads "name driver=x subnet=… gateway=… ip-range=…
// internal attachable ipv6 label.k=v".
func networkCreateLine(line string) (containers.NetworkCreateOptions, error) {
	name, rest, err := optionWords(line)
	if err != nil {
		return containers.NetworkCreateOptions{}, err
	}
	o := containers.NetworkCreateOptions{Name: name}
	for _, w := range rest {
		k, v, ok := strings.Cut(w, "=")
		if !ok {
			switch w {
			case "internal":
				o.Internal = true
			case "attachable":
				o.Attachable = true
			case "ipv6":
				o.IPv6 = true
			default:
				return o, fmt.Errorf("unknown flag %q (internal, attachable, ipv6)", w)
			}
			continue
		}
		switch {
		case k == "driver":
			o.Driver = v
		case k == "subnet":
			o.Subnet = v
		case k == "gateway":
			o.Gateway = v
		case k == "ip-range":
			o.IPRange = v
		case strings.HasPrefix(k, "label."):
			o.Labels = putKey(o.Labels, strings.TrimPrefix(k, "label."), v)
		default:
			return o, fmt.Errorf("unknown option %q (driver, subnet, gateway, ip-range, label.key)", k)
		}
	}
	if _, err := containers.NetworkCreateArgs(o); err != nil {
		return o, err
	}
	return o, nil
}

// volumeListingText is the browse page of one folder of a volume.
func volumeListingText(volume, dir string, entries []containers.VolumeEntry) string {
	dir = path.Clean("/" + dir)
	var b strings.Builder
	b.WriteString(volume + ":" + dir + "\n\n")
	if len(entries) == 0 {
		b.WriteString("Empty folder.\n")
		return b.String()
	}
	dirs, files := 0, 0
	for _, e := range entries {
		if e.Dir {
			dirs++
			b.WriteString("  " + e.Name + "/\n")
		} else {
			files++
			b.WriteString("  " + e.Name + "\n")
		}
	}
	b.WriteString("\n" + countWord(dirs, "folder") + ", " + countWord(files, "file") + "\n")
	return b.String()
}

func countWord(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// volumeBackupName is the backup file name of a volume at a time.
func volumeBackupName(volume string, at time.Time) string {
	return volume + "-" + at.Format("20060102-150405") + ".tar.gz"
}

// splitArchivePath is the absolute folder and the file name of a typed
// backup path (relative = the workspace folder).
func splitArchivePath(root, typed string) (string, string, error) {
	p := archivePath(root, typed)
	if p == "" {
		return "", "", errors.New("a backup file is needed")
	}
	return filepath.Dir(p), filepath.Base(p), nil
}

func (m *Model) handleVolumePrompt(kind promptKindEnum, value string) tea.Cmd {
	volume := m.services.pendingArchive
	m.services.pendingArchive = ""
	var (
		args []string
		err  error
		job  containersExtraJob
	)
	switch kind {
	case promptKindVolumeBrowse:
		return m.volumeBrowsePage(volume, value)
	case promptKindVolumeBackup:
		dir := archivePath(m.services.root, value)
		file := volumeBackupName(volume, time.Now())
		args, err = containers.VolumeBackupArgs(volume, dir, file)
		job = containersExtraJob{label: "back up " + volume + " to " + filepath.Join(dir, file), args: args, timeout: volumeJobTimeout}
	case promptKindVolumeRestore:
		var dir, file string
		dir, file, err = splitArchivePath(m.services.root, value)
		if err == nil {
			args, err = containers.VolumeRestoreArgs(volume, dir, file)
		}
		if err == nil {
			job = containersExtraJob{label: "restore " + volume + " from " + file, section: "volumes", args: args, timeout: volumeJobTimeout}
			return m.askContainersExtra("Restore volume?", "This unpacks "+filepath.Join(dir, file)+" into volume "+volume+". Files with the same name are overwritten. Stop the containers that use it first.\n\nCommand: "+m.containersCommandLine(args),
				[]confirm.Button{{ID: "run", Title: "Restore", Style: confirm.StyleDestructive}}, map[string]containersExtraJob{"run": job})
		}
	case promptKindVolumeClone:
		dst := strings.TrimSpace(value)
		args, err = containers.VolumeCloneArgs(volume, dst)
		if err == nil {
			job = containersExtraJob{label: "clone " + volume + " to " + dst, section: "volumes", args: args, timeout: volumeJobTimeout}
			body := "This copies every file of " + volume + " into " + dst + ". If " + dst + " exists, files with the same name are overwritten."
			for _, v := range m.services.volumeRows {
				if v.Name == dst {
					body = "Volume " + dst + " exists: files of " + volume + " overwrite files with the same name in it."
				}
			}
			return m.askContainersExtra("Clone volume?", body+"\n\nCommand: "+m.containersCommandLine(args),
				[]confirm.Button{{ID: "run", Title: "Clone", Style: confirm.StyleDestructive}}, map[string]containersExtraJob{"run": job})
		}
	case promptKindVolumeCreate:
		var o containers.VolumeCreateOptions
		if o, err = volumeCreateLine(value); err == nil {
			args, err = containers.VolumeCreateArgs(o)
			job = containersExtraJob{label: "create volume " + o.Name, section: "volumes", args: args, timeout: time.Minute}
		}
	case promptKindNetworkCreate:
		var o containers.NetworkCreateOptions
		if o, err = networkCreateLine(value); err == nil {
			args, err = containers.NetworkCreateArgs(o)
			job = containersExtraJob{label: "create network " + o.Name, section: "networks", args: args, timeout: time.Minute}
		}
	}
	if err != nil {
		return m.servicesToast(toast.Info, "Not run", err.Error())
	}
	return m.runContainersExtraJob(job)
}

func (m *Model) volumeBrowsePage(volume, dir string) tea.Cmd {
	e, t, ok := m.containersEngineOrToast()
	if !ok {
		return t
	}
	args, err := containers.VolumeBrowseArgs(volume, dir)
	if err != nil {
		return m.servicesToast(toast.Info, "Not shown", err.Error())
	}
	execFor := m.services.hostExec()
	argv := append([]string{e.Binary}, args...)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		out, stderr, code, err := execFor.Capture(ctx, "", argv...)
		if err == nil && code != 0 {
			err = errString(errLine(stderr))
		}
		return containersPageMsg{title: "volume " + volume + ":" + path.Clean("/"+dir), text: volumeListingText(volume, dir, containers.ParseVolumeListing(out)), err: err}
	}
}
