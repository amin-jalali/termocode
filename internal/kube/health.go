package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// The cluster report (CTX-27): a pulse summary per kind, an xray owner
// tree (Deployment → ReplicaSet → Pod, Job → Pod, Service → Pod) and a
// sanitizer list of findings. It only reads: the same `get -o json`
// calls as the lists, parsed once more for owners, labels, images,
// limits and probes.

// PulseRow is one kind of the pulse summary.
type PulseRow struct {
	Kind                  string
	Total, OK, Warn, Fail int
}

// TreeNode is one object of the xray tree.
type TreeNode struct {
	Kind, Name, Status string
	Children           []TreeNode
}

// Finding is one sanitizer line; Level is "error" or "warning".
type Finding struct {
	Level, Kind, Name, Text string
}

// Report is the whole cluster report of one namespace.
type Report struct {
	Pulse    []PulseRow
	Tree     []TreeNode
	Findings []Finding
}

// PodSpec is what the report reads from a pod beyond the list row.
type PodSpec struct {
	Name                 string
	OwnerKind, OwnerName string
	Labels               map[string]string
	Images               []string
	NoLimits, NoProbe    []string // container names
}

// ReportInput is the parsed objects the report is built from.
type ReportInput struct {
	Pods        []Pod
	Specs       []PodSpec
	Deployments []Deployment
	Services    []Service
	Selectors   map[string]map[string]string // service name → selector
	Jobs        []Job
}

type rawPodSpec struct {
	Metadata struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
		Owners []struct {
			Kind       string `json:"kind"`
			Name       string `json:"name"`
			Controller bool   `json:"controller"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		Containers []struct {
			Name      string `json:"name"`
			Image     string `json:"image"`
			Resources struct {
				Limits map[string]json.RawMessage `json:"limits"`
			} `json:"resources"`
			Readiness json.RawMessage `json:"readinessProbe"`
		} `json:"containers"`
	} `json:"spec"`
}

// ParseReportInput reads the `get -o json` output of pods, deployments,
// services and jobs (nil or empty: none of that kind).
func ParseReportInput(pods, deploys, services, jobs []byte) (ReportInput, error) {
	var in ReportInput
	var err error
	empty := func(b []byte) bool { return len(strings.TrimSpace(string(b))) == 0 }
	if !empty(pods) {
		if in.Pods, err = ParsePods(pods); err != nil {
			return in, err
		}
		raw, err := parseList[rawPodSpec](pods)
		if err != nil {
			return in, err
		}
		for _, r := range raw {
			s := PodSpec{Name: r.Metadata.Name, Labels: r.Metadata.Labels}
			for _, o := range r.Metadata.Owners {
				if o.Controller || s.OwnerKind == "" {
					s.OwnerKind, s.OwnerName = o.Kind, o.Name
				}
			}
			for _, c := range r.Spec.Containers {
				s.Images = append(s.Images, c.Image)
				if len(c.Resources.Limits["cpu"]) == 0 || len(c.Resources.Limits["memory"]) == 0 {
					s.NoLimits = append(s.NoLimits, c.Name)
				}
				if len(c.Readiness) == 0 || string(c.Readiness) == "null" {
					s.NoProbe = append(s.NoProbe, c.Name)
				}
			}
			in.Specs = append(in.Specs, s)
		}
	}
	if !empty(deploys) {
		if in.Deployments, err = ParseDeployments(deploys); err != nil {
			return in, err
		}
	}
	if !empty(services) {
		if in.Services, err = ParseServices(services); err != nil {
			return in, err
		}
		raw, err := parseList[struct {
			Metadata meta `json:"metadata"`
			Spec     struct {
				Selector map[string]string `json:"selector"`
			} `json:"spec"`
		}](services)
		if err != nil {
			return in, err
		}
		in.Selectors = map[string]map[string]string{}
		for _, r := range raw {
			in.Selectors[r.Metadata.Name] = r.Spec.Selector
		}
	}
	if !empty(jobs) {
		if in.Jobs, err = ParseJobs(jobs); err != nil {
			return in, err
		}
	}
	return in, nil
}

// Report reads the namespace and builds its cluster report.
func (c Client) Report(ctx context.Context) (Report, error) {
	var bs [4][]byte
	for i, kind := range []string{"pods", "deployments", "services", "jobs"} {
		b, err := c.get(ctx, kind, true)
		if err != nil {
			return Report{}, err
		}
		bs[i] = b
	}
	in, err := ParseReportInput(bs[0], bs[1], bs[2], bs[3])
	if err != nil {
		return Report{}, err
	}
	return BuildReport(in), nil
}

// podFailing: a status that means the pod does not work.
var podFailing = map[string]bool{"CrashLoopBackOff": true, "Error": true, "ImagePullBackOff": true, "ErrImagePull": true,
	"Failed": true, "OOMKilled": true, "CreateContainerConfigError": true, "InvalidImageName": true, "Evicted": true}

func podDone(p Pod) bool { return p.Phase == "Succeeded" || p.Status == "Completed" }

// podState is "ok", "warn" or "fail".
func podState(p Pod) string {
	switch {
	case podFailing[p.Status] || p.Phase == "Failed":
		return "fail"
	case podDone(p), p.Status == "Running" && p.ReadyCount >= p.Total:
		return "ok"
	}
	return "warn"
}

func deployState(d Deployment) string {
	switch {
	case d.Replicas > 0 && d.Available == 0:
		return "fail"
	case d.Available >= d.Replicas && d.ReadyN >= d.Replicas:
		return "ok"
	}
	return "warn"
}

func jobState(j Job) string {
	switch j.Status() {
	case "Complete":
		return "ok"
	case "Failed":
		return "fail"
	}
	return "warn"
}

func matches(sel, labels map[string]string) bool {
	if len(sel) == 0 {
		return false
	}
	for k, v := range sel {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// BuildReport builds the pulse, the tree and the findings.
func BuildReport(in ReportInput) Report {
	var r Report
	specs := map[string]PodSpec{}
	for _, s := range in.Specs {
		specs[s.Name] = s
	}
	pods := append([]Pod(nil), in.Pods...)
	sort.Slice(pods, func(i, j int) bool { return pods[i].Name < pods[j].Name })
	deploys := append([]Deployment(nil), in.Deployments...)
	sort.Slice(deploys, func(i, j int) bool { return deploys[i].Name < deploys[j].Name })
	services := append([]Service(nil), in.Services...)
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	jobs := append([]Job(nil), in.Jobs...)
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Name < jobs[j].Name })

	// Pulse.
	row := func(kind string, n int, state func(int) string) PulseRow {
		p := PulseRow{Kind: kind, Total: n}
		for i := 0; i < n; i++ {
			switch state(i) {
			case "ok":
				p.OK++
			case "fail":
				p.Fail++
			default:
				p.Warn++
			}
		}
		return p
	}
	selected := func(svc string) []Pod {
		var out []Pod
		for _, p := range pods {
			if matches(in.Selectors[svc], specs[p.Name].Labels) {
				out = append(out, p)
			}
		}
		return out
	}
	r.Pulse = []PulseRow{
		row("Pods", len(pods), func(i int) string { return podState(pods[i]) }),
		row("Deployments", len(deploys), func(i int) string { return deployState(deploys[i]) }),
		row("Services", len(services), func(i int) string {
			if len(in.Selectors[services[i].Name]) > 0 && len(selected(services[i].Name)) == 0 {
				return "warn"
			}
			return "ok"
		}),
		row("Jobs", len(jobs), func(i int) string { return jobState(jobs[i]) }),
	}

	// Xray.
	podNode := func(p Pod) TreeNode { return TreeNode{Kind: "Pod", Name: p.Name, Status: p.Status} }
	byOwner := map[string][]Pod{} // kind + "/" + name
	var ownerKeys []string
	for _, p := range pods {
		s := specs[p.Name]
		k := s.OwnerKind + "/" + s.OwnerName
		if _, ok := byOwner[k]; !ok {
			ownerKeys = append(ownerKeys, k)
		}
		byOwner[k] = append(byOwner[k], p)
	}
	sort.Strings(ownerKeys)
	used := map[string]bool{}
	for _, d := range deploys {
		n := TreeNode{Kind: "Deployment", Name: d.Name, Status: d.Ready()}
		for _, k := range ownerKeys {
			kind, name, _ := strings.Cut(k, "/")
			if kind != "ReplicaSet" {
				continue
			}
			if i := strings.LastIndex(name, "-"); i > 0 && name[:i] == d.Name {
				rs := TreeNode{Kind: "ReplicaSet", Name: name}
				for _, p := range byOwner[k] {
					rs.Children = append(rs.Children, podNode(p))
				}
				n.Children = append(n.Children, rs)
				used[k] = true
			}
		}
		r.Tree = append(r.Tree, n)
	}
	for _, k := range ownerKeys { // StatefulSets, DaemonSets, lone ReplicaSets …
		kind, name, _ := strings.Cut(k, "/")
		if kind == "" || kind == "Job" || used[k] {
			continue
		}
		n := TreeNode{Kind: kind, Name: name}
		for _, p := range byOwner[k] {
			n.Children = append(n.Children, podNode(p))
		}
		r.Tree = append(r.Tree, n)
	}
	for _, j := range jobs {
		n := TreeNode{Kind: "Job", Name: j.Name, Status: j.Status()}
		for _, p := range byOwner["Job/"+j.Name] {
			n.Children = append(n.Children, podNode(p))
		}
		r.Tree = append(r.Tree, n)
	}
	for _, s := range services {
		n := TreeNode{Kind: "Service", Name: s.Name, Status: s.Type}
		for _, p := range selected(s.Name) {
			n.Children = append(n.Children, podNode(p))
		}
		r.Tree = append(r.Tree, n)
	}
	for _, p := range byOwner["/"] {
		r.Tree = append(r.Tree, podNode(p))
	}

	// Sanitizer.
	add := func(level, kind, name, text string) {
		r.Findings = append(r.Findings, Finding{Level: level, Kind: kind, Name: name, Text: text})
	}
	for _, p := range pods {
		s := specs[p.Name]
		switch podState(p) {
		case "fail":
			add("error", "Pod", p.Name, p.Status)
		case "warn":
			add("warning", "Pod", p.Name, fmt.Sprintf("not ready (%s, %s)", p.Ready(), p.Status))
		}
		if p.Restarts >= 5 {
			add("warning", "Pod", p.Name, fmt.Sprintf("restarted %d times", p.Restarts))
		}
		if s.OwnerKind == "" && len(in.Specs) > 0 {
			add("warning", "Pod", p.Name, "no controller (a bare pod is not restarted on another node)")
		}
		var loose []string
		for _, img := range s.Images {
			if imageUnpinned(img) {
				loose = append(loose, img)
			}
		}
		if len(loose) > 0 {
			add("warning", "Pod", p.Name, "image without a fixed tag: "+strings.Join(loose, ", "))
		}
		if len(s.NoLimits) > 0 {
			add("warning", "Pod", p.Name, "no CPU / memory limits: "+strings.Join(s.NoLimits, ", "))
		}
		if len(s.NoProbe) > 0 && s.OwnerKind != "Job" && !podDone(p) {
			add("warning", "Pod", p.Name, "no readiness probe: "+strings.Join(s.NoProbe, ", "))
		}
	}
	for _, d := range deploys {
		switch {
		case d.Replicas > 0 && d.Available < d.Replicas:
			level := "warning"
			if d.Available == 0 {
				level = "error"
			}
			add(level, "Deployment", d.Name, fmt.Sprintf("%d of %d replicas available", d.Available, d.Replicas))
		case d.Replicas == 0:
			add("warning", "Deployment", d.Name, "scaled to 0 replicas")
		}
	}
	for _, s := range services {
		if len(in.Selectors[s.Name]) > 0 && len(selected(s.Name)) == 0 {
			add("warning", "Service", s.Name, "the selector matches no pods")
		}
	}
	for _, j := range jobs {
		if jobState(j) == "fail" {
			add("error", "Job", j.Name, fmt.Sprintf("failed (%d failed pods)", j.Failed))
		}
	}
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.Level != b.Level {
			return a.Level == "error"
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Text < b.Text
	})
	return r
}

// imageUnpinned: no tag, or :latest, and no digest.
func imageUnpinned(img string) bool {
	if img == "" || strings.Contains(img, "@") {
		return false
	}
	last := img[strings.LastIndex(img, "/")+1:]
	_, tag, ok := strings.Cut(last, ":")
	return !ok || tag == "latest"
}

// Text is the report as plain lines for a read-only page.
func (r Report) Text() string {
	var b strings.Builder
	b.WriteString("Pulse\n")
	for _, p := range r.Pulse {
		fmt.Fprintf(&b, "  %-12s %4d  ok %d · warn %d · fail %d\n", p.Kind, p.Total, p.OK, p.Warn, p.Fail)
	}
	b.WriteString("\nXray\n")
	var walk func(ns []TreeNode, depth int)
	walk = func(ns []TreeNode, depth int) {
		for _, n := range ns {
			line := strings.Repeat("  ", depth+1) + n.Kind + " " + n.Name
			if n.Status != "" {
				line += "  " + n.Status
			}
			b.WriteString(line + "\n")
			walk(n.Children, depth+1)
		}
	}
	walk(r.Tree, 0)
	if len(r.Tree) == 0 {
		b.WriteString("  (nothing in this namespace)\n")
	}
	errs, warns := 0, 0
	for _, f := range r.Findings {
		if f.Level == "error" {
			errs++
		} else {
			warns++
		}
	}
	if len(r.Findings) == 0 {
		b.WriteString("\nReport: no problems found\n")
		return b.String()
	}
	fmt.Fprintf(&b, "\nReport: %s, %s\n", plural(errs, "error"), plural(warns, "warning"))
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "  %-6s %s %s: %s\n", f.Level, f.Kind, f.Name, f.Text)
	}
	return b.String()
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
