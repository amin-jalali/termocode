package app

import (
	"fmt"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/lazyre"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Containers view: the host sections' rows and their look ──────────────
//
// Rows are typed (svcRowGroup / svcRowHostItem with a hostKind and an
// index into the section's data); nothing is parsed back from display
// text. Groups sit at the header's column, their children indent by 2;
// rows without a glyph indent so their names line up (plan §2.1, §2.4).

type hostKind int

const (
	hostNone hostKind = iota
	hostContainer
	hostImage
	hostNetwork
	hostVolume
	hostContext
	hostRegistry
	hostRegistryImage   // a local image under an open registry host
	hostRegistryConnect // the "Connect registry…" row
	hostHelpGuide       // HELP: Open guide
	hostHelpKeys        // HELP: Keys
	hostHelpReport      // HELP: Report a problem
	hostRemoteRepo      // CP-12: a repository's remote tags (foldable)
	hostRemoteTag       // CP-12: one remote tag
)

// hostSectionTitle is the header label of a host section.
func hostSectionTitle(section string) string { return strings.ToUpper(section) }

// engineTitle is "Docker" / "Podman" for messages.
// The one text for "no Docker or Podman on this machine", in every section.
const (
	engineMissingNote = "No container engine"
	engineMissingHint = "Install Docker or Podman."
)

// engineMissingRows is the one error state of "no Docker or Podman": a
// title, one plain sentence and the Retry button with its key (C-5).
func engineMissingRows(section string) []svcRow {
	return []svcRow{
		{kind: svcRowNote, section: section, text: engineMissingNote},
		{kind: svcRowHint, section: section, text: engineMissingHint},
		retryRow(section),
	}
}

func (st *servicesState) engineTitle() string {
	k := st.containerEngine.Kind
	if k == "" {
		return "The engine"
	}
	return strings.ToUpper(k[:1]) + k[1:]
}

// engineLine is "docker 27 · default": engine, major version, context.
func (st *servicesState) engineLine() string {
	e := st.containerEngine
	s := e.Kind
	if v := e.Version.Version; v != "" {
		s += " " + strings.Split(v, ".")[0]
	}
	if ctx := st.activeContext(); ctx != "" {
		s += " · " + ctx
	}
	return s
}

// errLine is the first line of an engine error, trimmed.
func errLine(s string) string { return strings.TrimSpace(firstLine(strings.TrimSpace(s))) }

// hostRows are the rows of every shown host section.
func (st *servicesState) hostRows() []svcRow {
	var out []svcRow
	absent := st.engineAbsent()
	for _, sec := range hostSectionOrder {
		if !st.sectionShown(sec) || (sec == "contexts" && st.contextsHidden()) {
			continue
		}
		// No engine: CONTAINERS says so once; the other engine sections
		// and REGISTRIES are not drawn (plan §2.9).
		if absent && sec != "containers" && sec != "help" {
			continue
		}
		key := hostSection(sec)
		out = append(out, svcRow{kind: svcRowSection, section: key})
		if st.collapsed[key] {
			continue
		}
		out = append(out, st.hostSectionBody(sec)...)
	}
	return out
}

func (st *servicesState) hostSectionBody(sec string) []svcRow {
	key := hostSection(sec)
	hint := func(t string) svcRow { return svcRow{kind: svcRowHint, section: key, text: t} }
	note := func(t string) svcRow { return svcRow{kind: svcRowNote, section: key, text: t} }
	switch sec {
	case "help":
		return st.helpSectionBody(key)
	case "registries":
		return st.registrySectionBody(key)
	}
	if !st.containerDetected {
		return []svcRow{hint("Loading…")}
	}
	if st.containerEngine.Binary == "" {
		return engineMissingRows(key)
	}
	if sec == "contexts" {
		return st.contextSectionBody(key)
	}
	if !st.containerEngine.Reachable {
		name := st.engineTitle()
		p := st.containerEngine.Problem
		if rows := st.remoteContextRows(sec, key, p.Detail); rows != nil {
			return rows
		}
		if sec != "containers" || (p.Kind == services.ProblemDaemonDown && st.projectBannerShown()) {
			return []svcRow{hint(name + " is off")}
		}
		switch p.Kind {
		case services.ProblemNoPermission:
			return []svcRow{note("No access to " + name),
				{kind: svcRowCode, section: key, text: svcPermissionFix},
				hint("y copies it · R to retry")}
		case services.ProblemDaemonDown:
			return []svcRow{note(name + " is not running"), retryRow(key)}
		}
		msg := errLine(p.Detail)
		if msg == "" {
			msg = name + " does not answer"
		}
		return []svcRow{note(msg), hint("R to retry")}
	}
	if err := st.containerErrors[sec]; err != "" {
		if rows := st.remoteContextRows(sec, key, err); rows != nil {
			return rows
		}
		return []svcRow{note(errLine(err)), hint("R to retry")}
	}
	if !st.hostLoaded[sec] {
		return []svcRow{hint("Loading…")}
	}
	switch sec {
	case "containers":
		return st.containerRowsOf(key)
	case "images":
		return st.imageRowsOf(key)
	case "networks":
		if len(st.networkRows) == 0 {
			return []svcRow{hint("No networks")}
		}
		var out []svcRow
		for i, n := range st.networkRows {
			row := svcRow{kind: svcRowHostItem, section: key, host: hostNetwork, idx: i, hostID: n.Name, name: n.Name, depth: 1}
			out = append(out, row)
			out = append(out, st.storageUserRows(row)...)
		}
		return out
	case "volumes":
		if len(st.volumeRows) == 0 {
			return []svcRow{hint("No volumes")}
		}
		var out []svcRow
		for i, v := range st.volumeRows {
			row := svcRow{kind: svcRowHostItem, section: key, host: hostVolume, idx: i, hostID: v.Name, name: v.Name, depth: 1}
			out = append(out, row)
			out = append(out, st.storageUserRows(row)...)
		}
		return out
	}
	return nil
}

// contextsHidden: CONTEXTS is drawn only when it has something to choose
// from: not while only one context (podman: one choice) exists, or the
// list is not known yet.
func (st *servicesState) contextsHidden() bool {
	if !st.hasContexts() || st.containerEngine.Binary == "" {
		return true
	}
	if st.sessionContext != "" {
		return false
	}
	return !st.hostLoaded["contexts"] || len(st.contextChoices()) < 2
}

// podmanLocal is the CONTEXTS row of podman's own local socket (Linux):
// "no connection". Parentheses never occur in a connection name we use.
const podmanLocal = "(local)"

// contextChoices are the CONTEXTS rows: docker contexts, or podman
// connections after a "local" row on Linux (plain podman runs locally
// there, so going back to it must be a choice).
func (st *servicesState) contextChoices() []containers.Context {
	if !st.isPodman() || runtime.GOOS != "linux" || !st.hostLoaded["contexts"] {
		return st.contextRows
	}
	out := make([]containers.Context, 0, len(st.contextRows)+1)
	out = append(out, containers.Context{Name: podmanLocal, DockerEndpoint: "this machine (no connection)"})
	return append(out, st.contextRows...)
}

// contextDisplayName is how a context row reads.
func contextDisplayName(name string) string {
	if name == podmanLocal {
		return "local"
	}
	return sanitizeOutputLine(name)
}

// contextSectionBody lists the contexts. It does not need a running
// daemon: `docker context ls` reads files, so a failing remote context
// can always be switched away from.
func (st *servicesState) contextSectionBody(key string) []svcRow {
	hint := func(t string) svcRow { return svcRow{kind: svcRowHint, section: key, text: t} }
	if err := st.containerErrors["contexts"]; err != "" {
		return []svcRow{{kind: svcRowNote, section: key, text: sanitizeOutputLine(errLine(err))}, hint("R to retry")}
	}
	if !st.hostLoaded["contexts"] {
		return []svcRow{hint("Loading…")}
	}
	choices := st.contextChoices()
	if len(choices) == 0 {
		return []svcRow{hint("No " + st.contextWord() + "s")}
	}
	active := st.activeContext()
	if active == "" && st.isPodman() {
		active = podmanLocal
	}
	var out []svcRow
	for i, c := range choices {
		out = append(out, svcRow{kind: svcRowHostItem, section: key, host: hostContext, idx: i, hostID: c.Name, name: contextDisplayName(c.Name), current: c.Name == active})
	}
	return out
}

// remoteContextRows: while the session uses a context and its engine
// does not answer, name the context in the section note (nil = not that
// case). Only CONTAINERS carries the full note; the others point to it.
func (st *servicesState) remoteContextRows(sec, key, detail string) []svcRow {
	if st.sessionContext == "" || !st.hasContexts() {
		return nil
	}
	name := sanitizeOutputLine(st.sessionContext)
	word := st.contextWord()
	if sec != "containers" {
		return []svcRow{{kind: svcRowHint, section: key, text: word + " " + name + " is not reachable"}}
	}
	msg := shortRemoteError(sanitizeOutputLine(errLine(detail)))
	low := strings.ToLower(msg)
	if msg == "" {
		msg = errLine(st.containerEngine.Problem.Message())
	}
	if msg == "" || strings.Contains(low, "deadline exceeded") || strings.HasSuffix(low, "version timed out") || low == "timed out" {
		msg = "timed out"
	}
	return []svcRow{{kind: svcRowNote, section: key, text: word + " " + name + ": " + msg},
		{kind: svcRowHint, section: key, text: "R retry · Enter on CONTEXTS to switch back"}}
}

var sshHostPortRe = lazyre.New(`^ssh: connect to host \S+ port \d+: `)

// shortRemoteError drops the engine's wrapper words so the cause fits a
// narrow sidebar: "error during connect: ssh: connect to host h port 22:
// Connection timed out" becomes "ssh: Connection timed out".
func shortRemoteError(s string) string {
	s = strings.TrimPrefix(s, "error during connect: ")
	return sshHostPortRe.ReplaceAllString(s, "ssh: ")
}

// containerRowsOf groups the host's containers by compose project (the
// workspace's project first, unlabeled ones last as "other").
func (st *servicesState) containerRowsOf(key string) []svcRow {
	out := []svcRow{{kind: svcRowHint, section: key, text: st.engineLine()}}
	if st.stats.lastFail != "" {
		out = append(out, svcRow{kind: svcRowHint, section: key, text: st.stats.lastFail})
	}
	if len(st.containerRows) == 0 {
		return append(out, svcRow{kind: svcRowHint, section: key, text: "No containers"})
	}
	index := make(map[string]int, len(st.containerRows))
	for i, c := range st.containerRows {
		index[c.ID] = i
	}
	for _, g := range containers.GroupByProject(st.containerRows, st.project) {
		gkey := hostGroup("containers", g.Name)
		up := 0
		for _, c := range g.Containers {
			if strings.EqualFold(c.State, "running") {
				up++
			}
		}
		out = append(out, svcRow{kind: svcRowGroup, section: gkey, name: g.Name,
			count: fmt.Sprintf("%d/%d", up, len(g.Containers)), current: st.project != "" && g.Name == st.project})
		if st.collapsed[gkey] && st.filter == "" { // a filter looks inside folded groups
			continue
		}
		for _, c := range g.Containers {
			group := g.Name
			if g.IsOther {
				group = ""
			}
			out = append(out, svcRow{kind: svcRowHostItem, section: key, host: hostContainer, idx: index[c.ID],
				hostID: c.ID, name: containerDisplayName(c, group), depth: 1})
			out = append(out, st.statsDetailRows(c, 2)...)
		}
	}
	return out
}

// containerDisplayName is the short name under a project group: the
// compose service and its number ("postgres-1"), not the full
// "termocode-postgres-1" that would be cut off in a narrow sidebar.
func containerDisplayName(c containers.Container, group string) string {
	name := c.ID
	if len(name) > 12 {
		name = name[:12]
	}
	if len(c.Names) > 0 {
		name = strings.TrimPrefix(c.Names[0], "/")
	}
	if group == "" {
		return name
	}
	if svc := c.Labels["com.docker.compose.service"]; svc != "" {
		if n := c.Labels["com.docker.compose.container-number"]; n != "" {
			return svc + "-" + n
		}
		return svc
	}
	for _, sep := range []string{"-", "_"} {
		if p := group + sep; strings.HasPrefix(name, p) && len(name) > len(p) {
			return name[len(p):]
		}
	}
	return name
}

// containerShownName is the name the list shows for c; every card, toast
// and AI card for the user uses it, so one container has one name.
func containerShownName(c containers.Container) string {
	return containerDisplayName(c, c.Project)
}

const danglingRepo = "<none>"

// imageRefs maps each repo to its (tag, image index) pairs.
type imageRef struct {
	tag string
	ref string
	idx int
}

func (st *servicesState) imageRepos() (map[string][]imageRef, []string) {
	byRepo := map[string][]imageRef{}
	for i, image := range st.imageRows {
		tags := image.RepoTags
		if len(tags) == 0 {
			tags = []string{danglingRepo + ":" + danglingRepo}
		}
		for _, ref := range tags {
			repo, tag := splitImageTag(ref)
			if repo == danglingRepo || tag == danglingRepo {
				repo, tag = danglingRepo, shortImageID(image.ID)
			}
			byRepo[repo] = append(byRepo[repo], imageRef{tag: tag, ref: ref, idx: i})
		}
	}
	repos := make([]string, 0, len(byRepo))
	for repo := range byRepo {
		repos = append(repos, repo)
	}
	sort.Slice(repos, func(i, j int) bool {
		if (repos[i] == danglingRepo) != (repos[j] == danglingRepo) {
			return repos[j] == danglingRepo
		}
		return repos[i] < repos[j]
	})
	return byRepo, repos
}

func (st *servicesState) imageRowsOf(key string) []svcRow {
	byRepo, repos := st.imageRepos()
	if len(repos) == 0 {
		return []svcRow{{kind: svcRowHint, section: key, text: "No images"}}
	}
	inUse := st.imageUseCounts(byRepo)
	var out []svcRow
	for _, repo := range repos {
		gkey := hostGroup("images", repo)
		label := repo
		if repo == danglingRepo {
			label = "<none> dangling"
		}
		count := strconv.Itoa(uniqueImages(st.imageRows, byRepo[repo]))
		if repo != danglingRepo {
			if used := inUse[repo]; used > 0 {
				count += " · " + strconv.Itoa(used) + " in use"
			}
		}
		out = append(out, svcRow{kind: svcRowGroup, section: gkey, name: label, count: count})
		if st.collapsed[gkey] && st.filter == "" { // a filter looks inside folded groups
			continue
		}
		for _, ref := range byRepo[repo] {
			id := st.imageRows[ref.idx].ID
			out = append(out, svcRow{kind: svcRowHostItem, section: key, host: hostImage, idx: ref.idx,
				hostID: shortImageID(id) + ":" + ref.tag, name: ref.tag, tag: ref.tag, imageRef: ref.ref, depth: 2})
		}
		out = append(out, st.imageRemoteRows(key, repo)...)
	}
	return out
}

func uniqueImages(images []containers.Image, refs []imageRef) int {
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if ref.idx >= 0 && ref.idx < len(images) {
			seen[images[ref.idx].ID] = true
		}
	}
	return len(seen)
}

// imageUseCounts is the "N in use" of each repository: the containers
// that run one of its images, by image ID or by any tag of those images.
// A container counts once per repository. One pass over the containers
// for all repositories (it was one pass per repository).
func (st *servicesState) imageUseCounts(byRepo map[string][]imageRef) map[string]int {
	reposOf := make([][]string, len(st.imageRows)) // image index → the repositories that list it
	for repo, refs := range byRepo {
		for _, ref := range refs {
			if !slices.Contains(reposOf[ref.idx], repo) {
				reposOf[ref.idx] = append(reposOf[ref.idx], repo)
			}
		}
	}
	byID, byTag := map[string][]int{}, map[string][]int{}
	for i, im := range st.imageRows {
		if len(reposOf[i]) == 0 {
			continue
		}
		id := strings.TrimPrefix(im.ID, "sha256:")
		byID[id] = append(byID[id], i)
		for _, tag := range im.RepoTags {
			byTag[tag] = append(byTag[tag], i)
		}
	}
	counts := make(map[string]int, len(byRepo))
	var hit []string
	for _, c := range st.containerRows {
		hit = hit[:0]
		for _, list := range [][]int{byID[strings.TrimPrefix(c.ImageID, "sha256:")], byTag[c.Image]} {
			for _, i := range list {
				for _, repo := range reposOf[i] {
					if !slices.Contains(hit, repo) {
						hit = append(hit, repo)
					}
				}
			}
		}
		for _, repo := range hit {
			counts[repo]++
		}
	}
	return counts
}

func storageRowDetail(st *servicesState, r svcRow) string {
	if r.host == hostNetwork && r.idx >= 0 && r.idx < len(st.networkRows) {
		n := st.networkRows[r.idx]
		return strings.TrimSpace(n.Driver + " · " + n.Scope)
	}
	if r.host == hostVolume && r.idx >= 0 && r.idx < len(st.volumeRows) {
		v := st.volumeRows[r.idx]
		detail := v.Driver
		if v.Size != "" {
			detail += " · " + v.Size
		}
		return detail
	}
	return ""
}

// hostGroupKeys are the group keys the current data has (folded or not).
func (st *servicesState) hostGroupKeys() map[string]bool {
	out := map[string]bool{}
	for _, g := range containers.GroupByProject(st.containerRows, st.project) {
		out[hostGroup("containers", g.Name)] = true
	}
	_, repos := st.imageRepos()
	for _, repo := range repos {
		out[hostGroup("images", repo)] = true
	}
	return out
}

func shortImageID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		id = id[:12]
	}
	return id
}

func splitImageTag(ref string) (string, string) {
	lastSlash := strings.LastIndexByte(ref, '/')
	colon := strings.LastIndexByte(ref, ':')
	if colon > lastSlash {
		return ref[:colon], ref[colon+1:]
	}
	return ref, "latest"
}

// hostSectionCount is the header count: "" when there is nothing to say
// (HELP, a folded section never loaded, no engine), "…" while loading,
// "?" when the engine does not answer.
func (st *servicesState) hostSectionCount(sec string) string {
	visible := st.hostSectionVisible(sec)
	loading := ""
	if visible {
		loading = "…"
	}
	switch sec {
	case "help":
		return ""
	case "registries":
		if !st.hostLoaded[sec] {
			return loading
		}
		return strconv.Itoa(len(st.registryRows))
	}
	switch {
	case !st.containerDetected:
		if st.containerDetecting {
			return loading
		}
		return ""
	case st.containerEngine.Binary == "":
		return ""
	case !st.containerEngine.Reachable:
		return "?"
	case !st.hostLoaded[sec]:
		return loading
	}
	switch sec {
	case "containers":
		return strconv.Itoa(len(st.containerRows))
	case "images":
		return strconv.Itoa(len(st.imageRows))
	case "networks":
		return strconv.Itoa(len(st.networkRows))
	case "volumes":
		return strconv.Itoa(len(st.volumeRows))
	case "contexts":
		return strconv.Itoa(len(st.contextChoices()))
	}
	return ""
}

// ── Look ─────────────────────────────────────────────────────────────────

// containerGlyph is a container state's glyph and colour (plan §2.5).
func containerGlyph(c containers.Container) (theme.Icon, theme.Color256) {
	status := strings.ToLower(c.Status)
	switch strings.ToLower(c.State) {
	case "running":
		if strings.Contains(status, "(unhealthy)") {
			return theme.GlyphSvcBad, theme.DiagError
		}
		if strings.Contains(status, "health: starting") {
			return theme.GlyphSvcStarting, theme.GitModified
		}
		return theme.GlyphSvcUp, theme.GitAdded
	case "paused":
		return theme.GlyphContPaused, theme.GitModified
	case "restarting":
		return theme.GlyphSvcRestarting, theme.GitModified
	case "created", "removing":
		return theme.GlyphSvcStarting, theme.GitModified
	case "exited":
		if containerExitCode(c.Status) != 0 {
			return theme.GlyphSvcExited, theme.DiagError
		}
		return theme.GlyphSvcStopped, theme.TextDim
	case "dead":
		return theme.GlyphSvcExited, theme.DiagError
	}
	return theme.GlyphSvcStopped, theme.TextDim
}

// containerExitCode reads "Exited (1) 2 hours ago" (0 when unknown).
func containerExitCode(status string) int {
	if !strings.HasPrefix(status, "Exited (") {
		return 0
	}
	rest := strings.TrimPrefix(status, "Exited (")
	end := strings.IndexByte(rest, ')')
	if end < 0 {
		return 0
	}
	n, _ := strconv.Atoi(rest[:end])
	return n
}

// containerUptime is the right column: "2h", "done", "exit 1", "paused".
func containerUptime(c containers.Container) (string, theme.Color256) {
	switch strings.ToLower(c.State) {
	case "paused":
		return "paused", theme.TextMuted
	case "restarting":
		return "—", theme.TextMuted
	case "created":
		return "new", theme.TextMuted
	case "exited", "dead":
		if code := containerExitCode(c.Status); code != 0 {
			return "exit " + strconv.Itoa(code), theme.DiagError
		}
		return "done", theme.TextMuted
	case "running":
		return shortDockerDuration(c.Status), theme.TextMuted
	}
	return "", theme.TextMuted
}

// shortDockerDuration turns "Up 2 hours (healthy)" into "2h".
func shortDockerDuration(status string) string {
	s := strings.TrimPrefix(strings.TrimSpace(status), "Up ")
	if i := strings.Index(s, " ("); i >= 0 {
		s = s[:i]
	}
	f := strings.Fields(s)
	if len(f) >= 2 && f[0] == "Less" {
		return "<1s"
	}
	if len(f) >= 3 && f[0] == "About" {
		f = []string{"1", f[2]}
	}
	if len(f) < 2 {
		return ""
	}
	units := map[string]string{"second": "s", "minute": "m", "hour": "h", "day": "d", "week": "w", "month": "mo", "year": "y"}
	u, ok := units[strings.TrimSuffix(f[1], "s")]
	if !ok {
		return ""
	}
	if _, err := strconv.Atoi(f[0]); err != nil {
		return ""
	}
	return f[0] + u
}

// shortAge is "3w" for a creation time.
func shortAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return strconv.Itoa(max(int(d.Minutes()), 1)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	case d < 14*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	case d < 60*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24/7)) + "w"
	case d < 730*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24/30)) + "mo"
	}
	return strconv.Itoa(int(d.Hours()/24/365)) + "y"
}

// imageCreated is the image's creation time (zero when unknown).
func imageCreated(im containers.Image) time.Time {
	if !im.Created.IsZero() {
		return im.Created
	}
	for _, layout := range []string{"2006-01-02 15:04:05 -0700 MST", time.RFC3339} {
		if t, err := time.Parse(layout, im.CreatedAt); err == nil {
			return t
		}
	}
	return time.Time{}
}

// firstPublishedPort is ":8081" from "0.0.0.0:8081->80/tcp, …" ("" when
// nothing is published).
func firstPublishedPort(ports string) string {
	for _, p := range strings.Split(ports, ",") {
		host, _, ok := strings.Cut(strings.TrimSpace(p), "->")
		if !ok {
			continue
		}
		if i := strings.LastIndexByte(host, ':'); i >= 0 {
			host = host[i+1:]
		}
		if host != "" {
			return ":" + host
		}
	}
	return ""
}

// hostCol is one right-hand column of a host row.
type hostCol struct {
	text  string
	width int
	right bool
	col   theme.Color256
}

// renderHostLine draws lead, indent, an optional glyph, the name (takes
// the free width) and fixed columns, exactly w cells.
func renderHostLine(active, focused bool, w, depth int, glyph string, gcol theme.Color256, name string, ncol theme.Color256, cols []hostCol) string {
	bg := gitSelBg(active, focused)
	row := lipgloss.NewStyle().Background(bg)
	fg := func(c theme.Color256) lipgloss.Style {
		return lipgloss.NewStyle().Background(bg).Foreground(theme.LG(c))
	}
	indent := 2 * depth
	fixed := 1 + indent
	gw := 0 // glyph and its space; the ASCII tier of an icon can be 3 cells ("(i)")
	if glyph != "" {
		gw = runewidth.StringWidth(glyph) + 1
		fixed += gw
	}
	for _, c := range cols {
		fixed += 1 + c.width
	}
	nameW := w - fixed
	for nameW < 4 && len(cols) > 0 { // too narrow: drop columns from the right
		last := cols[len(cols)-1]
		cols = cols[:len(cols)-1]
		nameW += 1 + last.width
	}
	nameW = max(nameW, 1)
	var b strings.Builder
	b.WriteString(gitLead(active, focused, bg))
	b.WriteString(row.Render(strings.Repeat(" ", indent)))
	used := 1 + indent
	if glyph != "" {
		b.WriteString(fg(gcol).Render(glyph))
		b.WriteString(row.Render(" "))
		used += gw
	}
	n := svcTruncMiddle(name, nameW)
	b.WriteString(fg(ncol).Render(n))
	b.WriteString(row.Render(strings.Repeat(" ", max(nameW-runewidth.StringWidth(n), 0))))
	used += nameW
	for _, c := range cols {
		t := runewidth.Truncate(c.text, c.width, "")
		pad := strings.Repeat(" ", max(c.width-runewidth.StringWidth(t), 0))
		b.WriteString(row.Render(" "))
		if c.right {
			b.WriteString(row.Render(pad))
			b.WriteString(fg(c.col).Render(t))
		} else {
			b.WriteString(fg(c.col).Render(t))
			b.WriteString(row.Render(pad))
		}
		used += 1 + c.width
	}
	return clampSidebarRow(b.String(), used, w, row)
}

// renderHostGroup is a group row: "▾ termocode · ········ 2/2".
func renderHostGroup(r svcRow, collapsed, active, focused bool, w int) string {
	bg := gitSelBg(active, focused)
	row := lipgloss.NewStyle().Background(bg)
	fg := func(c theme.Color256) lipgloss.Style {
		return lipgloss.NewStyle().Background(bg).Foreground(theme.LG(c))
	}
	fold := theme.GlyphExpand
	if collapsed {
		fold = theme.GlyphTreeCollapsed
	}
	dot := "·"
	if theme.CurrentIconMode == theme.IconModeASCII {
		dot = "."
	}
	indent := 2 * r.depth
	mark := ""
	if r.current {
		// The same containers are in PROJECT above: say so, quietly.
		mark = " (project)"
	}
	cw := runewidth.StringWidth(r.count)
	room := w - 1 - indent - 2 - runewidth.StringWidth(mark) - 2 - cw
	name := svcTruncMiddle(r.name, max(room, 1))
	var b strings.Builder
	b.WriteString(gitLead(active, focused, bg))
	b.WriteString(row.Render(strings.Repeat(" ", indent)))
	b.WriteString(fg(theme.TextDim).Render(fold.String()))
	b.WriteString(row.Render(" "))
	labelCol := theme.TextSecondary
	if active && focused {
		labelCol = theme.TextPrimary
	}
	b.WriteString(fg(labelCol).Render(name))
	used := 1 + indent + 2 + runewidth.StringWidth(name)
	if mark != "" {
		b.WriteString(fg(theme.TextDim).Render(mark))
		used += runewidth.StringWidth(mark)
	}
	// The leader counts cells, not dots: a terminal that draws "·" two
	// cells wide (East Asian ambiguous width) gets half as many.
	dw := max(runewidth.StringWidth(dot), 1)
	if dots := (w - used - cw - 2) / dw; dots > 0 {
		b.WriteString(row.Render(" "))
		b.WriteString(fg(theme.BorderSubtle).Render(strings.Repeat(dot, dots)))
		used += 1 + dots*dw
	}
	b.WriteString(row.Render(" "))
	b.WriteString(fg(theme.TextMuted).Render(r.count))
	used += 1 + cw
	return clampSidebarRow(b.String(), used, w, row)
}

// renderHostItem draws one host object row (plan §2.4 width ladder).
func (st *servicesState) renderHostItem(r svcRow, active, focused bool, w int) string {
	switch r.host {
	case hostContainer:
		if r.idx < 0 || r.idx >= len(st.containerRows) {
			break
		}
		c := st.containerRows[r.idx]
		g, gcol := containerGlyph(c)
		if st.busy[containerBusyPrefix+c.ID].Busy() {
			g, gcol = theme.GlyphSvcStarting, theme.TextSecondary
		}
		var cols []hostCol
		if sc := st.statsRowCol(c, w); sc != nil { // live CPU / memory takes the image's place
			cols = append(cols, *sc)
		} else if w >= 44 {
			free := w - 1 - 2*r.depth - 2 - 7 - 7 - 18 - 1
			if free >= 6 {
				cols = append(cols, hostCol{text: c.Image, width: free, col: theme.TextDim})
			}
		}
		if p := firstPublishedPort(c.PortsText); p != "" && w >= 22 {
			cols = append(cols, hostCol{text: p, width: 6, col: theme.TextSecondary})
		}
		if w >= 28 {
			up, ucol := containerUptime(c)
			cols = append(cols, hostCol{text: up, width: 6, right: true, col: ucol})
		}
		ncol := theme.TextPrimary
		if gcol == theme.TextDim {
			ncol = theme.TextSecondary
		}
		return renderHostLine(active, focused, w, r.depth, g.String(), gcol, r.name, ncol, cols)
	case hostImage:
		if r.idx < 0 || r.idx >= len(st.imageRows) {
			break
		}
		im := st.imageRows[r.idx]
		var cols []hostCol
		if w >= 22 {
			cols = append(cols, hostCol{text: im.Size, width: 7, right: true, col: theme.TextMuted})
		}
		if w >= 28 && st.imagesHaveAge() {
			age := ""
			if t := imageCreated(im); !t.IsZero() {
				age = shortAge(clock.Now().Sub(t))
			}
			cols = append(cols, hostCol{text: age, width: 4, right: true, col: theme.TextMuted})
		}
		return renderHostLine(active, focused, w, r.depth, "", 0, r.tag, theme.TextPrimary, cols)
	case hostNetwork:
		if r.idx < 0 || r.idx >= len(st.networkRows) {
			break
		}
		n := st.networkRows[r.idx]
		cols := []hostCol{{text: n.Driver, width: 6, col: theme.TextSecondary}, {text: n.Scope, width: 5, col: theme.TextMuted}}
		return renderHostLine(active, focused, w, r.depth, storageFoldGlyph(st, r), theme.TextDim, n.Name, theme.TextPrimary, cols)
	case hostVolume:
		if r.idx < 0 || r.idx >= len(st.volumeRows) {
			break
		}
		v := st.volumeRows[r.idx]
		var cols []hostCol
		if w >= 22 {
			text, col := v.Size, theme.TextMuted
			if text == "" {
				text, col = v.Driver, theme.TextDim
			}
			cols = append(cols, hostCol{text: text, width: 7, right: true, col: col})
		}
		return renderHostLine(active, focused, w, r.depth, storageFoldGlyph(st, r), theme.TextDim, v.Name, theme.TextPrimary, cols)
	case hostContext:
		g, gcol, ncol := theme.GlyphSvcStopped, theme.TextDim, theme.TextSecondary
		if r.current {
			g, gcol, ncol = theme.GlyphSvcUp, theme.GitAdded, theme.TextPrimary
		}
		return renderHostLine(active, focused, w, r.depth, g.String(), gcol, r.name, ncol, nil)
	case hostRegistry, hostRegistryConnect:
		g, gcol, ncol := st.registryRowLook(r)
		return renderHostLine(active, focused, w, r.depth, g, gcol, r.name, ncol, nil)
	case hostHelpGuide, hostHelpKeys, hostHelpReport:
		g, gcol, ncol := helpRowLook(r)
		return renderHostLine(active, focused, w, r.depth, g, gcol, r.name, ncol, nil)
	case hostRemoteRepo, hostRemoteTag:
		return st.renderRemoteRow(r, active, focused, w)
	case hostRegistryImage:
		var cols []hostCol
		if w >= 22 && r.idx >= 0 && r.idx < len(st.imageRows) {
			cols = append(cols, hostCol{text: st.imageRows[r.idx].Size, width: 7, right: true, col: theme.TextMuted})
		}
		return renderHostLine(active, focused, w, r.depth, "", 0, r.name, theme.TextPrimary, cols)
	}
	return renderHostLine(active, focused, w, r.depth, "", 0, r.name, theme.TextPrimary, nil)
}

func (st *servicesState) imagesHaveAge() bool {
	for _, im := range st.imageRows {
		if !imageCreated(im).IsZero() {
			return true
		}
	}
	return false
}

// hostFooter is the two footer lines for a host header, group or row.
func (st *servicesState) hostFooter(r svcRow) (string, string) {
	switch r.kind {
	case svcRowSection:
		sec := strings.TrimPrefix(r.section, "host/")
		switch sec {
		case "containers":
			if st.containerDetected && st.containerEngine.Reachable {
				l1 := st.containerEngine.Kind
				if v := st.containerEngine.Version.Version; v != "" {
					l1 += " " + v
				}
				if ctx := st.activeContext(); ctx != "" {
					l1 += " · " + st.contextWord() + " " + ctx
				}
				return l1, "? keys · Tab next"
			}
			return "containers on this machine", "? keys · Tab next"
		case "images":
			return "images on this machine", "u pull · z prune · b tags · R refresh"
		case "registries":
			return "registries you signed in to", "n connect · s search · b browse tags · u pull"
		case "networks":
			return "networks on this machine", "n create · D remove · z prune · R refresh"
		case "volumes":
			return "volumes on this machine", "n create · D remove · z prune · d disk usage · R refresh"
		case "contexts":
			if st.isPodman() {
				return "podman connections", "Enter use here · D make default"
			}
			return "engine contexts", "Enter use here · D make default"
		case "help":
			return "guide, keys and problem reports", "space fold · m sections"
		}
	case svcRowGroup:
		sec, _, _ := strings.Cut(strings.TrimPrefix(r.section, "grp/"), "/")
		if sec == "containers" {
			if r.name == "other" {
				return "containers without a compose project", "space fold"
			}
			l1 := "compose project " + r.name
			if r.current {
				l1 += " (this folder)"
			}
			return l1, "space fold"
		}
		return "image " + r.name, "space fold · b remote tags"
	case svcRowHostItem:
		switch r.host {
		case hostContainer:
			if r.idx < len(st.containerRows) {
				c := st.containerRows[r.idx]
				l1 := c.Image
				if p := strings.ReplaceAll(c.PortsText, "->", "→"); p != "" {
					l1 += " · " + p
				}
				if busy := st.busy[containerBusyPrefix+c.ID]; busy.Busy() {
					return l1, busy.String()
				}
				return l1, c.Status
			}
		case hostImage:
			if r.idx < len(st.imageRows) {
				im := st.imageRows[r.idx]
				l2 := im.Size
				if t := imageCreated(im); !t.IsZero() {
					l2 += " · " + shortAge(clock.Now().Sub(t)) + " old"
				}
				return shortImageID(im.ID) + " · " + r.tag, l2
			}
		case hostNetwork:
			if r.idx < len(st.networkRows) {
				n := st.networkRows[r.idx]
				return n.Name, n.Driver + " · " + n.Scope
			}
		case hostVolume:
			if r.idx < len(st.volumeRows) {
				v := st.volumeRows[r.idx]
				l2 := v.Mountpoint
				if l2 == "" {
					l2 = v.Driver
				}
				return v.Name, l2
			}
		case hostContext:
			if choices := st.contextChoices(); r.idx < len(choices) {
				c := choices[r.idx]
				l1 := contextDisplayName(c.Name)
				if r.current {
					l1 += " (active)"
				}
				l2 := sanitizeOutputLine(c.DockerEndpoint)
				if c.Error != "" {
					l2 = sanitizeOutputLine(errLine(c.Error))
				}
				return l1, l2
			}
		case hostRegistry, hostRegistryImage, hostRegistryConnect:
			return st.registryFooter(r)
		case hostRemoteRepo, hostRemoteTag:
			return st.remoteFooter(r)
		case hostHelpGuide, hostHelpKeys, hostHelpReport:
			return helpRowFooter(r)
		}
	}
	return "", ""
}
