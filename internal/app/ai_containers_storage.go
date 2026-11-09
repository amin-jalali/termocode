package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/containers"
)

// containers_storage: the volumes and networks of the Containers view on
// any host (create with options, remove, browse, back up, restore, clone,
// disk usage). containers_prune: every prune of the GUI (stopped
// containers, untagged or all unused images, unused networks and volumes,
// the system, the build cache).
//
// They ask where the GUI asks: remove, restore, clone and every prune are
// the GUI's cards and ask every time, Cancel first, with no "Always
// allow"; create and back up ask with the containers class (the GUI's
// prompts); browse and disk usage read at once. A volume a container
// uses and a built-in network are refused, as the GUI refuses them.

var aiStorageOps = []string{"volume_create", "network_create", "volume_remove", "network_remove", "volume_browse",
	"volume_backup", "volume_restore", "volume_clone", "disk_usage"}

var aiStorageCards = map[string]bool{"volume_remove": true, "network_remove": true, "volume_restore": true, "volume_clone": true}

var aiPruneKinds = []string{pruneContainers, pruneImages, pruneImagesAll, pruneNetworks, pruneVolumes, pruneSystem, "build-cache"}

// aiStorageReq is one checked containers_storage call.
type aiStorageReq struct {
	op   string
	h    aiContainersHost
	name string
	argv []string // without the binary
	note string   // the GUI card's sentence
	wait bool     // a helper-container job (long)
	dir  string   // volume_backup / volume_restore: the folder
}

// aiLocalEngine: the engine runs on this machine (a backup folder is a
// folder of this machine only then).
func aiLocalEngine(ctx context.Context, h aiContainersHost) bool {
	if h.picked != "" {
		return h.where == "this machine"
	}
	list, err := webHostContexts(ctx, h)
	if err != nil {
		return h.session == ""
	}
	for _, c := range list {
		if aiContextIsSession(h, c) {
			return serverTarget(c) == "this machine"
		}
	}
	return h.session == ""
}

func aiStorageParse(ctx context.Context, call aiIDECaller, a map[string]any) (aiStorageReq, error) {
	r := aiStorageReq{op: ai.ArgString(a, "op"), name: strings.TrimSpace(ai.ArgString(a, "name"))}
	if !contains(aiStorageOps, r.op) {
		return r, errors.New("give op: " + strings.Join(aiStorageOps, ", "))
	}
	if err := aiValidContextArg(a); err != nil {
		return r, err
	}
	if r.op != "disk_usage" {
		if err := containers.ValidName(r.name); err != nil {
			return r, fmt.Errorf("name: %v", err)
		}
	}
	h, err := aiContainersHostAt(ctx, call, a)
	r.h = h
	if err != nil {
		return r, err
	}
	labels := func() (map[string]string, error) {
		list, err := aiArgList(a, "labels")
		if err != nil || len(list) == 0 {
			return nil, err
		}
		out := map[string]string{}
		for _, kv := range list {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || k == "" {
				return nil, fmt.Errorf("label %q: write KEY=value", kv)
			}
			out[k] = v
		}
		return out, nil
	}
	volumes := func() error {
		list, err := h.e.ListVolumes(ctx)
		if err != nil {
			return errors.New(aiContainersRedact(err.Error()))
		}
		var names []string
		for _, v := range list {
			if v.Name == r.name {
				return nil
			}
			names = append(names, v.Name)
		}
		return fmt.Errorf("no volume %q on this host; volumes: %s (see volumes_list)", r.name, strings.Join(names, ", "))
	}
	backupDir := func() (string, string, error) {
		if !aiLocalEngine(ctx, h) {
			return "", "", errors.New("the backup folder is a folder of the engine's machine: this works on a host of this machine only")
		}
		dir, err := aiDBInside(aiCtrRoot(h), strings.TrimSpace(ai.ArgString(a, "dir")))
		if err != nil {
			return "", "", fmt.Errorf("dir: %v", err)
		}
		return dir, strings.TrimSpace(ai.ArgString(a, "file")), nil
	}
	switch r.op {
	case "volume_create":
		o := containers.VolumeCreateOptions{Name: r.name, Driver: strings.TrimSpace(ai.ArgString(a, "driver"))}
		if o.Labels, err = labels(); err != nil {
			return r, err
		}
		if r.argv, err = containers.VolumeCreateArgs(o); err != nil {
			return r, err
		}
	case "network_create":
		o := containers.NetworkCreateOptions{Name: r.name, Driver: strings.TrimSpace(ai.ArgString(a, "driver")),
			Subnet: strings.TrimSpace(ai.ArgString(a, "subnet")), Gateway: strings.TrimSpace(ai.ArgString(a, "gateway")),
			Internal: ai.ArgBool(a, "internal"), Attachable: ai.ArgBool(a, "attachable"), IPv6: ai.ArgBool(a, "ipv6")}
		if o.Labels, err = labels(); err != nil {
			return r, err
		}
		if r.argv, err = containers.NetworkCreateArgs(o); err != nil {
			return r, err
		}
	case "network_remove":
		if webBuiltinNetwork(r.name) {
			return r, fmt.Errorf("%s is a built-in network; it cannot be removed", r.name)
		}
		list, err := h.e.ListNetworks(ctx)
		if err != nil {
			return r, errors.New(aiContainersRedact(err.Error()))
		}
		found := false
		var names []string
		for _, n := range list {
			found = found || n.Name == r.name
			names = append(names, n.Name)
		}
		if !found {
			return r, fmt.Errorf("no network %q on this host; networks: %s (see networks_list)", r.name, strings.Join(names, ", "))
		}
		r.argv = []string{"network", "rm", r.name}
		r.note = "Containers still on it keep running but lose it."
	case "volume_remove", "volume_browse", "volume_backup", "volume_restore", "volume_clone":
		if err := volumes(); err != nil {
			return r, err
		}
		switch r.op {
		case "volume_remove":
			if users, err := h.e.ContainersUsingVolume(ctx, r.name); err == nil && len(users) > 0 {
				var names []string
				for _, c := range users {
					names = append(names, aiContainerName(c))
				}
				return r, fmt.Errorf("the volume stays while containers use it: %s", strings.Join(names, ", "))
			}
			r.argv = []string{"volume", "rm", r.name}
			r.note = "This permanently removes its data; this cannot be undone."
		case "volume_browse":
			p := strings.TrimSpace(ai.ArgString(a, "path"))
			if r.argv, err = containers.VolumeBrowseArgs(r.name, p); err != nil {
				return r, err
			}
			r.wait = true
		case "volume_backup", "volume_restore":
			dir, file, err := backupDir()
			if err != nil {
				return r, err
			}
			r.dir = dir
			if r.op == "volume_backup" {
				r.argv, err = containers.VolumeBackupArgs(r.name, dir, file)
			} else {
				if _, serr := os.Stat(filepath.Join(dir, file)); serr != nil {
					return r, fmt.Errorf("no backup %s in %s", file, dir)
				}
				r.argv, err = containers.VolumeRestoreArgs(r.name, dir, file)
				r.note = "Files of the backup overwrite the volume's files."
			}
			if err != nil {
				return r, err
			}
			r.wait = true
		case "volume_clone":
			to := strings.TrimSpace(ai.ArgString(a, "to"))
			if err := containers.ValidName(to); err != nil {
				return r, fmt.Errorf("to: %v", err)
			}
			if r.argv, err = containers.VolumeCloneArgs(r.name, to); err != nil {
				return r, err
			}
			r.note = "Files of " + r.name + " overwrite files of " + to + " (made when missing)."
			r.wait = true
		}
	}
	return r, nil
}

func aiStorageDescribe(call aiIDECaller, a map[string]any) string {
	op, name := ai.ArgString(a, "op"), strings.TrimSpace(ai.ArgString(a, "name"))
	words := strings.SplitN(op, "_", 2)
	title := op
	if len(words) == 2 {
		title = aiTitle(words[1]) + " " + words[0]
	}
	ctx, cancel := aiContainersDescribeCtx()
	defer cancel()
	r, err := aiStorageParse(ctx, call, a)
	if err != nil {
		return title + " " + name + " (refused: " + aiOneLineClip(err.Error(), 120) + ")"
	}
	if r.op == "disk_usage" || r.op == "volume_browse" {
		return title + " " + name
	}
	s := fmt.Sprintf("%s %s (%s) · %s", title, name, svcShellLine(append([]string{r.h.bin()}, r.argv...)), r.h.contextLabel(ctx))
	if r.note != "" {
		s += " · " + r.note
	}
	return s
}

func aiStorageRun(ctx context.Context, call aiIDECaller, a map[string]any) (string, error) {
	r, err := aiStorageParse(ctx, call, a)
	if err != nil {
		return "", err
	}
	h := r.h
	if r.op == "disk_usage" {
		rows, err := h.e.DiskSummary(ctx)
		if err != nil {
			return "", errors.New(aiContainersRedact(err.Error()))
		}
		var b strings.Builder
		b.WriteString(h.header(ctx) + "\nDisk usage (type · total · active · size · reclaimable)\n")
		for _, d := range rows {
			fmt.Fprintf(&b, "- %s · %s · %s · %s · %s\n", d.Type, d.Total, d.Active, d.Size, d.Reclaimable)
		}
		if vols, err := h.e.ListDanglingVolumes(ctx); err == nil {
			fmt.Fprintf(&b, "Unused volumes: %d (containers_prune kind volumes removes them, asking first)\n", len(vols))
		}
		return aiContainersRedact(strings.TrimRight(b.String(), "\n")), nil
	}
	wait := aiContainersCallWait
	if r.wait {
		wait = volumeJobTimeout
	}
	if r.op == "volume_backup" {
		if err := os.MkdirAll(r.dir, 0o755); err != nil {
			return "", err
		}
	}
	out, err := aiEngineRun(ctx, h, wait, r.argv...)
	if err != nil {
		return "", fmt.Errorf("%s failed: %v", strings.ReplaceAll(r.op, "_", " "), err)
	}
	if r.op == "volume_browse" {
		ents := containers.ParseVolumeListing(out)
		var b strings.Builder
		fmt.Fprintf(&b, "%s\nVolume %s:%s: %d entries\n", h.header(ctx), r.name, path.Clean("/"+ai.ArgString(a, "path")), len(ents))
		for i, e := range ents {
			if i == aiContainersListLimit {
				fmt.Fprintf(&b, "… %d more\n", len(ents)-i)
				break
			}
			if e.Dir {
				b.WriteString("- " + e.Name + "/\n")
			} else {
				b.WriteString("- " + e.Name + "\n")
			}
		}
		return aiContainersRedact(strings.TrimRight(b.String(), "\n")), nil
	}
	section := "volumes"
	if strings.HasPrefix(r.op, "network") {
		section = "networks"
	}
	_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) { return "", m.containersRefresh(section), nil })
	return aiContainersRedact(fmt.Sprintf("ran `%s` · %s", svcShellLine(append([]string{h.bin()}, r.argv...)), h.contextLabel(ctx))), nil
}

// aiPruneArgv is the argv (without the binary) and the card of a kind.
func aiPruneArgv(h aiContainersHost, kind string, all bool) ([]string, [2]string, error) {
	if !contains(aiPruneKinds, kind) {
		return nil, [2]string{}, errors.New("kind must be one of " + strings.Join(aiPruneKinds, ", "))
	}
	if kind == "build-cache" {
		txt := [2]string{"Remove the build cache?", "Build cache no image needs goes; the next builds are slower."}
		if all {
			txt[1] = "Every build cache entry goes; the next builds start from scratch."
		}
		return h.e.BuildCachePruneArgs(all), txt, nil
	}
	return pruneArgv(h.e.Binary, kind)[1:], webPruneText[kind], nil
}

func (m *Model) aiContainersStorageTools(call aiIDECaller) []ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	boo := func(d string) map[string]any { return map[string]any{"type": "boolean", "description": d} }
	session := aiContainersGate(m.services)
	host := aiContextParam()
	return []ai.Tool{
		{
			Name: "containers_storage",
			Description: "Volumes and networks of a host, as the Containers view. volume_create (name, driver, labels) and network_create " +
				"(name, driver, subnet, gateway, internal, attachable, ipv6, labels) ask; volume_remove (never one a container uses), " +
				"network_remove (never a built-in), volume_restore (name, dir, file) and volume_clone (name, to) ask the user every time; " +
				"volume_backup (name, dir: a workspace folder, file: like db.tgz) asks; volume_browse (name, path) and disk_usage read at once.",
			Params: map[string]any{
				"op":      map[string]any{"type": "string", "enum": aiStorageOps, "description": "what to do"},
				"name":    str("the volume or network (volumes_list, networks_list)"),
				"context": host,
				"driver":  str("create: the driver"), "subnet": str("network_create: a CIDR like 10.9.0.0/24"),
				"gateway": str("network_create: the gateway IP"), "internal": boo("network_create: no outside access"),
				"attachable": boo("network_create: containers may join it by hand"), "ipv6": boo("network_create: IPv6"),
				"labels": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "create: KEY=value labels"},
				"path":   str("volume_browse: a folder in the volume (default /)"),
				"dir":    str("volume_backup / volume_restore: the folder, relative to the workspace"),
				"file":   str("volume_backup / volume_restore: the archive name like db.tgz"),
				"to":     str("volume_clone: the target volume"),
			},
			Required: []string{"op"},
			Mutating: true,
			Gate: func(a map[string]any) (bool, string) {
				op := ai.ArgString(a, "op")
				switch {
				case op == "volume_browse" || op == "disk_usage":
					return false, ""
				case aiStorageCards[op] || !contains(aiStorageOps, op):
					return true, ""
				}
				return session(a)
			},
			Validate: func(a map[string]any) error {
				ctx, cancel := context.WithTimeout(context.Background(), aiContainersCallWait)
				defer cancel()
				_, err := aiStorageParse(ctx, call, a)
				return err
			},
			Describe: func(a map[string]any) string { return aiStorageDescribe(call, a) },
			Run:      func(ctx context.Context, a map[string]any) (string, error) { return aiStorageRun(ctx, call, a) },
		},
		{
			Name: "containers_prune",
			Description: "Prune a host as the Containers view does: kind containers (stopped ones), images (untagged), images-all " +
				"(every unused image), networks, volumes (unused; their data is deleted), system or build-cache (all: every entry). " +
				"Asks the user every time.",
			Params: map[string]any{
				"kind":    map[string]any{"type": "string", "enum": aiPruneKinds, "description": "what to prune"},
				"all":     boo("build-cache: every entry, not only unused ones"),
				"context": host,
			},
			Required: []string{"kind"},
			Mutating: true,
			Gate:     func(map[string]any) (bool, string) { return true, "" },
			Validate: func(a map[string]any) error {
				if err := aiValidContextArg(a); err != nil {
					return err
				}
				_, _, err := aiPruneArgv(aiContainersHost{}, ai.ArgString(a, "kind"), false)
				return err
			},
			Describe: func(a map[string]any) string {
				kind := ai.ArgString(a, "kind")
				ctx, cancel := aiContainersDescribeCtx()
				defer cancel()
				h, err := aiContainersHostAt(ctx, call, a)
				if err != nil {
					return "Prune " + kind + " (refused: " + aiOneLineClip(err.Error(), 120) + ")"
				}
				argv, txt, err := aiPruneArgv(h, kind, ai.ArgBool(a, "all"))
				if err != nil {
					return "Prune " + kind + " (refused: " + err.Error() + ")"
				}
				return fmt.Sprintf("%s (%s) · on %s: %s", strings.TrimSuffix(txt[0], "?"), svcShellLine(append([]string{h.bin()}, argv...)), h.contextLabel(ctx), txt[1])
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				kind := ai.ArgString(a, "kind")
				h, err := aiContainersHostAt(ctx, call, a)
				if err != nil {
					return "", err
				}
				argv, _, err := aiPruneArgv(h, kind, ai.ArgBool(a, "all"))
				if err != nil {
					return "", err
				}
				out, err := aiEngineRun(ctx, h, volumeJobTimeout, argv...)
				if err != nil {
					return "", fmt.Errorf("prune %s failed: %v", kind, err)
				}
				_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) { return "", m.containersRefresh(pruneSection(kind)), nil })
				res := fmt.Sprintf("ran `%s` · %s", svcShellLine(append([]string{h.bin()}, argv...)), h.contextLabel(ctx))
				if l := sanitizeOutputLine(errLine(lastLine(out))); l != "" {
					res += ": " + l
				}
				return aiContainersRedact(res), nil
			},
		},
	}
}
