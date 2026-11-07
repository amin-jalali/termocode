package containers

// CTX-25: the System page: what the engine is (docker / podman info),
// how much disk it uses (system df) and what happened lately (events).

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// EngineInfo is the part of `docker info` / `podman info` the System
// page shows. Every field is display text ("" = the engine did not say).
type EngineInfo struct {
	Name       string
	Version    string
	OS         string
	Arch       string
	Kernel     string
	CPUs       string
	Memory     string
	Storage    string
	Runtime    string
	Runtimes   []string // every runtime, sorted
	Cgroup     string
	Containers string
	Running    string
	Paused     string
	Stopped    string
	Images     string
	Warnings   []string
}

// DiskRow is one line of `system df`: images, containers, volumes,
// build cache.
type DiskRow struct {
	Type        string
	Total       string
	Active      string
	Size        string
	Reclaimable string
}

// Info reads the engine info.
func (e *Engine) Info(ctx context.Context) (EngineInfo, error) {
	out, err := e.list(ctx, "info", "--format", "{{json .}}")
	if err != nil {
		return EngineInfo{}, err
	}
	return ParseEngineInfo([]byte(out))
}

// DiskSummary reads the `system df` summary rows.
func (e *Engine) DiskSummary(ctx context.Context) ([]DiskRow, error) {
	return parseList(ctx, e, []string{"system", "df", "--format", "{{json .}}"}, ParseDiskSummary)
}

// ParseEngineInfo reads docker's flat info JSON or podman's nested one.
func ParseEngineInfo(data []byte) (EngineInfo, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return EngineInfo{}, fmt.Errorf("engine info: %w", err)
	}
	var i EngineInfo
	if _, podman := raw["host"]; podman {
		i.Name = firstString(raw, "host.hostname")
		i.Version = firstString(raw, "version.Version")
		i.OS = strings.TrimSpace(firstString(raw, "host.distribution.distribution") + " " + firstString(raw, "host.distribution.version"))
		if i.OS == "" {
			i.OS = firstString(raw, "host.os")
		}
		i.Arch = firstString(raw, "host.arch")
		i.Kernel = firstString(raw, "host.kernel")
		i.CPUs = firstString(raw, "host.cpus")
		i.Memory = memText(firstString(raw, "host.memTotal"))
		i.Storage = firstString(raw, "store.graphDriverName")
		i.Runtime = firstString(raw, "host.ociRuntime.name")
		if i.Runtime != "" {
			i.Runtimes = []string{i.Runtime}
		}
		i.Cgroup = strings.TrimSpace(firstString(raw, "host.cgroupManager") + " " + firstString(raw, "host.cgroupVersion"))
		i.Containers = firstString(raw, "store.containerStore.number")
		i.Running = firstString(raw, "store.containerStore.running")
		i.Paused = firstString(raw, "store.containerStore.paused")
		i.Stopped = firstString(raw, "store.containerStore.stopped")
		i.Images = firstString(raw, "store.imageStore.number")
		return i, nil
	}
	i.Name = firstString(raw, "Name")
	i.Version = firstString(raw, "ServerVersion")
	i.OS = firstString(raw, "OperatingSystem", "OSType")
	i.Arch = firstString(raw, "Architecture")
	i.Kernel = firstString(raw, "KernelVersion")
	i.CPUs = firstString(raw, "NCPU")
	i.Memory = memText(firstString(raw, "MemTotal"))
	i.Storage = firstString(raw, "Driver")
	i.Runtime = firstString(raw, "DefaultRuntime")
	var rts map[string]json.RawMessage
	if json.Unmarshal(raw["Runtimes"], &rts) == nil {
		for name := range rts {
			i.Runtimes = append(i.Runtimes, name)
		}
		sort.Strings(i.Runtimes)
	}
	if cd := firstString(raw, "CgroupDriver"); cd != "" {
		i.Cgroup = cd
		if cv := firstString(raw, "CgroupVersion"); cv != "" {
			i.Cgroup += " v" + cv
		}
	}
	i.Containers = firstString(raw, "Containers")
	i.Running = firstString(raw, "ContainersRunning")
	i.Paused = firstString(raw, "ContainersPaused")
	i.Stopped = firstString(raw, "ContainersStopped")
	i.Images = firstString(raw, "Images")
	_ = json.Unmarshal(raw["Warnings"], &i.Warnings)
	return i, nil
}

// memText shows a byte count in GiB with one decimal ("15.6 GiB",
// "8 GiB"); text that is not a number stays as it is.
func memText(s string) string {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n <= 0 {
		return s
	}
	g := strconv.FormatFloat(n/(1<<30), 'f', 1, 64)
	return strings.TrimSuffix(g, ".0") + " GiB"
}

// ParseDiskSummary reads `system df --format '{{json .}}'` lines.
func ParseDiskSummary(data []byte) ([]DiskRow, error) {
	var out []DiskRow
	err := parseLines(data, func(raw map[string]json.RawMessage) error {
		out = append(out, DiskRow{
			Type:        firstString(raw, "Type"),
			Total:       firstString(raw, "TotalCount", "Total"),
			Active:      firstString(raw, "Active"),
			Size:        firstString(raw, "Size"),
			Reclaimable: firstString(raw, "Reclaimable"),
		})
		return nil
	})
	return out, err
}

// maxReportEvents is how many recent events the report lists.
const maxReportEvents = 50

// SystemReport is the System page text: engine, disk, recent events
// (newest first, times in loc).
func SystemReport(kind string, i EngineInfo, df []DiskRow, events []Event, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	var b strings.Builder
	line := func(label, v string) {
		if v != "" {
			b.WriteString(label + ": " + v + "\n")
		}
	}
	b.WriteString(strings.TrimSpace("Engine: "+kind+" "+i.Version) + "\n")
	os := i.OS
	if i.Arch != "" {
		os = strings.TrimSpace(os + " (" + i.Arch + ")")
	}
	line("OS", os)
	line("Kernel", i.Kernel)
	if i.CPUs != "" || i.Memory != "" {
		b.WriteString("CPUs: " + orDash(i.CPUs) + "   Memory: " + orDash(i.Memory) + "\n")
	}
	line("Storage driver", i.Storage)
	if i.Runtime != "" {
		rt := i.Runtime
		var others []string
		for _, r := range i.Runtimes {
			if r != i.Runtime {
				others = append(others, r)
			}
		}
		if len(others) > 0 {
			rt += " (also: " + strings.Join(others, ", ") + ")"
		}
		line("Runtime", rt)
	}
	line("Cgroup", i.Cgroup)
	if i.Containers != "" {
		c := "Containers: " + i.Containers
		var parts []string
		if i.Running != "" {
			parts = append(parts, i.Running+" running")
		}
		if i.Paused != "" && i.Paused != "0" {
			parts = append(parts, i.Paused+" paused")
		}
		if i.Stopped != "" {
			parts = append(parts, i.Stopped+" stopped")
		}
		if len(parts) > 0 {
			c += " (" + strings.Join(parts, ", ") + ")"
		}
		if i.Images != "" {
			c += "   Images: " + i.Images
		}
		b.WriteString(c + "\n")
	}
	for _, w := range i.Warnings {
		b.WriteString("! " + w + "\n")
	}

	b.WriteString("\nDisk\n")
	if len(df) == 0 {
		b.WriteString("  No disk summary.\n")
	} else {
		rows := [][]string{{"TYPE", "TOTAL", "ACTIVE", "SIZE", "RECLAIMABLE"}}
		for _, r := range df {
			rows = append(rows, []string{r.Type, r.Total, r.Active, r.Size, r.Reclaimable})
		}
		widths := make([]int, 5)
		for _, r := range rows {
			for c, v := range r {
				widths[c] = max(widths[c], len(v))
			}
		}
		for _, r := range rows {
			var cells []string
			for c, v := range r {
				cells = append(cells, v+strings.Repeat(" ", widths[c]-len(v)))
			}
			b.WriteString("  " + strings.TrimRight(strings.Join(cells, "  "), " ") + "\n")
		}
	}

	b.WriteString("\nRecent events\n")
	if len(events) == 0 {
		b.WriteString("  No events yet.\n")
		return b.String()
	}
	ev := append([]Event(nil), events...)
	sort.SliceStable(ev, func(a, c int) bool { return ev[a].Time.After(ev[c].Time) })
	if len(ev) > maxReportEvents {
		ev = ev[:maxReportEvents]
	}
	for _, e := range ev {
		b.WriteString("  " + e.Time.In(loc).Format("15:04:05") + "  " + e.Type + "  " + e.Action + "  " + e.Actor + "\n")
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
