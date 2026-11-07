package containers

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// Detail is what a container detail page shows (CTX-10). EnvNames holds
// only the names of the env variables: their values are never kept.
type Detail struct {
	ID, Name, Status string
	ExitCode         int
	RestartCount     int
	Health           DetailHealth
	EnvNames         []string
	Mounts           []DetailMount
	Labels           []DetailLabel // sorted by key
}

// DetailHealth is the health check state and its last results.
type DetailHealth struct {
	Status        string
	FailingStreak int
	Log           []DetailProbe
}

// DetailProbe is one health check run.
type DetailProbe struct {
	Start    string
	ExitCode int
	Output   string
}

// DetailMount is one mount of the container.
type DetailMount struct {
	Type, Name, Source, Destination string
	RW                              bool
}

// DetailLabel is one label.
type DetailLabel struct{ Key, Value string }

// ParseDetail reads the first object of an inspect result.
func ParseDetail(data []byte) (Detail, error) {
	var a []struct {
		ID           string `json:"Id"`
		Name         string
		RestartCount int
		Config       struct {
			Env    []string
			Labels map[string]string
		}
		State struct {
			Status   string
			ExitCode int
			Health   *struct {
				Status        string
				FailingStreak int
				Log           []struct {
					Start    string
					ExitCode int
					Output   string
				}
			}
		}
		Mounts []struct {
			Type, Name, Source, Destination string
			RW                              bool
		}
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return Detail{}, err
	}
	if len(a) == 0 {
		return Detail{}, errors.New("inspect returned no container")
	}
	c := a[0]
	d := Detail{ID: c.ID, Name: strings.TrimPrefix(c.Name, "/"), Status: c.State.Status, ExitCode: c.State.ExitCode, RestartCount: c.RestartCount}
	for _, kv := range c.Config.Env {
		name, _, _ := strings.Cut(kv, "=")
		if name != "" {
			d.EnvNames = append(d.EnvNames, name)
		}
	}
	sort.Strings(d.EnvNames)
	for k, v := range c.Config.Labels {
		d.Labels = append(d.Labels, DetailLabel{Key: k, Value: v})
	}
	sort.Slice(d.Labels, func(i, j int) bool { return d.Labels[i].Key < d.Labels[j].Key })
	for _, mt := range c.Mounts {
		d.Mounts = append(d.Mounts, DetailMount{Type: mt.Type, Name: mt.Name, Source: mt.Source, Destination: mt.Destination, RW: mt.RW})
	}
	if h := c.State.Health; h != nil {
		d.Health.Status, d.Health.FailingStreak = h.Status, h.FailingStreak
		for _, l := range h.Log {
			d.Health.Log = append(d.Health.Log, DetailProbe{Start: l.Start, ExitCode: l.ExitCode, Output: strings.TrimSpace(l.Output)})
		}
	}
	return d, nil
}

// TopTable is the process list of a container (`top`).
type TopTable struct {
	Titles []string
	Rows   [][]string
}

// ParseTop reads `top` output: a header line and one line per process.
// The last column (the command) keeps its inner spaces.
func ParseTop(out string) TopTable {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return TopTable{}
	}
	t := TopTable{Titles: strings.Fields(lines[0])}
	n := len(t.Titles)
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		row := make([]string, 0, n)
		rest := strings.TrimSpace(l)
		for len(row) < n-1 && rest != "" {
			f, r, _ := strings.Cut(rest, " ")
			row = append(row, f)
			rest = strings.TrimLeft(r, " \t")
		}
		row = append(row, rest)
		t.Rows = append(t.Rows, row)
	}
	return t
}

// Top lists the processes of container id.
func (e *Engine) Top(ctx context.Context, id string) (TopTable, error) {
	out, err := e.list(ctx, "top", id)
	if err != nil {
		return TopTable{}, err
	}
	return ParseTop(out), nil
}
