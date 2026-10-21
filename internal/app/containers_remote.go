package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Remote tags and Docker Hub search (CP-12) ────────────────────────────
//
// `b` on an image repository (IMAGES) lists its remote tags right under
// it; `b` in REGISTRIES asks for a repository and lists its tags there.
// The tags come from the Registry HTTP API v2 with anonymous tokens only
// (internal/containers/registryapi.go): no credential store, helper or
// config.json auth value is read. A private repository shows "needs
// sign-in; pull by name still works". `s` in REGISTRIES searches Docker
// Hub through the engine's own `search` (no HTTP of ours). A listed tag or
// a search result pulls through the existing pull prompt and tab.
//
// Nothing here is offered to the AI (palette ids are aiNever): it reaches
// the network on the user's behalf and needs the user's typed input.

const (
	promptKindContainerSearch promptKindEnum = 1720 + iota
	promptKindContainerBrowse
)

const pickerKindContainerSearch pickerKindEnum = 1701

// remoteTags is one repository's remote tag list (session only).
type remoteTags struct {
	repo      containers.RemoteRepo
	ref       string // repo.Ref(): the map key and the pull name
	inImages  bool   // opened from IMAGES: shown under its repo group
	open      bool
	loading   bool
	loaded    bool
	tags      []string
	truncated bool
	err       string // a short note ("" = fine)
}

type remoteTagsMsg struct {
	gen  uint64
	ref  string
	list containers.TagList
	err  error
}

type containerSearchMsg struct {
	term    string
	results []containers.SearchResult
	err     error
}

// newRegistryAPI builds the HTTP lister; tests point it at loopback
// servers.
var newRegistryAPI = func() *containers.RegistryAPI { return &containers.RegistryAPI{} }

// remoteKey is the remote list key of a local repository name
// ("postgres", "ghcr.io/acme/api"); "" when it is not a plain repository.
func remoteKey(repo string) string {
	r, err := containers.ParseRemoteRepo(repo)
	if err != nil {
		return ""
	}
	return r.Ref()
}

func remoteRowID(ref string) string      { return "remote:" + ref }
func remoteTagID(ref, tag string) string { return "remote:" + ref + ":" + tag }

// remoteRows are the rows of one open remote list at depth (its header
// row, then the tags or a note).
func (st *servicesState) remoteRows(section, ref, label string, depth int) []svcRow {
	rt := st.remoteTags[ref]
	if rt == nil {
		return nil
	}
	head := svcRow{kind: svcRowHostItem, section: section, host: hostRemoteRepo, hostID: remoteRowID(ref), name: label, imageRef: ref, depth: depth}
	switch {
	case rt.loading:
		head.count = "…"
	case rt.loaded && rt.err == "":
		head.count = strconv.Itoa(len(rt.tags))
		if rt.truncated {
			head.count += "+"
		}
	}
	out := []svcRow{head}
	if !rt.open {
		return out
	}
	hint := func(t string) svcRow {
		return svcRow{kind: svcRowHint, section: section, text: strings.Repeat("  ", depth+1) + t}
	}
	switch {
	case rt.loading:
		return append(out, hint("Loading tags…"))
	case rt.err != "":
		return append(out, hint(rt.err))
	case !rt.loaded:
		return out
	case len(rt.tags) == 0:
		return append(out, hint("no tags"))
	}
	for _, tag := range rt.tags {
		out = append(out, svcRow{kind: svcRowHostItem, section: section, host: hostRemoteTag, hostID: remoteTagID(ref, tag),
			name: tag, tag: tag, imageRef: ref + ":" + tag, depth: depth + 1})
	}
	if rt.truncated {
		out = append(out, hint("first "+strconv.Itoa(len(rt.tags))+" tags · pull by name for others"))
	}
	return out
}

// imageRemoteRows are the remote tags under an IMAGES repo group.
func (st *servicesState) imageRemoteRows(key, repo string) []svcRow {
	if len(st.remoteTags) == 0 || repo == danglingRepo {
		return nil
	}
	ref := remoteKey(repo)
	if rt := st.remoteTags[ref]; ref == "" || rt == nil || !rt.inImages {
		return nil
	}
	return st.remoteRows(key, ref, "remote tags", 2)
}

// registryRemoteRows are the repositories browsed in REGISTRIES.
func (st *servicesState) registryRemoteRows(key string) []svcRow {
	var out []svcRow
	for _, ref := range st.remoteOrder {
		out = append(out, st.remoteRows(key, ref, sanitizeOutputLine(ref), 0)...)
	}
	return out
}

// renderRemoteRow draws a remote repo header or a remote tag.
func (st *servicesState) renderRemoteRow(r svcRow, active, focused bool, w int) string {
	if r.host == hostRemoteTag {
		return renderHostLine(active, focused, w, r.depth, "", 0, r.name, theme.TextPrimary, nil)
	}
	fold := theme.GlyphTreeCollapsed
	if rt := st.remoteTags[r.imageRef]; rt != nil && rt.open {
		fold = theme.GlyphExpand
	}
	var cols []hostCol
	if w >= 22 && r.count != "" {
		cols = append(cols, hostCol{text: r.count, width: 5, right: true, col: theme.TextMuted})
	}
	return renderHostLine(active, focused, w, r.depth, fold.String(), theme.TextDim, r.name, theme.TextSecondary, cols)
}

func (st *servicesState) remoteFooter(r svcRow) (string, string) {
	if r.host == hostRemoteTag {
		return sanitizeOutputLine(r.imageRef), "remote tag · u pull · y copy"
	}
	rt := st.remoteTags[r.imageRef]
	if rt == nil {
		return "", ""
	}
	l1 := sanitizeOutputLine(rt.repo.Host + "/" + rt.repo.Name)
	switch {
	case rt.loading:
		return l1, "asking the registry…"
	case rt.err != "":
		return l1, rt.err
	}
	return l1, "remote tags · space fold · r reload · u pull"
}

// ── Open, load ───────────────────────────────────────────────────────────

// browseRemote opens (or reloads) the remote tags of ref. inImages shows
// them under the IMAGES group, else in REGISTRIES.
func (m *Model) browseRemote(ref string, inImages bool) tea.Cmd {
	st := m.services
	repo, err := containers.ParseRemoteRepo(strings.TrimSpace(ref))
	if err != nil {
		return m.servicesToast(toast.Info, "Not an image repository", sanitizeOutputLine(err.Error()))
	}
	if st.remoteTags == nil {
		st.remoteTags = map[string]*remoteTags{}
	}
	key := repo.Ref()
	rt := st.remoteTags[key]
	if rt == nil {
		rt = &remoteTags{repo: repo, ref: key}
		st.remoteTags[key] = rt
	}
	if inImages {
		rt.inImages = true
	} else if !containsString(st.remoteOrder, key) {
		st.remoteOrder = append(st.remoteOrder, key)
	}
	rt.open = true
	st.touch()
	if !inImages {
		st.collapsed[hostSection("registries")] = false
		m.servicesCursorTo(func(r svcRow) bool {
			return r.host == hostRemoteRepo && r.imageRef == key && r.hostSectionID() == "registries"
		})
	}
	if rt.loading || rt.loaded {
		return nil
	}
	return m.loadRemoteTags(key)
}

// servicesCursorTo moves the cursor to the first row that matches.
func (m *Model) servicesCursorTo(match func(svcRow) bool) bool {
	st := m.services
	for i, r := range st.rows() {
		if match(r) {
			st.cursor = i
			st.touch()
			return true
		}
	}
	return false
}

// loadRemoteTags asks the registry; the call is cancelled with the rest
// of the view's work when the view hides.
func (m *Model) loadRemoteTags(ref string) tea.Cmd {
	st := m.services
	rt := st.remoteTags[ref]
	if rt == nil || !st.visible {
		return nil
	}
	rt.loading, rt.loaded, rt.err, rt.tags, rt.truncated = true, false, "", nil, false
	st.touch()
	ctx, cancel := context.WithCancel(context.Background())
	key := "remote/" + ref
	if old := st.containerCancels[key]; old != nil {
		old()
	}
	st.containerCancels[key] = cancel
	gen, repo, api := st.containerGen, rt.repo, newRegistryAPI()
	return func() tea.Msg {
		defer cancel()
		list, err := api.Tags(ctx, repo)
		return remoteTagsMsg{gen: gen, ref: ref, list: list, err: err}
	}
}

func (m *Model) onRemoteTags(msg remoteTagsMsg) tea.Cmd {
	st := m.services
	if st == nil {
		return nil
	}
	rt := st.remoteTags[msg.ref]
	if rt == nil || !rt.loading {
		return nil
	}
	if cancel := st.containerCancels["remote/"+msg.ref]; cancel != nil && msg.gen == st.containerGen {
		cancel()
		delete(st.containerCancels, "remote/"+msg.ref)
	}
	rt.loading = false
	if msg.gen != st.containerGen {
		// The view hid (or the context changed) while it ran: ask again
		// on the next open.
		st.touch()
		return nil
	}
	rt.loaded = true
	switch {
	case msg.err == nil:
		rt.tags, rt.truncated = msg.list.Tags, msg.list.Truncated
	case errors.Is(msg.err, containers.ErrNeedsSignIn):
		rt.err = containers.ErrNeedsSignIn.Error()
	case errors.Is(msg.err, containers.ErrRepoNotFound):
		rt.err = "not found on " + rt.repo.Host
	default:
		rt.err = sanitizeOutputLine(errLine(msg.err.Error()))
	}
	st.touch()
	return nil
}

// stopRemoteLoading is the view hiding: a list that was loading is asked
// for again on the next open.
func (st *servicesState) stopRemoteLoading() {
	for _, rt := range st.remoteTags {
		if rt.loading {
			rt.loading, rt.loaded = false, false
		}
	}
}

// toggleRemote folds a remote list (the first open loads it).
func (m *Model) toggleRemote(r svcRow) tea.Cmd {
	st := m.services
	rt := st.remoteTags[r.imageRef]
	if rt == nil {
		return nil
	}
	rt.open = !rt.open
	st.touch()
	if rt.open && !rt.loading && !rt.loaded {
		return m.loadRemoteTags(rt.ref)
	}
	return nil
}

// forgetRemote drops a browsed repository from REGISTRIES.
func (m *Model) forgetRemote(ref string) {
	st := m.services
	for i, k := range st.remoteOrder {
		if k == ref {
			st.remoteOrder = append(st.remoteOrder[:i:i], st.remoteOrder[i+1:]...)
			break
		}
	}
	if rt := st.remoteTags[ref]; rt != nil && !rt.inImages {
		delete(st.remoteTags, ref)
	}
	st.touch()
	m.servicesClampCursor()
}

// imageBrowseTarget is the repository of an IMAGES group or tag row.
func (m *Model) imageBrowseTarget(r svcRow) (string, bool) {
	switch {
	case r.kind == svcRowGroup && r.hostSectionID() == "images":
		_, repo, _ := strings.Cut(strings.TrimPrefix(r.section, "grp/"), "/")
		if repo == danglingRepo {
			return "", false
		}
		return repo, true
	case r.kind == svcRowHostItem && r.host == hostImage:
		repo, _ := splitImageTag(r.imageRef)
		if repo == "" || repo == danglingRepo || strings.Contains(r.imageRef, "<none>") {
			return "", false
		}
		return repo, true
	}
	return "", false
}

// askBrowse asks for a repository to list tags of (REGISTRIES).
func (m *Model) askBrowse(prefill string) {
	m.prompt = prompt.New("Browse remote tags", "Repository (like nginx or ghcr.io/acme/api):", prefill)
	m.prompt.SetNote("Public repositories only: termocode asks the registry without your login. Pull by name works for private ones.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindContainerBrowse
}

// browseFromPalette: the selected image's repository, else a prompt.
func (m *Model) browseFromPalette() tea.Cmd {
	if r, ok := m.servicesCurrentRow(); ok {
		if repo, ok := m.imageBrowseTarget(r); ok {
			return m.browseRemote(repo, true)
		}
	}
	m.askBrowse("")
	return nil
}

// ── Search ───────────────────────────────────────────────────────────────

func (m *Model) askSearch() {
	m.prompt = prompt.New("Search Docker Hub", "Search for:", "")
	m.prompt.SetNote("Runs " + m.services.registryEngineName() + " search. Pick a result to pull it or see its tags.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindContainerSearch
}

func (m *Model) runSearch(term string) tea.Cmd {
	st := m.services
	term = strings.ToLower(strings.TrimSpace(term))
	if err := containers.ValidSearchTerm(term); err != nil {
		return m.servicesToast(toast.Info, "Cannot search", err.Error())
	}
	e, ok := st.registryEngine()
	if !ok {
		return m.servicesToast(toast.Errr, "Cannot search", "No container engine (Docker or Podman) on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if old := st.containerCancels["search"]; old != nil {
		old()
	}
	st.containerCancels["search"] = cancel
	return tea.Batch(m.servicesToast(toast.Info, "Searching Docker Hub for "+term+"…", ""), func() tea.Msg {
		defer cancel()
		rs, err := e.Search(ctx, term, containers.DefaultSearchLimit)
		return containerSearchMsg{term: term, results: rs, err: err}
	})
}

func (m *Model) onContainerSearch(msg containerSearchMsg) tea.Cmd {
	st := m.services
	if st == nil {
		return nil
	}
	delete(st.containerCancels, "search")
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Search failed", sanitizeOutputLine(errLine(msg.err.Error())))
	}
	if len(msg.results) == 0 {
		return m.servicesToast(toast.Info, "No results for "+msg.term, "")
	}
	st.searchResults = msg.results
	items := make([]picker.Item, 0, len(msg.results))
	for i, r := range msg.results {
		hint := "★ " + strconv.Itoa(r.Stars)
		if theme.CurrentIconMode == theme.IconModeASCII {
			hint = "* " + strconv.Itoa(r.Stars)
		}
		if r.Official {
			hint += " · official"
		}
		if r.Description != "" {
			hint += " · " + sanitizeOutputLine(r.Description)
		}
		items = append(items, picker.Item{ID: strconv.Itoa(i), Title: sanitizeOutputLine(r.Name), Hint: hint})
	}
	m.picker = picker.NewItems(" Docker Hub: "+sanitizeOutputLine(msg.term)+" ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen, m.pickerKind = true, pickerKindContainerSearch
	return nil
}

// onSearchPicked asks what to do with a result: pull it or see its tags.
func (m *Model) onSearchPicked(id string) tea.Cmd {
	st := m.services
	i, err := strconv.Atoi(id)
	if err != nil || i < 0 || i >= len(st.searchResults) {
		return nil
	}
	r := st.searchResults[i]
	st.searchPick = r
	body := sanitizeOutputLine(r.Description)
	if body == "" {
		body = "No description."
	}
	body += "\n\n" + strconv.Itoa(r.Stars) + " stars"
	if r.Official {
		body += " · official image"
	}
	m.confirm = confirm.New(sanitizeOutputLine(r.Name), body, []confirm.Button{
		{ID: "cancel", Title: "Cancel"}, {ID: "tags", Title: "Browse tags"}, {ID: "pull", Title: "Pull…", Style: confirm.StylePrimary},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerSearchResult
	return nil
}

func (m *Model) answerSearchResult(id string) tea.Cmd {
	st := m.services
	r := st.searchPick
	st.searchPick = containers.SearchResult{}
	if r.Name == "" {
		return nil
	}
	switch id {
	case "pull":
		m.askImagePull(r.Name)
	case "tags":
		return m.browseRemote(r.Name, false)
	}
	return nil
}

// onRemotePrompt is Enter on the search or browse prompt.
func (m *Model) onRemotePrompt(kind promptKindEnum, value string) tea.Cmd {
	switch kind {
	case promptKindContainerSearch:
		return m.runSearch(value)
	case promptKindContainerBrowse:
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return m.browseRemote(value, false)
	}
	return nil
}

// ── Keys, menu ───────────────────────────────────────────────────────────

// remoteKey handles a key on a remote row, `b` on an image row and `s` /
// `b` in REGISTRIES (ok=false: not ours).
func (m *Model) remoteRowKey(key string, r svcRow) (tea.Cmd, bool) {
	if r.kind == svcRowHostItem && (r.host == hostRemoteRepo || r.host == hostRemoteTag) {
		switch key {
		case "enter", "l", " ":
			if r.host == hostRemoteRepo {
				return m.toggleRemote(r), true
			}
			m.askImagePull(r.imageRef)
			return nil, true
		case "u":
			m.askImagePull(r.imageRef)
			return nil, true
		case "y":
			m.copyToClipboard(r.imageRef)
			return m.servicesToast(toast.Info, "Copied", sanitizeOutputLine(r.imageRef)), true
		case "r", "b":
			if r.host == hostRemoteRepo {
				if rt := m.services.remoteTags[r.imageRef]; rt != nil {
					rt.open = true
					return m.loadRemoteTags(r.imageRef), true
				}
			}
		case "x":
			if r.host == hostRemoteRepo && r.hostSectionID() == "registries" {
				m.forgetRemote(r.imageRef)
				return nil, true
			}
		}
		return nil, false
	}
	switch key {
	case "b":
		if repo, ok := m.imageBrowseTarget(r); ok {
			if rt := m.services.remoteTags[remoteKey(repo)]; rt != nil && rt.inImages && rt.open {
				rt.open = false
				m.services.touch()
				return nil, true
			}
			return m.browseRemote(repo, true), true
		}
		if r.hostSectionID() == "registries" {
			prefill := ""
			if r.host == hostRegistry {
				prefill = registryPullPrefix(r.name)
			}
			m.askBrowse(prefill)
			return nil, true
		}
	case "s":
		if r.hostSectionID() == "registries" {
			m.askSearch()
			return nil, true
		}
	}
	return nil, false
}

func (m *Model) openRemoteMenu(r svcRow, x, y int) {
	m.services.menuRow = r
	items := []menu.Item{{ID: "pull", Title: "Pull…", Hint: "u"}, {ID: "copy", Title: "Copy reference", Hint: "y"}}
	if r.host == hostRemoteRepo {
		items = append([]menu.Item{{ID: "reload", Title: "Reload tags", Hint: "r"}}, items...)
		if r.hostSectionID() == "registries" {
			items = append(items, menu.Item{Sep: true}, menu.Item{ID: "forget", Title: "Remove from list", Hint: "x"})
		}
	}
	m.menu = menu.New(items, x, y)
	m.menu.SetScreenSize(m.w, m.h)
	m.menuOpen, m.menuKind = true, menuKindServices
}

func (m *Model) handleRemoteMenuSelect(id string, r svcRow) tea.Cmd {
	switch id {
	case "pull":
		m.askImagePull(r.imageRef)
	case "copy":
		m.copyToClipboard(r.imageRef)
		return m.servicesToast(toast.Info, "Copied", sanitizeOutputLine(r.imageRef))
	case "reload":
		if rt := m.services.remoteTags[r.imageRef]; rt != nil {
			rt.open = true
			return m.loadRemoteTags(r.imageRef)
		}
	case "forget":
		m.forgetRemote(r.imageRef)
	}
	return nil
}

func isRemoteHostKind(k hostKind) bool { return k == hostRemoteRepo || k == hostRemoteTag }
