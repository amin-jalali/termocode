package containers

import "sort"

// GroupByProject groups compose-labeled containers and puts the current project first.
// Unlabeled containers are collected in a final group named "other".
func GroupByProject(containers []Container, currentProject string) []ProjectGroup {
	byName := make(map[string][]Container)
	dirs := make(map[string]string)
	var names []string
	for _, c := range containers {
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
		if _, ok := byName[name]; !ok {
			names = append(names, name)
		}
		byName[name] = append(byName[name], c)
		wd := c.WorkingDir
		if wd == "" {
			wd = c.Labels["com.docker.compose.project.working_dir"]
		}
		if wd == "" {
			wd = c.Labels["io.podman.compose.project.working_dir"]
		}
		if wd != "" {
			dirs[name] = wd
		}
	}
	sort.SliceStable(names, func(i, j int) bool {
		if names[i] == names[j] {
			return false
		}
		if names[i] == currentProject {
			return true
		}
		if names[j] == currentProject {
			return false
		}
		if names[i] == "other" {
			return false
		}
		if names[j] == "other" {
			return true
		}
		return names[i] < names[j]
	})
	out := make([]ProjectGroup, 0, len(names))
	for _, name := range names {
		out = append(out, ProjectGroup{Name: name, WorkingDir: dirs[name], Containers: byName[name], IsOther: name == "other"})
	}
	return out
}
