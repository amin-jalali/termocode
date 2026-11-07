package containers

import (
	"strings"
	"testing"
	"time"
)

// CTX-25: the System page: engine info, a disk summary, recent events.

func TestParseEngineInfoDocker(t *testing.T) {
	in := `{"Name":"box","ServerVersion":"27.3.1","OperatingSystem":"Ubuntu 24.04","OSType":"linux","Architecture":"x86_64","KernelVersion":"6.8.0","NCPU":8,"MemTotal":16777216000,"Driver":"overlay2","DefaultRuntime":"runc","Runtimes":{"runc":{},"crun":{}},"Containers":5,"ContainersRunning":2,"ContainersPaused":0,"ContainersStopped":3,"Images":11,"CgroupDriver":"systemd","CgroupVersion":"2","Warnings":["WARNING: No swap limit support"]}`
	i, err := ParseEngineInfo([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if i.Version != "27.3.1" || i.OS != "Ubuntu 24.04" || i.Arch != "x86_64" || i.CPUs != "8" || i.Memory != "15.6 GiB" || i.Storage != "overlay2" {
		t.Errorf("info = %+v", i)
	}
	if i.Runtime != "runc" || strings.Join(i.Runtimes, ",") != "crun,runc" || i.Containers != "5" || i.Running != "2" || i.Stopped != "3" || i.Images != "11" {
		t.Errorf("counts/runtimes = %+v", i)
	}
	if len(i.Warnings) != 1 || i.Cgroup != "systemd v2" {
		t.Errorf("warnings/cgroup = %+v", i)
	}
}

func TestParseEngineInfoPodman(t *testing.T) {
	in := `{
  "host": {"arch": "arm64", "cpus": 4, "memTotal": 8589934592, "os": "linux", "distribution": {"distribution": "fedora", "version": "40"}, "ociRuntime": {"name": "crun"}, "kernel": "6.9.1", "hostname": "pm"},
  "store": {"graphDriverName": "overlay", "containerStore": {"number": 3, "running": 1, "paused": 0, "stopped": 2}, "imageStore": {"number": 7}},
  "version": {"Version": "5.2.0"}
}`
	i, err := ParseEngineInfo([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if i.Version != "5.2.0" || i.OS != "fedora 40" || i.Arch != "arm64" || i.CPUs != "4" || i.Memory != "8 GiB" || i.Storage != "overlay" || i.Runtime != "crun" || i.Containers != "3" || i.Running != "1" || i.Images != "7" {
		t.Errorf("podman info = %+v", i)
	}
	if _, err := ParseEngineInfo([]byte("nope")); err == nil {
		t.Error("garbage parsed")
	}
}

func TestParseDiskSummary(t *testing.T) {
	in := `{"Active":"2","Reclaimable":"812MB (66%)","Size":"1.2GB","TotalCount":"5","Type":"Images"}
{"Active":"1","Reclaimable":"0B (0%)","Size":"20kB","TotalCount":"3","Type":"Containers"}
{"Active":"0","Reclaimable":"55MB","Size":"55MB","TotalCount":"1","Type":"Local Volumes"}
{"Active":"0","Reclaimable":"0B","Size":"0B","TotalCount":"0","Type":"Build Cache"}
`
	rows, err := ParseDiskSummary([]byte(in))
	if err != nil || len(rows) != 4 {
		t.Fatalf("rows %+v %v", rows, err)
	}
	if r := rows[0]; r.Type != "Images" || r.Total != "5" || r.Active != "2" || r.Size != "1.2GB" || r.Reclaimable != "812MB (66%)" {
		t.Errorf("row 0 = %+v", r)
	}
}

func TestSystemReport(t *testing.T) {
	info := EngineInfo{Version: "27.3.1", OS: "Ubuntu 24.04", Arch: "x86_64", CPUs: "8", Memory: "15.6 GiB", Storage: "overlay2", Runtime: "runc", Runtimes: []string{"crun", "runc"}, Containers: "5", Running: "2", Stopped: "3", Images: "11", Warnings: []string{"WARNING: No swap limit support"}}
	df := []DiskRow{{Type: "Images", Total: "5", Active: "2", Size: "1.2GB", Reclaimable: "812MB (66%)"}}
	at := time.Date(2026, 11, 7, 10, 4, 5, 0, time.UTC)
	ev := []Event{{Type: "container", Action: "start", Actor: "web", Time: at}, {Type: "image", Action: "pull", Actor: "nginx:1.27", Time: at.Add(time.Second)}}
	got := SystemReport("docker", info, df, ev, time.UTC)
	for _, want := range []string{
		"Engine: docker 27.3.1",
		"OS: Ubuntu 24.04 (x86_64)",
		"CPUs: 8   Memory: 15.6 GiB",
		"Storage driver: overlay2",
		"Runtime: runc (also: crun)",
		"Containers: 5 (2 running, 3 stopped)   Images: 11",
		"! WARNING: No swap limit support",
		"Images", "1.2GB", "812MB (66%)",
		"10:04:06  image  pull  nginx:1.27",
		"10:04:05  container  start  web",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report lacks %q:\n%s", want, got)
		}
	}
	// Newest event first.
	if strings.Index(got, "nginx:1.27") > strings.Index(got, "start  web") {
		t.Errorf("events not newest first:\n%s", got)
	}
	if empty := SystemReport("podman", EngineInfo{}, nil, nil, time.UTC); !strings.Contains(empty, "No events yet") || !strings.Contains(empty, "Engine: podman") {
		t.Errorf("empty report:\n%s", empty)
	}
}
