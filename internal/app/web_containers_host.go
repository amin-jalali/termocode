package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The rest of the Containers view from the page (web parity, Containers
// area). Reads list the host (images, networks, volumes, contexts,
// registries), inspect one item, read an image's history, one stats
// sample, the disk usage, a Docker Hub search and a repository's tags.
// Every engine call runs off the UI thread with a time limit; env
// values, auth fields and secret-looking labels are masked on the hub.
//
// Pause, tag, create, compose up / restart, the session's context, start
// all services and the services.json template run at once (the page
// asked first). Everything else waits for a laptop card and then runs
// the laptop's own command: a shell, pull, push, run or compose stop /
// down in a laptop terminal (the page sees it in its terminals), a
// prune, rmi, network or volume rm, logout or `context use` captured. A
// registry sign-in and Open in Databases open the laptop's own prompt:
// a password is typed there, never in the page. An item is named only by
// what the last listing gave (containers.list, containers.host).

// webCtrTerminal runs a container command in a laptop terminal tab (a
// seam for tests).
var webCtrTerminal = func(m *Model, c services.Command, key string) error { return m.servicesRunCommand(c, key) }

// webSvcAll is the laptop's start-all or stop-all of the project's
// services (a seam for tests).
var webSvcAll = func(m *Model, verb string) tea.Cmd { return m.servicesAction(verb) }

func init() {
	webOps[webproto.OpContainersHost] = (*Model).webContainersHostList
	webOps[webproto.OpContainersInspect] = (*Model).webContainersInspect
	webOps[webproto.OpContainersHistory] = (*Model).webContainersHistory
	webOps[webproto.OpContainersStats] = (*Model).webContainersStats
	webOps[webproto.OpContainersDiskUsage] = (*Model).webContainersDiskUsage
	webOps[webproto.OpContainersSearch] = (*Model).webContainersSearch
	webOps[webproto.OpContainersTags] = (*Model).webContainersTags
	webOps[webproto.OpContainersPause] = func(m *Model, call *webCall) tea.Cmd { return m.webContainersAct(call, "pause") }
	webOps[webproto.OpContainersTag] = (*Model).webContainersTag
	webOps[webproto.OpContainersCreate] = (*Model).webContainersCreate
	webOps[webproto.OpContainersCompose] = (*Model).webContainersCompose
	webOps[webproto.OpContainersContext] = (*Model).webContainersContext
	webOps[webproto.OpServicesStartAll] = (*Model).webServicesStartAll
	webOps[webproto.OpServicesConfigure] = (*Model).webServicesConfigure
	webAskOps[webproto.OpContainersExec] = (*Model).webContainersExec
	webAskOps[webproto.OpContainersImage] = (*Model).webContainersImage
	webAskOps[webproto.OpContainersPrune] = (*Model).webContainersPrune
	webAskOps[webproto.OpContainersStorageRemove] = (*Model).webContainersStorageRemove
	webAskOps[webproto.OpContainersComposeAsk] = (*Model).webContainersComposeAsk
	webAskOps[webproto.OpContainersContextDefault] = (*Model).webContainersContextDefault
	webAskOps[webproto.OpContainersRegistry] = (*Model).webContainersRegistry
	webAskOps[webproto.OpContainersOpenDB] = (*Model).webContainersOpenDB
	webAskOps[webproto.OpServicesStopAll] = (*Model).webServicesStopAll
}

// webHostSeen is what containers.host listed last (Update only).
type webHostSeen struct {
	images     map[string]containers.Image // by each tag and by its short id
	inUse      map[string]bool             // the same keys: a container uses it
	networks   map[string]bool             // name → built in
	volumes    map[string]bool
	contexts   map[string]bool
	registries map[string]bool // host → signed in
}

var errHostNotListed = errors.New("That item is not in the list. Open the list again.")

func webShortImageID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		id = id[:12]
	}
	return id
}

func webImageTags(im containers.Image) []string {
	out := []string{}
	for _, t := range im.RepoTags {
		if t != "" && t != "<none>:<none>" {
			out = append(out, t)
		}
	}
	return out
}

func webBuiltinNetwork(name string) bool { return name == "bridge" || name == "host" || name == "none" }

// webHostContexts lists the engine's contexts (podman: connections).
func webHostContexts(ctx context.Context, h aiContainersHost) ([]containers.Context, error) {
	if h.e.Kind == "podman" {
		return h.e.ListConnections(ctx)
	}
	return h.e.ListContexts(ctx)
}

// webContainersHostList answers everything of the host but its
// containers and keeps it for the actions.
func (m *Model) webContainersHostList(call *webCall) tea.Cmd {
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, webContainersWait)
		defer cancel()
		out := webproto.ContainersHost{Engine: h.bin(), Context: h.contextLabel(ctx), Images: []webproto.ImageItem{}, Networks: []webproto.NetworkItem{},
			Volumes: []webproto.VolumeItem{}, Contexts: []webproto.ContextItem{}, Registries: []webproto.RegistryItem{}}
		seen := &webHostSeen{images: map[string]containers.Image{}, inUse: map[string]bool{}, networks: map[string]bool{}, volumes: map[string]bool{},
			contexts: map[string]bool{}, registries: map[string]bool{}}
		problem := func(sec string, err error) {
			if out.Problems == nil {
				out.Problems = map[string]string{}
			}
			out.Problems[sec] = errLine(aiContainersRedact(err.Error()))
		}
		used := map[string]bool{}
		var hosts []containers.Context // the hosts tree (web_servers.go)
		if list, err := h.e.ListContainers(ctx); err == nil {
			for _, c := range list {
				used[webShortImageID(c.ImageID)] = true
				used[c.Image] = true
			}
		}
		if ims, err := h.e.ListImages(ctx); err != nil {
			problem("images", err)
		} else {
			for i, im := range ims {
				if i == webproto.MaxHostItems {
					break
				}
				id := webShortImageID(im.ID)
				it := webproto.ImageItem{ID: id, Tags: webImageTags(im), Size: webName(im.Size), Created: webName(im.CreatedAt), InUse: used[id]}
				for _, t := range it.Tags {
					it.InUse = it.InUse || used[t]
				}
				for _, k := range append([]string{id}, it.Tags...) {
					seen.images[k], seen.inUse[k] = im, it.InUse
				}
				out.Images = append(out.Images, it)
			}
		}
		if nws, err := h.e.ListNetworks(ctx); err != nil {
			problem("networks", err)
		} else {
			for i, n := range nws {
				if i == webproto.MaxHostItems {
					break
				}
				b := webBuiltinNetwork(n.Name)
				out.Networks = append(out.Networks, webproto.NetworkItem{Name: webName(n.Name), Driver: webName(n.Driver), Scope: webName(n.Scope), BuiltIn: b})
				seen.networks[n.Name] = b
			}
		}
		if vols, err := h.e.ListVolumes(ctx); err != nil {
			problem("volumes", err)
		} else {
			for i, v := range vols {
				if i == webproto.MaxHostItems {
					break
				}
				out.Volumes = append(out.Volumes, webproto.VolumeItem{Name: webName(v.Name), Driver: webName(v.Driver), Size: webName(v.Size)})
				seen.volumes[v.Name] = true
			}
		}
		if cs, err := webHostContexts(ctx, h); err != nil {
			problem("contexts", err)
		} else {
			for _, c := range cs {
				sh := webServerHost(c, h.session)
				out.Contexts = append(out.Contexts, webproto.ContextItem{Name: webName(c.Name), Current: c.Current, Session: h.session != "" && c.Name == h.session,
					Description: aiContainersRedact(webName(c.Description)), Where: sh.Where, Local: sh.Local, SSH: sh.SSH})
				seen.contexts[c.Name] = true
				hosts = append(hosts, c)
			}
		}
		if cfg, err := containers.ReadRegistryConfig(registryConfigPath(h.e.Kind)); err != nil && !os.IsNotExist(err) {
			problem("registries", errors.New("cannot read the registry config"))
		} else {
			for _, r := range cfg.Registries {
				out.Registries = append(out.Registries, webproto.RegistryItem{Host: webName(r.Host), SignedIn: r.SignedIn, User: webName(h.users[r.Host])})
				seen.registries[r.Host] = r.SignedIn
			}
		}
		return applyMsg(func(m *Model) tea.Cmd {
			if m.web != nil {
				m.web.more.host = seen
				if hosts != nil {
					srv := m.webServersSeen()
					srv.hosts = map[string]containers.Context{}
					for _, c := range hosts {
						srv.hosts[c.Name] = c
					}
				}
			}
			call.reply(out)
			return nil
		})
	}
}

// webContainersInspect answers one item's inspect, masked.
func (m *Model) webContainersInspect(call *webCall) tea.Cmd {
	a := call.args.(*webproto.InspectArgs)
	if err := aiValidInspectTarget(a.Kind, a.ID); err != nil {
		call.fail(webproto.CodeBadRequest, err.Error())
		return nil
	}
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, webContainersWait)
		defer cancel()
		id := a.ID
		if a.Kind == "container" {
			c, err := webContainersFind(ctx, h, a.ID)
			if err != nil {
				call.fail(webproto.CodeNotFound, err.Error())
				return nil
			}
			id = c.ID
		}
		v, err := h.e.InspectType(ctx, a.Kind, id)
		if err != nil {
			call.fail(webproto.CodeNotFound, aiContainersRedact(err.Error()))
			return nil
		}
		masked, n := containers.MaskInspect(v)
		data, err := json.MarshalIndent(masked, "", "  ")
		if err != nil {
			call.fail(webproto.CodeInternal, err.Error())
			return nil
		}
		text, more := aiServicesRedact(string(data))
		out := webproto.ContainerInspect{Kind: a.Kind, ID: a.ID, Masked: n + more}
		if len(text) > webproto.MaxInspectText {
			text, out.Truncated = text[:webproto.MaxInspectText], true
		}
		out.Text = text
		call.reply(out)
		return nil
	}
}

// webMaskEnvStep hides the values of an ENV step of an image's history
// ("ENV A=1 B=2" → "ENV A=… B=…"; the old "ENV A 1" form keeps A only).
func webMaskEnvStep(s string) string {
	i := strings.Index(s, "ENV ")
	if i < 0 {
		return s
	}
	head, rest := s[:i+4], strings.Fields(s[i+4:])
	if len(rest) == 0 {
		return s
	}
	if !strings.Contains(rest[0], "=") {
		return head + rest[0] + " …"
	}
	var out []string
	for _, f := range rest {
		if k, _, ok := strings.Cut(f, "="); ok && k != "" {
			out = append(out, k+"=…")
		}
	}
	return head + strings.Join(out, " ")
}

func (m *Model) webContainersHistory(call *webCall) tea.Cmd {
	ref := call.args.(*webproto.RefArgs).Ref
	if err := aiValidInspectTarget("image", ref); err != nil {
		call.fail(webproto.CodeBadRequest, err.Error())
		return nil
	}
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, webContainersWait)
		defer cancel()
		rows, err := h.e.History(ctx, ref)
		if err != nil {
			call.fail(webproto.CodeNotFound, aiContainersRedact(err.Error()))
			return nil
		}
		out := webproto.ImageHistory{Ref: ref, Rows: []webproto.HistoryRow{}}
		for i, r := range rows {
			if i == webproto.MaxHistoryRows {
				break
			}
			by := aiContainersRedact(webMaskEnvStep(webName(r.CreatedBy)))
			id := r.ID
			if id != "<missing>" {
				id = webShortImageID(id)
			} else {
				id = ""
			}
			out.Rows = append(out.Rows, webproto.HistoryRow{ID: id, Created: webName(r.CreatedAt), CreatedBy: by, Size: webName(r.Size), Comment: aiContainersRedact(webName(r.Comment))})
		}
		call.reply(out)
		return nil
	}
}

func (m *Model) webContainersStats(call *webCall) tea.Cmd {
	id := call.args.(*webproto.ContainerArgs).ID
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, webContainersWait)
		defer cancel()
		c, err := webContainersFind(ctx, h, id)
		if err != nil {
			call.fail(webproto.CodeNotFound, err.Error())
			return nil
		}
		st, err := h.e.Stats(ctx, c.ID)
		if err != nil || len(st) == 0 {
			msg := "That container is not running."
			if err != nil {
				msg = aiContainersRedact(err.Error())
			}
			call.fail(webproto.CodeConflict, msg)
			return nil
		}
		s := st[0]
		call.reply(webproto.ContainerStats{ID: c.ID, CPU: webName(s.CPUPerc), Mem: webName(s.MemUsage), MemPerc: webName(s.MemPerc), Net: webName(s.NetIO),
			Block: webName(s.BlockIO), PIDs: webName(s.PIDs)})
		return nil
	}
}

func (m *Model) webContainersDiskUsage(call *webCall) tea.Cmd {
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, webContainersWait)
		defer cancel()
		text, err := h.e.DiskUsage(ctx)
		if err != nil {
			call.fail(webproto.CodeInternal, aiContainersRedact(err.Error()))
			return nil
		}
		text = aiContainersRedact(strings.TrimRight(text, "\n"))
		out := webproto.DiskUsage{Lines: []string{}}
		if text != "" {
			for _, l := range strings.Split(text, "\n") {
				out.Lines = append(out.Lines, webName(l))
			}
		}
		call.reply(out)
		return nil
	}
}

func (m *Model) webContainersSearch(call *webCall) tea.Cmd {
	term := strings.ToLower(call.args.(*webproto.SearchTermArgs).Term)
	if err := containers.ValidSearchTerm(term); err != nil {
		call.fail(webproto.CodeBadRequest, err.Error())
		return nil
	}
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	m.webAuditOp(call, term, "ok")
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, webContainersWait)
		defer cancel()
		rs, err := h.e.Search(ctx, term, containers.DefaultSearchLimit)
		if err != nil {
			call.fail(webproto.CodeInternal, aiContainersRedact(err.Error()))
			return nil
		}
		out := webproto.ImageSearch{Term: term, Results: []webproto.SearchItem{}}
		for i, r := range rs {
			if i == webproto.MaxSearchResults {
				break
			}
			out.Results = append(out.Results, webproto.SearchItem{Name: webName(r.Name), Description: webName(r.Description), Stars: r.Stars, Official: r.Official})
		}
		call.reply(out)
		return nil
	}
}

func (m *Model) webContainersTags(call *webCall) tea.Cmd {
	repo, err := containers.ParseRemoteRepo(call.args.(*webproto.RefArgs).Ref)
	if err != nil {
		call.fail(webproto.CodeBadRequest, err.Error())
		return nil
	}
	m.webAuditOp(call, repo.Ref(), "ok")
	api := newRegistryAPI()
	call.later()
	return func() tea.Msg {
		list, err := api.Tags(call.ctx, repo)
		out := webproto.RemoteTags{Repo: repo.Ref(), Tags: []string{}}
		switch {
		case errors.Is(err, containers.ErrNeedsSignIn), errors.Is(err, containers.ErrRepoNotFound):
			out.Note = err.Error()
		case err != nil:
			call.fail(webproto.CodeInternal, aiContainersRedact(err.Error()))
			return nil
		}
		for i, t := range list.Tags {
			if i == webproto.MaxRemoteTags {
				out.Truncated = true
				break
			}
			out.Tags = append(out.Tags, webName(t))
		}
		out.Truncated = out.Truncated || list.Truncated
		call.reply(out)
		return nil
	}
}

// ── At once (the page asked first) ──────────────────────────────────────

// webHostFind is a listed image, network, volume, context or registry.
func (m *Model) webHostSeen(call *webCall) (*webHostSeen, bool) {
	if m.web == nil || m.web.more.host == nil {
		call.fail(webproto.CodeNotFound, errHostNotListed.Error())
		return nil, false
	}
	return m.web.more.host, true
}

// webHostRun runs one engine command off the UI thread; done answers on
// Update.
func webHostRun(parent context.Context, h aiContainersHost, args []string, done func(m *Model, out string, err error) tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, 2*webContainersWait)
		defer cancel()
		x := h.e.X
		if x == nil {
			x = servicesExec
		}
		if env := h.e.ContextEnv(); env != "" {
			x = services.WithEnv(x, env)
		}
		stdout, stderr, code, err := x.Capture(ctx, "", append([]string{h.e.Binary}, args...)...)
		if err == nil && code != 0 {
			err = errors.New(errLine(stderr))
		}
		if err != nil {
			err = errors.New(aiContainersRedact(err.Error()))
		}
		return applyMsg(func(m *Model) tea.Cmd { return done(m, stdout, err) })
	}
}

// webHostReply answers an in-page engine command.
func (m *Model) webHostReply(call *webCall, h aiContainersHost, summary, what string, args ...string) tea.Cmd {
	call.later()
	return webHostRun(call.ctx, h, args, func(m *Model, _ string, err error) tea.Cmd {
		if err != nil {
			m.webAuditOp(call, summary, "failed")
			call.fail(webproto.CodeInternal, what+" failed: "+err.Error())
			return nil
		}
		m.webAuditOp(call, summary, "ok")
		call.reply(webproto.HostDone{})
		return m.containersRefresh("images")
	})
}

func (m *Model) webContainersTag(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ImageTagArgs)
	seen, ok := m.webHostSeen(call)
	if !ok {
		return nil
	}
	im, listed := seen.images[a.Ref]
	if !listed {
		call.fail(webproto.CodeNotFound, errHostNotListed.Error())
		return nil
	}
	if err := containers.ValidImageRef(a.Tag); err != nil {
		call.fail(webproto.CodeBadRequest, err.Error())
		return nil
	}
	if other, taken := seen.images[a.Tag]; taken && other.ID != im.ID {
		call.fail(webproto.CodeConflict, "That tag is on another image. Move it on the laptop.")
		return nil
	}
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	return m.webHostReply(call, h, a.Ref+" → "+a.Tag, "Tag", "tag", a.Ref, a.Tag)
}

func (m *Model) webContainersCreate(call *webCall) tea.Cmd {
	a := call.args.(*webproto.StorageArgs)
	if err := containers.ValidName(a.Name); err != nil {
		call.fail(webproto.CodeBadRequest, err.Error())
		return nil
	}
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	return m.webHostReply(call, h, a.Kind+" "+a.Name, "Create", a.Kind, "create", a.Name)
}

// webComposeTarget is a compose project of the last containers.list:
// its folder (it must exist) and config files.
func (m *Model) webComposeTarget(call *webCall, project string) (dir string, files []string, ok bool) {
	found := false
	if m.web != nil {
		for _, c := range m.web.more.ctrRaw {
			if c.Project != project {
				continue
			}
			found = true
			if dir == "" && c.WorkingDir != "" {
				dir = c.WorkingDir
			}
			if len(files) == 0 && len(c.ConfigFiles) > 0 {
				files = append([]string(nil), c.ConfigFiles...)
			}
		}
	}
	if !found {
		call.fail(webproto.CodeNotFound, "That project is not in the list. Open the list again.")
		return "", nil, false
	}
	if st := m.services; dir == "" && st != nil && project == st.project {
		dir = st.root
	}
	if !projectDirExists(dir) {
		call.fail(webproto.CodeConflict, "The project folder is not on the laptop. Use the laptop for this project.")
		return "", nil, false
	}
	return dir, files, true
}

// webComposeRun runs compose verb of project in a laptop terminal.
func (m *Model) webComposeRun(h aiContainersHost, project, dir string, files []string, verb string) error {
	args := []string{verb}
	if verb == "up" {
		args = append(args, "-d")
	}
	cmd := h.e.ComposeWithFiles(project, dir, files, args...)
	cmd.Label = "compose " + verb + ": " + project
	if err := webCtrTerminal(m, cmd, containerTabPrefix+"compose:"+project); err != nil {
		return err
	}
	if st := m.services; st != nil && m.web != nil {
		state := map[string]services.State{"up": services.Starting, "stop": services.Stopping, "down": services.Stopping}[verb]
		if state == 0 {
			state = services.Restarting
		}
		for id, c := range m.web.more.ctrRaw {
			if c.Project == project {
				st.busy[containerBusyPrefix+id] = state
			}
		}
		st.touch()
	}
	return nil
}

func (m *Model) webContainersCompose(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ComposeArgs)
	dir, files, ok := m.webComposeTarget(call, a.Project)
	if !ok {
		return nil
	}
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	if err := m.webComposeRun(h, a.Project, dir, files, a.Verb); err != nil {
		m.webAuditOp(call, a.Project+" "+a.Verb, "failed")
		call.fail(webproto.CodeInternal, err.Error())
		return nil
	}
	m.webAuditOp(call, a.Project+" "+a.Verb, "ok")
	call.reply(webproto.HostDone{Note: "Running in a laptop terminal."})
	return nil
}

// webContainersContext uses a context for this session only (the
// machine's default is unchanged), as Enter on a context row does.
func (m *Model) webContainersContext(call *webCall) tea.Cmd {
	name := call.args.(*webproto.ContextArgs).Name
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, webContainersWait)
		defer cancel()
		list, err := webHostContexts(ctx, h)
		if err != nil {
			call.fail(webproto.CodeInternal, aiContainersRedact(err.Error()))
			return nil
		}
		known := false
		for _, c := range list {
			known = known || c.Name == name
		}
		return applyMsg(func(m *Model) tea.Cmd {
			if !known {
				call.fail(webproto.CodeNotFound, "That context is not on this laptop.")
				return nil
			}
			st := m.webServicesState()
			if st.sessionContext == name {
				call.reply(webproto.HostDone{Note: "Already using " + name + "."})
				return nil
			}
			st.cancelHostWork()
			st.stopHostWatch()
			st.sessionContext = name
			st.resetHostSnapshot(true)
			m.webAuditOp(call, name, "ok")
			call.reply(webproto.HostDone{Note: "This session only; the default is unchanged."})
			if st.visible {
				return tea.Batch(m.containersDetectWithWatch(true), m.servicesRefresh())
			}
			return nil
		})
	}
}

func (m *Model) webServicesStartAll(call *webCall) tea.Cmd {
	m.webServicesState()
	m.webAuditOp(call, "all", "ok")
	call.reply(webproto.HostDone{})
	return webSvcAll(m, "start-all")
}

// webServicesConfigure writes .termocode/services.json (the commented
// template) when missing and names it; the page opens it.
func (m *Model) webServicesConfigure(call *webCall) tea.Cmd {
	root := m.taskVars().WorkspaceFolder
	if st := m.services; st != nil && st.root != "" {
		root = st.root
	}
	path := filepath.Join(root, services.ConfigPath)
	created := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		werr := os.MkdirAll(filepath.Dir(path), 0o755)
		if werr == nil {
			werr = os.WriteFile(path, []byte(services.Template), 0o644)
		}
		if werr != nil {
			call.fail(webproto.CodeInternal, "Cannot create services.json: "+werr.Error())
			return nil
		}
		created = true
	}
	m.webAuditOp(call, services.ConfigPath, map[bool]string{true: "created", false: "ok"}[created])
	call.reply(webproto.ServicesConfig{Path: filepath.ToSlash(services.ConfigPath), Created: created})
	return nil
}

// ── Laptop cards ────────────────────────────────────────────────────────

// webCtrAsk builds a laptop card whose yes runs run; body ends with the
// exact command when argv is given.
func webCtrAsk(summary, title, action, body string, argv []string, run func(m *Model) tea.Cmd) *webAsk {
	if len(argv) > 0 {
		body += "\n\nCommand: " + svcShellLine(argv)
	}
	return &webAsk{summary: summary, title: title, action: action, body: body, run: run}
}

// webCtrToast is the laptop's toast after a card's command.
func (m *Model) webCtrToast(fail, ok string, err error) tea.Cmd {
	var t tea.Cmd
	if err != nil {
		m.toast, t = m.toast.PushDetail(toast.Errr, fail, err.Error())
	} else {
		m.toast, t = m.toast.PushDetail(toast.Info, ok, "Asked from the web.")
	}
	return t
}

// webCtrTermRun opens cmd in a laptop terminal; a failure toasts.
func (m *Model) webCtrTermRun(cmd services.Command, key, fail string) tea.Cmd {
	if err := webCtrTerminal(m, cmd, key); err != nil {
		return m.webCtrToast(fail, "", err)
	}
	return nil
}

func (m *Model) webContainersExec(call *webCall) *webAsk {
	a := call.args.(*webproto.ExecArgs)
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	it, listed := m.web.more.ctrs[a.ID]
	if !listed {
		call.fail(webproto.CodeNotFound, "That container is not in the list. Open the list again.")
		return nil
	}
	if it.State != "running" {
		call.fail(webproto.CodeConflict, "That container is not running.")
		return nil
	}
	cmd := h.e.Shell(a.ID)
	title, body := "Open a shell?", "Open a shell in "+it.Name+" ("+it.Image+") in a laptop terminal? Commands typed there run inside the container."
	if a.Kind == "attach" {
		cmd = h.e.Attach(a.ID)
		title, body = "Attach to container?", "Attach a laptop terminal to "+it.Name+" ("+it.Image+")? What you type goes to its main process; Ctrl+P Ctrl+Q detaches."
	}
	cmd.Label = a.Kind + ": " + it.Name
	return webCtrAsk(aiShortID(a.ID)+" "+it.Name+" "+a.Kind, title, "Open", body, cmd.Argv, func(m *Model) tea.Cmd {
		return m.webCtrTermRun(cmd, containerTabPrefix+a.Kind+":"+a.ID, "Cannot open "+a.Kind)
	})
}

func (m *Model) webContainersImage(call *webCall) *webAsk {
	a := call.args.(*webproto.ImageActArgs)
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	if a.Verb == "pull" {
		if err := containers.ValidImageRef(a.Ref); err != nil {
			call.fail(webproto.CodeBadRequest, err.Error())
			return nil
		}
		cmd := h.e.PullCommand(a.Ref)
		ask := webCtrAsk("pull "+a.Ref, "Pull image?", "Pull", "Download "+a.Ref+" from its registry in a laptop terminal?", cmd.Argv, func(m *Model) tea.Cmd {
			return m.webCtrTermRun(cmd, containerTabPrefix+"pull:"+a.Ref, "Cannot pull image")
		})
		ask.noAsk = true // the TUI pulls without a question
		return ask
	}
	seen, ok := m.webHostSeen(call)
	if !ok {
		return nil
	}
	if _, listed := seen.images[a.Ref]; !listed {
		call.fail(webproto.CodeNotFound, errHostNotListed.Error())
		return nil
	}
	switch a.Verb {
	case "push":
		if !strings.Contains(a.Ref, ":") || isHex(a.Ref) {
			call.fail(webproto.CodeConflict, "Push needs a tagged image (repo:tag).")
			return nil
		}
		cmd := h.e.PushCommand(a.Ref)
		return webCtrAsk("push "+a.Ref, "Push image?", "Push", "Send "+a.Ref+" to its registry? It leaves this machine.", cmd.Argv, func(m *Model) tea.Cmd {
			return m.webCtrTermRun(cmd, containerTabPrefix+"push:"+a.Ref, "Cannot push image")
		})
	case "run":
		cmd := h.e.RunCommand(a.Ref, a.Name, a.Ports, nil)
		cmd.Label = "run: " + a.Ref
		return webCtrAsk("run "+a.Ref, "Run image?", "Run", "Start a new container from "+a.Ref+" in a laptop terminal? No env values come from the web.", cmd.Argv, func(m *Model) tea.Cmd {
			return m.webCtrTermRun(cmd, containerTabPrefix+"run:"+a.Ref, "Cannot run image")
		})
	}
	if seen.inUse[a.Ref] {
		call.fail(webproto.CodeConflict, "A container uses this image. Remove the container first.")
		return nil
	}
	argv := []string{"rmi", a.Ref}
	return webCtrAsk("rmi "+a.Ref, "Remove image?", "Remove", "Remove image reference "+a.Ref+"? You can pull it again.", append([]string{h.e.Binary}, argv...), func(m *Model) tea.Cmd {
		return webHostRun(context.Background(), h, argv, func(m *Model, _ string, err error) tea.Cmd {
			return tea.Batch(m.webCtrToast("Could not remove image", "Removed "+a.Ref, err), m.containersRefresh("images"))
		})
	})
}

// webPruneText is the card of a prune kind.
var webPruneText = map[string][2]string{
	pruneContainers: {"Remove stopped containers?", "Every stopped container on this host goes. Running and paused containers stay."},
	pruneImages:     {"Remove untagged images?", "Only unused untagged images go. Tagged images stay."},
	pruneImagesAll:  {"Remove all unused images?", "Every image no container uses goes, tagged ones too. You can pull a removed image again."},
	pruneNetworks:   {"Remove unused networks?", "The engine decides which are unused. Built-in networks stay."},
	pruneVolumes:    {"Remove unused volumes?", "Unnamed volumes no container uses go. Their data is deleted."},
	pruneSystem:     {"Clean up unused items?", "Stopped containers, untagged images, unused networks and old build files go. Volumes and tagged images stay."},
}

func (m *Model) webContainersPrune(call *webCall) *webAsk {
	kind := call.args.(*webproto.PruneArgs).Kind
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	argv := pruneArgv(h.e.Binary, kind)
	txt := webPruneText[kind]
	where := h.bin() // no engine call on the UI thread: the session's context only
	if h.session != "" {
		where += " · context " + h.session
	}
	return webCtrAsk(kind, txt[0], "Remove", "On this host ("+where+"): "+txt[1], argv, func(m *Model) tea.Cmd {
		return webHostRun(context.Background(), h, argv[1:], func(m *Model, out string, err error) tea.Cmd {
			done := "Pruned " + kind
			if l := errLine(lastLine(out)); l != "" {
				done += ": " + sanitizeOutputLine(l)
			}
			return tea.Batch(m.webCtrToast("Could not prune "+kind, done, err), m.containersRefresh(pruneSection(kind)))
		})
	})
}

// lastLine is the last non-empty line of s.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

func (m *Model) webContainersStorageRemove(call *webCall) *webAsk {
	a := call.args.(*webproto.StorageArgs)
	seen, ok := m.webHostSeen(call)
	if !ok {
		return nil
	}
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	var title, body string
	if a.Kind == "network" {
		builtIn, listed := seen.networks[a.Name]
		if !listed {
			call.fail(webproto.CodeNotFound, errHostNotListed.Error())
			return nil
		}
		if builtIn {
			call.fail(webproto.CodeConflict, a.Name+" is a built-in network; it cannot be removed.")
			return nil
		}
		title, body = "Remove network?", "Remove network "+a.Name+"? Containers still on it keep running but lose it."
	} else {
		if !seen.volumes[a.Name] {
			call.fail(webproto.CodeNotFound, errHostNotListed.Error())
			return nil
		}
		title, body = "Remove volume?", "Permanently remove volume "+a.Name+" and its data? This cannot be undone."
	}
	argv := []string{a.Kind, "rm", a.Name}
	return webCtrAsk(a.Kind+" "+a.Name, title, "Remove", body, append([]string{h.e.Binary}, argv...), func(m *Model) tea.Cmd {
		return webHostRun(context.Background(), h, argv, func(m *Model, _ string, err error) tea.Cmd {
			return tea.Batch(m.webCtrToast("Could not remove "+a.Kind, "Removed "+a.Kind+" "+a.Name, err), m.containersRefresh(a.Kind+"s"))
		})
	})
}

func (m *Model) webContainersComposeAsk(call *webCall) *webAsk {
	a := call.args.(*webproto.ComposeAskArgs)
	dir, files, ok := m.webComposeTarget(call, a.Project)
	if !ok {
		return nil
	}
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	cmd := h.e.ComposeWithFiles(a.Project, dir, files, a.Verb)
	title, body := "Stop project?", "Stop every container of "+a.Project+"? What runs in them ends."
	if a.Verb == "down" {
		title, body = "Remove project containers?", "Stop and remove the containers and networks of "+a.Project+"? Volumes stay."
	}
	return webCtrAsk(a.Project+" "+a.Verb, title, map[string]string{"stop": "Stop", "down": "Remove"}[a.Verb], body, cmd.Argv, func(m *Model) tea.Cmd {
		if err := m.webComposeRun(h, a.Project, dir, files, a.Verb); err != nil {
			return m.webCtrToast("Cannot "+a.Verb+" project", "", err)
		}
		return nil
	})
}

func (m *Model) webContainersContextDefault(call *webCall) *webAsk {
	name := call.args.(*webproto.ContextArgs).Name
	seen, ok := m.webHostSeen(call)
	if !ok {
		return nil
	}
	if !seen.contexts[name] {
		call.fail(webproto.CodeNotFound, "That context is not in the list. Open the list again.")
		return nil
	}
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	argv := append([]string{h.e.Binary}, h.e.UseContextArgs(name)...)
	return webCtrAsk(name, "Make "+name+" the default?", "Make default",
		"This changes the default for every terminal and app on this machine, not only termocode.", argv, func(m *Model) tea.Cmd {
			e := h.e
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), webContainersWait)
				defer cancel()
				err := e.UseContext(ctx, name)
				return applyMsg(func(m *Model) tea.Cmd {
					return tea.Batch(m.webCtrToast("Could not change the default", "Default is now "+name, err), m.containersRefreshContexts())
				})
			}
		})
}

func (m *Model) webContainersRegistry(call *webCall) *webAsk {
	a := call.args.(*webproto.RegistryArgs)
	h, ok := m.webContainersHost(call)
	if !ok {
		return nil
	}
	if a.Verb == "connect" {
		if a.Host != "" {
			if err := containers.ValidRegistryHost(a.Host); err != nil {
				call.fail(webproto.CodeBadRequest, err.Error())
				return nil
			}
		}
		where := "a registry"
		if a.Host != "" {
			where = a.Host
		}
		return webCtrAsk("connect "+a.Host, "Sign in to "+where+"?", "Sign in",
			"The laptop asks for the user and password next. They are typed on the laptop and never sent to the web.", nil, func(m *Model) tea.Cmd {
				m.webServicesState()
				m.startRegistryConnect(a.Host)
				return nil
			})
	}
	seen, ok := m.webHostSeen(call)
	if !ok {
		return nil
	}
	if signed, listed := seen.registries[a.Host]; !listed || !signed {
		call.fail(webproto.CodeNotFound, "That registry has no sign-in here.")
		return nil
	}
	argv := []string{"logout", a.Host}
	return webCtrAsk("signout "+a.Host, "Sign out of "+a.Host+"?", "Sign out",
		"The engine forgets the saved login for this host. Local images stay; private pulls and pushes need a new sign-in.", append([]string{h.e.Binary}, argv...), func(m *Model) tea.Cmd {
			return webHostRun(context.Background(), h, argv, func(m *Model, _ string, err error) tea.Cmd {
				if err == nil && m.services != nil {
					delete(m.services.registryUsers, a.Host)
				}
				return tea.Batch(m.webCtrToast("Could not sign out", "Signed out of "+a.Host, err), m.containersRefresh("registries"))
			})
		})
}

func (m *Model) webContainersOpenDB(call *webCall) *webAsk {
	id := call.args.(*webproto.ContainerArgs).ID
	c, listed := m.web.more.ctrRaw[id]
	if !listed {
		call.fail(webproto.CodeNotFound, "That container is not in the list. Open the list again.")
		return nil
	}
	t, ok := dbTargetFromContainer(m.webServicesState(), c)
	if !ok {
		call.fail(webproto.CodeConflict, "Not a database container (PostgreSQL, MySQL, MariaDB, Redis or Valkey).")
		return nil
	}
	return webCtrAsk(aiShortID(id)+" "+t.name, "Add "+t.name+" to Databases?", "Add",
		fmt.Sprintf("Add %s (localhost:%d) to the Databases view? The laptop asks for its password; it is never sent to the web.", t.name, t.port), nil, func(m *Model) tea.Cmd {
			return m.databaseOpenTarget(t)
		})
}

func (m *Model) webServicesStopAll(call *webCall) *webAsk {
	st := m.webServicesState()
	var names []string
	for _, s := range st.compose {
		names = append(names, s.Name)
	}
	for _, s := range st.local {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	body := "Stop every service of this project? What runs in them ends."
	if len(names) > 0 {
		body += "\n\nServices: " + strings.Join(names, ", ")
	}
	return webCtrAsk("all", "Stop all services?", "Stop", body, nil, func(m *Model) tea.Cmd { return webSvcAll(m, "stop-all") })
}
