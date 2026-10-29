package containers

import "sort"

// GroupIndex is one project group by position: Idx are the indexes of
// its containers in the list given to GroupIndexes, in list order.
type GroupIndex struct {
	Name       string
	WorkingDir string
	Idx        []int
	IsOther    bool
}

// GroupByProject groups compose-labeled containers and puts the current project first.
// Unlabeled containers are collected in a final group named "other".
func GroupByProject(containers []Container, currentProject string) []ProjectGroup {
	groups := GroupIndexes(containers, currentProject)
	out := make([]ProjectGroup, 0, len(groups))
	for _, g := range groups {
		cs := make([]Container, len(g.Idx))
		for i, idx := range g.Idx {
			cs[i] = containers[idx]
		}
		out = append(out, ProjectGroup{Name: g.Name, WorkingDir: g.WorkingDir, Containers: cs, IsOther: g.IsOther})
	}
	return out
}

// GroupIndexes is GroupByProject without copying the containers: the
// same groups in the same order, by index. A view that draws every
// frame uses it.
func GroupIndexes(containers []Container, currentProject string) []GroupIndex {
	at := make(map[string]int) // name → position in out
	var out []GroupIndex
	for i := range containers {
		c := &containers[i]
		name := c.Project
		if name == "" {
			name = c.Labels["com.docker.compose.project"]
		}
		if name == "" {
			name = c.Labels["io.podman.compose.project"]
		}
		if name == "" {
			name = "other"
		}
		g, ok := at[name]
		if !ok {
			g = len(out)
			at[name] = g
			out = append(out, GroupIndex{Name: name, IsOther: name == "other"})
		}
		out[g].Idx = append(out[g].Idx, i)
		wd := c.WorkingDir
		if wd == "" {
			wd = c.Labels["com.docker.compose.project.working_dir"]
		}
		if wd == "" {
			wd = c.Labels["io.podman.compose.project.working_dir"]
		}
		if wd != "" {
			out[g].WorkingDir = wd
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Name, out[j].Name
		if a == b {
			return false
		}
		if a == currentProject {
			return true
		}
		if b == currentProject {
			return false
		}
		if a == "other" {
			return false
		}
		if b == "other" {
			return true
		}
		return a < b
	})
	return out
}
