package app

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The Databases view's host tree (SERVERS): the ssh:// hosts that the
// discovery read (the same host list as the Containers view), each host,
// then its compose projects, then its database containers (postgres,
// mysql / mariadb, redis…). A host that does not answer shows its
// one-line reason under it. Picking a container adds it with the SSH
// tunnel, container and port filled in and asks only for the password.
// The list ends with + Add Server… (the Containers view's Create
// Context: a name, then ssh://user@host, then a card, Cancel first).

// dbAddServerPath is the tree path of the + Add Server… row.
const dbAddServerPath = "\x00add-server"

// dbServerPath is the tree path of a host (or a project of a host).
func dbServerPath(parts ...string) string { return "\x00server/" + strings.Join(parts, "/") }

// serverTreeRows are the SERVERS rows (none before the first discovery).
func (s *dbState) serverTreeRows() []dbTreeRow {
	if !s.discoverDone {
		return nil
	}
	out := []dbTreeRow{{kind: dbNodeSuggestionsHeader, name: "SERVERS"}}
	for _, f := range s.discoveredRemote {
		head := dbServerPath(f.host.context)
		label := serverLabel(containers.Context{Name: f.host.context, DockerEndpoint: f.host.endpoint})
		out = append(out, dbTreeRow{kind: dbNodeFolderHeader, path: head, name: label, selectable: true})
		if s.folded[head] && s.filter == "" {
			continue
		}
		if f.err != nil {
			reason := shortRemoteError(sanitizeDBText(errLine(f.err.Error())))
			if reason == "" {
				reason = "does not answer"
			}
			out = append(out, dbTreeRow{kind: dbNodeNote, name: reason, depth: 1})
			continue
		}
		// The database containers of this host, by compose project.
		byProject := map[string][]string{}
		for path, t := range s.remoteTargets {
			if t.context != f.host.context || s.hidden[path] {
				continue
			}
			p := t.cont.Project
			if p == "" {
				p = t.cont.Labels["com.docker.compose.project"]
			}
			if p == "" {
				p = "other"
			}
			byProject[p] = append(byProject[p], path)
		}
		if len(byProject) == 0 {
			out = append(out, dbTreeRow{kind: dbNodeRedisMore, name: "no database containers", depth: 1})
			continue
		}
		projects := make([]string, 0, len(byProject))
		for p := range byProject {
			projects = append(projects, p)
		}
		sort.Strings(projects)
		for _, p := range projects {
			ph := dbServerPath(f.host.context, p)
			out = append(out, dbTreeRow{kind: dbNodeFolderHeader, path: ph, name: sanitizeDBText(p), depth: 1, selectable: true})
			if s.folded[ph] && s.filter == "" {
				continue
			}
			paths := byProject[p]
			sort.Strings(paths)
			for _, path := range paths {
				n := s.nodes[path]
				out = append(out, dbTreeRow{kind: dbNodeSuggestion, path: path, name: n.name, conn: path, detail: n.detail, depth: 2, selectable: true})
			}
		}
	}
	return append(out, dbTreeRow{kind: dbNodeAddServer, path: dbAddServerPath, name: "Add Server…", selectable: true})
}

// databaseAddServer is + Add Server…: the Containers view's Create
// Context (it needs the engine the Containers view found).
func (m *Model) databaseAddServer() tea.Cmd {
	st := m.services
	if st == nil || !st.containerDetected || st.containerEngine.Binary == "" {
		return m.pushToastDetail(toast.Info, "Open the Containers view once", "It finds Docker or Podman; then + Add Server… works here too.")
	}
	return m.askContextCreate()
}
