package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Prune kinds. "images-all" is the separate "incl. unused images" item.
// System prune never adds the flags for tagged images or volumes: those
// stay reachable only from the IMAGES and VOLUMES headers.
const (
	pruneContainers = "containers"
	pruneImages     = "images"
	pruneImagesAll  = "images-all"
	pruneNetworks   = "networks"
	pruneVolumes    = "volumes"
	pruneSystem     = "system"
)

// pruneSection is the section a prune kind changes (refreshed after).
func pruneSection(kind string) string {
	if kind == pruneImagesAll {
		return "images"
	}
	return kind
}

// pruneArgv is the exact command of a kind.
func pruneArgv(binary, kind string) []string {
	if binary == "" {
		binary = "docker"
	}
	switch kind {
	case pruneContainers:
		return []string{binary, "container", "prune", "-f"}
	case pruneImages:
		return []string{binary, "image", "prune", "-f"}
	case pruneImagesAll:
		return []string{binary, "image", "prune", "-a", "-f"}
	case pruneNetworks:
		return []string{binary, "network", "prune", "-f"}
	case pruneVolumes:
		return []string{binary, "volume", "prune", "-f"}
	default:
		return []string{binary, "system", "prune", "-f"}
	}
}

func countOf(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// stoppedContainerCount counts containers the engine would remove.
func (st *servicesState) stoppedContainerCount() int {
	n := 0
	for _, c := range st.containerRows {
		switch strings.ToLower(c.State) {
		case "exited", "created", "dead":
			n++
		}
	}
	return n
}

func (st *servicesState) danglingImageCount() int {
	n := 0
	for _, image := range st.imageRows {
		if len(image.RepoTags) == 0 || (len(image.RepoTags) == 1 && image.RepoTags[0] == "<none>:<none>") {
			n++
		}
	}
	return n
}

// unusedImageCount counts images no container uses.
func (st *servicesState) unusedImageCount() int {
	used := map[string]bool{}
	for _, c := range st.containerRows {
		used[strings.TrimPrefix(c.ImageID, "sha256:")] = true
		used[c.Image] = true
	}
	n := 0
	for _, image := range st.imageRows {
		inUse := used[strings.TrimPrefix(image.ID, "sha256:")]
		for _, tag := range image.RepoTags {
			if used[tag] {
				inUse = true
			}
		}
		if !inUse {
			n++
		}
	}
	return n
}

func (st *servicesState) customNetworkCount() int {
	n := 0
	for _, nw := range st.networkRows {
		switch nw.Name {
		case "bridge", "host", "none":
		default:
			n++
		}
	}
	return n
}

// askPrune opens the prune card of a kind. The volumes card first lists
// the dangling volumes (one engine call), so it returns a command.
func (m *Model) askPrune(kind string) tea.Cmd {
	if kind != pruneVolumes {
		m.showPrune(kind, nil)
		return nil
	}
	e := m.services.containerEngine
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		rows, err := e.ListDanglingVolumes(ctx)
		var names []string
		if err == nil {
			for _, volume := range rows {
				if isAnonymousVolume(volume.Name) {
					names = append(names, volume.Name)
				}
			}
		}
		return storagePrunePreviewMsg{section: kind, volumes: names, err: err}
	}
}

// showPrune builds the card: what goes (counts from the last snapshot),
// the exact command, one destructive button. Esc and Cancel run nothing.
func (m *Model) showPrune(kind string, volumes []string) {
	st := m.services
	args := pruneArgv(st.containerEngine.Binary, kind)
	where := st.containerEngine.Kind
	if where == "" {
		where = "engine"
	}
	if ctx := st.activeContext(); ctx != "" && ctx != "default" {
		where += " · context " + sanitizeOutputLine(ctx)
	}
	head := "On this host (" + where + ") this removes:\n"
	var title, what, keep string
	switch kind {
	case pruneContainers:
		title = "Remove stopped containers?"
		what = "  " + countOf(st.stoppedContainerCount(), "stopped container", "stopped containers") + "."
		keep = "Running and paused containers stay."
	case pruneImages:
		title = "Remove untagged images?"
		what = "  " + countOf(st.danglingImageCount(), "untagged image", "untagged images") + "."
		keep = "Only unused images without a tag go. Tagged images stay."
	case pruneImagesAll:
		title = "Remove all unused images?"
		what = "  every image no container uses (" + countOf(st.unusedImageCount(), "image", "images") + " now), tagged ones too."
		keep = "Images used by a container stay. You can pull a removed image again."
	case pruneNetworks:
		title = "Remove unused networks?"
		what = "  unused networks (" + countOf(st.customNetworkCount(), "custom network", "custom networks") + " listed now)."
		keep = "The engine decides which are unused. Built-in networks stay."
	case pruneVolumes:
		title = "Remove unused volumes?"
		what = "  unnamed volumes no container uses. Their data is deleted."
		if st.containerEngine.Kind == "podman" {
			keep = "Podman may remove unused named volumes too. The list below shows only unnamed volumes."
		} else {
			keep = "Volumes with a name stay."
		}
		if len(volumes) == 0 {
			keep += "\n\nNo unnamed volumes were found."
		} else {
			short := make([]string, 0, len(volumes))
			for _, name := range volumes {
				if len(name) > 12 {
					name = name[:12]
				}
				short = append(short, name)
			}
			keep += "\n\nUnnamed volumes (their data is deleted):\n" + strings.Join(short, "\n")
		}
	default:
		kind = pruneSystem
		title = "Clean up unused items?"
		what = "  " + countOf(st.stoppedContainerCount(), "stopped container", "stopped containers") + " · " +
			countOf(st.danglingImageCount(), "untagged image", "untagged images") + " · unused networks · old build files."
		keep = "Volumes and tagged images stay."
	}
	verb := "Remove"
	if kind == pruneSystem {
		verb = "Clean up"
	}
	m.confirm = confirm.New(title, head+what+"\n"+keep+"\n\nCommand: "+svcShellLine(args), []confirm.Button{
		{ID: "cancel", Title: "Cancel"}, {ID: "prune", Title: verb, Style: confirm.StyleDestructive},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerHostAction
	m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = args, "prune-"+kind, pruneSection(kind)
}

// pruneReclaimed finds the engine's reclaimed text in the last line.
func pruneReclaimed(output string) (reclaimed, last string) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	last = strings.TrimSpace(lines[len(lines)-1])
	if i := strings.Index(strings.ToLower(last), "reclaimed"); i >= 0 {
		if _, after, ok := strings.Cut(last[i:], ":"); ok {
			return strings.TrimSpace(after), last
		}
	}
	return "", last
}

// onPruneDone is the toast and the refresh after a prune succeeded.
func (m *Model) onPruneDone(msg hostActionDoneMsg) tea.Cmd {
	reclaimed, last := pruneReclaimed(msg.output)
	title, detail := "Pruned", ""
	if reclaimed != "" {
		title = "Pruned · reclaimed " + sanitizeOutputLine(reclaimed)
	} else if last != "" {
		detail = sanitizeOutputLine(last)
	}
	refresh := m.containersRefresh(msg.section)
	if msg.operation == "prune-system" {
		refresh = m.containersReloadAll()
	}
	return tea.Batch(m.servicesToast(toast.Info, title, detail), refresh)
}

// openSectionMenu is the right-click menu of a section header: the
// prune items of CONTAINERS, IMAGES, NETWORKS and VOLUMES.
func (m *Model) openSectionMenu(r svcRow, x, y int) {
	var items []menu.Item
	icon := theme.GlyphPrune.String()
	switch r.hostSectionID() {
	case "containers":
		items = []menu.Item{{ID: "prune-containers", Title: "Prune stopped containers…", Hint: "z", Icon: icon}}
	case "images":
		items = []menu.Item{
			{ID: "prune-images", Title: "Prune dangling images…", Hint: "z", Icon: icon},
			{ID: "prune-images-all", Title: "Prune incl. unused images…", Icon: icon},
		}
	case "networks":
		items = []menu.Item{{ID: "prune-networks", Title: "Prune unused networks…", Hint: "z", Icon: icon}}
	case "volumes":
		items = []menu.Item{{ID: "prune-volumes", Title: "Prune dangling volumes…", Hint: "z", Icon: icon}}
	case "registries":
		items = []menu.Item{{ID: "connect", Title: "Connect registry…", Hint: "n"},
			{ID: "browse", Title: "Browse tags…", Hint: "b"},
			{ID: "search", Title: "Search Docker Hub…", Hint: "s"}}
	default:
		return
	}
	m.services.menuRow = r
	m.menu = menu.New(items, x, y)
	m.menu.SetScreenSize(m.w, m.h)
	m.menuOpen, m.menuKind = true, menuKindServices
}

func (m *Model) handleSectionMenuSelect(id string) tea.Cmd {
	if kind, ok := strings.CutPrefix(id, "prune-"); ok {
		return m.askPrune(kind)
	}
	switch id {
	case "connect":
		m.startRegistryConnect("")
	case "browse":
		m.askBrowse("")
	case "search":
		m.askSearch()
	}
	return nil
}
