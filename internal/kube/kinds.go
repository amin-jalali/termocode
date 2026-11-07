package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// StatefulSet is one row of `kubectl get statefulsets`.
type StatefulSet struct {
	Name      string
	Namespace string
	Created   time.Time
	Replicas  int // wanted (spec)
	ReadyN    int
}

// Ready is "ready/wanted".
func (s StatefulSet) Ready() string { return fmt.Sprintf("%d/%d", s.ReadyN, s.Replicas) }

// DaemonSet is one row of `kubectl get daemonsets`.
type DaemonSet struct {
	Name      string
	Namespace string
	Created   time.Time
	Desired   int
	Current   int
	ReadyN    int
	Updated   int
	Available int
}

// Ready is "ready/desired".
func (d DaemonSet) Ready() string { return fmt.Sprintf("%d/%d", d.ReadyN, d.Desired) }

// ReplicaSet is one row of `kubectl get replicasets`.
type ReplicaSet struct {
	Name      string
	Namespace string
	Created   time.Time
	Desired   int
	Current   int
	ReadyN    int
	Owner     string // "Deployment/api" ("" = none)
}

// Ready is "ready/desired".
func (r ReplicaSet) Ready() string { return fmt.Sprintf("%d/%d", r.ReadyN, r.Desired) }

// CronJob is one row of `kubectl get cronjobs`.
type CronJob struct {
	Name         string
	Namespace    string
	Created      time.Time
	Schedule     string
	Suspend      bool
	Active       int
	LastSchedule time.Time // zero = never ran
}

// ConfigMap is one row of `kubectl get configmaps`: names of its keys
// (data and binaryData, sorted), never the values.
type ConfigMap struct {
	Name      string
	Namespace string
	Created   time.Time
	Keys      []string
}

// Secret is one row of `kubectl get secrets`: its type and key names.
// The values are never read: kubectl prints only names (secretsTemplate).
type Secret struct {
	Name      string
	Namespace string
	Created   time.Time
	Type      string
	Keys      []string
}

// Ingress is one row of `kubectl get ingresses`.
type Ingress struct {
	Name      string
	Namespace string
	Created   time.Time
	Class     string
	Hosts     []string // rule hosts ("" = any host)
	Address   []string // load balancer IPs / host names
	TLS       bool
}

// HostsText is kubectl's HOSTS column ("*" for a rule with no host).
func (i Ingress) HostsText() string {
	hs := make([]string, 0, len(i.Hosts))
	for _, h := range i.Hosts {
		if h == "" {
			h = "*"
		}
		hs = append(hs, h)
	}
	if len(hs) == 0 {
		return "*"
	}
	return strings.Join(hs, ",")
}

// PortsText is kubectl's PORTS column: "80" or "80, 443" with TLS.
func (i Ingress) PortsText() string {
	if i.TLS {
		return "80, 443"
	}
	return "80"
}

// PVC is one row of `kubectl get persistentvolumeclaims`.
type PVC struct {
	Name         string
	Namespace    string
	Created      time.Time
	Status       string // Bound, Pending, Lost
	Volume       string
	Capacity     string // "10Gi" ("" until bound)
	AccessModes  []string
	StorageClass string
}

var accessShort = map[string]string{
	"ReadWriteOnce": "RWO", "ReadOnlyMany": "ROX", "ReadWriteMany": "RWX", "ReadWriteOncePod": "RWOP",
}

// AccessText is kubectl's ACCESS MODES column: "RWO", "RWX,ROX".
func (p PVC) AccessText() string {
	out := make([]string, 0, len(p.AccessModes))
	for _, m := range p.AccessModes {
		if s, ok := accessShort[m]; ok {
			m = s
		}
		out = append(out, m)
	}
	return strings.Join(out, ",")
}

// Node is one row of `kubectl get nodes` (cluster scoped).
type Node struct {
	Name    string
	Created time.Time
	Status  string // Ready, NotReady, plus ",SchedulingDisabled" when cordoned
	Roles   []string
	Version string
}

// RolesText is kubectl's ROLES column ("<none>" for no role).
func (n Node) RolesText() string {
	if len(n.Roles) == 0 {
		return "<none>"
	}
	return strings.Join(n.Roles, ",")
}

type rawStatefulSet struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		Replicas *int `json:"replicas"`
	} `json:"spec"`
	Status struct {
		ReadyReplicas int `json:"readyReplicas"`
	} `json:"status"`
}

// ParseStatefulSets reads `kubectl get statefulsets -o json`.
func ParseStatefulSets(b []byte) ([]StatefulSet, error) {
	raw, err := parseList[rawStatefulSet](b)
	if err != nil {
		return nil, err
	}
	out := make([]StatefulSet, 0, len(raw))
	for _, r := range raw {
		want := 1 // the API's default
		if r.Spec.Replicas != nil {
			want = *r.Spec.Replicas
		}
		out = append(out, StatefulSet{Name: r.Metadata.Name, Namespace: r.Metadata.Namespace, Created: r.Metadata.CreationTimestamp,
			Replicas: want, ReadyN: r.Status.ReadyReplicas})
	}
	return out, nil
}

type rawDaemonSet struct {
	Metadata meta `json:"metadata"`
	Status   struct {
		Desired   int `json:"desiredNumberScheduled"`
		Current   int `json:"currentNumberScheduled"`
		Ready     int `json:"numberReady"`
		Updated   int `json:"updatedNumberScheduled"`
		Available int `json:"numberAvailable"`
	} `json:"status"`
}

// ParseDaemonSets reads `kubectl get daemonsets -o json`.
func ParseDaemonSets(b []byte) ([]DaemonSet, error) {
	raw, err := parseList[rawDaemonSet](b)
	if err != nil {
		return nil, err
	}
	out := make([]DaemonSet, 0, len(raw))
	for _, r := range raw {
		s := r.Status
		out = append(out, DaemonSet{Name: r.Metadata.Name, Namespace: r.Metadata.Namespace, Created: r.Metadata.CreationTimestamp,
			Desired: s.Desired, Current: s.Current, ReadyN: s.Ready, Updated: s.Updated, Available: s.Available})
	}
	return out, nil
}

type rawReplicaSet struct {
	Metadata struct {
		meta
		OwnerReferences []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		Replicas *int `json:"replicas"`
	} `json:"spec"`
	Status struct {
		Replicas      int `json:"replicas"`
		ReadyReplicas int `json:"readyReplicas"`
	} `json:"status"`
}

// ParseReplicaSets reads `kubectl get replicasets -o json`.
func ParseReplicaSets(b []byte) ([]ReplicaSet, error) {
	raw, err := parseList[rawReplicaSet](b)
	if err != nil {
		return nil, err
	}
	out := make([]ReplicaSet, 0, len(raw))
	for _, r := range raw {
		want := 1
		if r.Spec.Replicas != nil {
			want = *r.Spec.Replicas
		}
		rs := ReplicaSet{Name: r.Metadata.Name, Namespace: r.Metadata.Namespace, Created: r.Metadata.CreationTimestamp,
			Desired: want, Current: r.Status.Replicas, ReadyN: r.Status.ReadyReplicas}
		if len(r.Metadata.OwnerReferences) > 0 {
			o := r.Metadata.OwnerReferences[0]
			rs.Owner = o.Kind + "/" + o.Name
		}
		out = append(out, rs)
	}
	return out, nil
}

type rawCronJob struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		Schedule string `json:"schedule"`
		Suspend  *bool  `json:"suspend"`
	} `json:"spec"`
	Status struct {
		Active           []struct{} `json:"active"`
		LastScheduleTime *time.Time `json:"lastScheduleTime"`
	} `json:"status"`
}

// ParseCronJobs reads `kubectl get cronjobs -o json`.
func ParseCronJobs(b []byte) ([]CronJob, error) {
	raw, err := parseList[rawCronJob](b)
	if err != nil {
		return nil, err
	}
	out := make([]CronJob, 0, len(raw))
	for _, r := range raw {
		c := CronJob{Name: r.Metadata.Name, Namespace: r.Metadata.Namespace, Created: r.Metadata.CreationTimestamp,
			Schedule: r.Spec.Schedule, Suspend: r.Spec.Suspend != nil && *r.Spec.Suspend, Active: len(r.Status.Active)}
		if r.Status.LastScheduleTime != nil {
			c.LastSchedule = *r.Status.LastScheduleTime
		}
		out = append(out, c)
	}
	return out, nil
}

// skipValue reads any JSON value and keeps nothing (config map values).
type skipValue struct{}

func (*skipValue) UnmarshalJSON([]byte) error { return nil }

type rawConfigMap struct {
	Metadata   meta                 `json:"metadata"`
	Data       map[string]skipValue `json:"data"`
	BinaryData map[string]skipValue `json:"binaryData"`
}

// ParseConfigMaps reads `kubectl get configmaps -o json`: key names only.
func ParseConfigMaps(b []byte) ([]ConfigMap, error) {
	raw, err := parseList[rawConfigMap](b)
	if err != nil {
		return nil, err
	}
	out := make([]ConfigMap, 0, len(raw))
	for _, r := range raw {
		var keys []string
		for k := range r.Data {
			keys = append(keys, k)
		}
		for k := range r.BinaryData {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out = append(out, ConfigMap{Name: r.Metadata.Name, Namespace: r.Metadata.Namespace, Created: r.Metadata.CreationTimestamp, Keys: keys})
	}
	return out, nil
}

// secretsTemplate makes kubectl print one line per secret: name, type,
// creation time and the key names ("a,b,"), tab separated. The values
// never leave kubectl.
const secretsTemplate = `{{range .items}}{{.metadata.name}}{{"\t"}}{{.type}}{{"\t"}}{{.metadata.creationTimestamp}}{{"\t"}}{{range $k, $_ := .data}}{{$k}},{{end}}{{"\n"}}{{end}}`

// ParseSecrets reads the secretsTemplate output.
func ParseSecrets(b []byte) ([]Secret, error) {
	var out []Secret
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		f := strings.Split(l, "\t")
		if len(f) != 4 {
			return nil, fmt.Errorf("cannot read kubectl output: %d fields in a secrets line", len(f))
		}
		s := Secret{Name: f[0], Type: f[1]}
		if t, err := time.Parse(time.RFC3339, f[2]); err == nil {
			s.Created = t
		}
		for _, k := range strings.Split(f[3], ",") {
			if k != "" {
				s.Keys = append(s.Keys, k)
			}
		}
		out = append(out, s)
	}
	return out, nil
}

type rawIngress struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		Class *string `json:"ingressClassName"`
		Rules []struct {
			Host string `json:"host"`
		} `json:"rules"`
		TLS []struct{} `json:"tls"`
	} `json:"spec"`
	Status struct {
		LoadBalancer struct {
			Ingress []struct {
				IP       string `json:"ip"`
				Hostname string `json:"hostname"`
			} `json:"ingress"`
		} `json:"loadBalancer"`
	} `json:"status"`
}

// ParseIngresses reads `kubectl get ingresses -o json`.
func ParseIngresses(b []byte) ([]Ingress, error) {
	raw, err := parseList[rawIngress](b)
	if err != nil {
		return nil, err
	}
	out := make([]Ingress, 0, len(raw))
	for _, r := range raw {
		in := Ingress{Name: r.Metadata.Name, Namespace: r.Metadata.Namespace, Created: r.Metadata.CreationTimestamp, TLS: len(r.Spec.TLS) > 0}
		if r.Spec.Class != nil {
			in.Class = *r.Spec.Class
		}
		for _, rule := range r.Spec.Rules {
			in.Hosts = append(in.Hosts, rule.Host)
		}
		for _, lb := range r.Status.LoadBalancer.Ingress {
			if lb.IP != "" {
				in.Address = append(in.Address, lb.IP)
			} else if lb.Hostname != "" {
				in.Address = append(in.Address, lb.Hostname)
			}
		}
		out = append(out, in)
	}
	return out, nil
}

type rawPVC struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		AccessModes  []string `json:"accessModes"`
		StorageClass *string  `json:"storageClassName"`
		VolumeName   string   `json:"volumeName"`
	} `json:"spec"`
	Status struct {
		Phase       string            `json:"phase"`
		Capacity    map[string]string `json:"capacity"`
		AccessModes []string          `json:"accessModes"`
	} `json:"status"`
}

// ParsePVCs reads `kubectl get persistentvolumeclaims -o json`.
func ParsePVCs(b []byte) ([]PVC, error) {
	raw, err := parseList[rawPVC](b)
	if err != nil {
		return nil, err
	}
	out := make([]PVC, 0, len(raw))
	for _, r := range raw {
		p := PVC{Name: r.Metadata.Name, Namespace: r.Metadata.Namespace, Created: r.Metadata.CreationTimestamp,
			Status: r.Status.Phase, Volume: r.Spec.VolumeName, Capacity: r.Status.Capacity["storage"], AccessModes: r.Status.AccessModes}
		if len(p.AccessModes) == 0 {
			p.AccessModes = r.Spec.AccessModes
		}
		if r.Spec.StorageClass != nil {
			p.StorageClass = *r.Spec.StorageClass
		}
		if r.Metadata.DeletionTimestamp != nil {
			p.Status = "Terminating"
		}
		out = append(out, p)
	}
	return out, nil
}

type rawNode struct {
	Metadata struct {
		meta
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec struct {
		Unschedulable bool `json:"unschedulable"`
	} `json:"spec"`
	Status struct {
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
		NodeInfo struct {
			KubeletVersion string `json:"kubeletVersion"`
		} `json:"nodeInfo"`
	} `json:"status"`
}

const nodeRolePrefix = "node-role.kubernetes.io/"

// ParseNodes reads `kubectl get nodes -o json`.
func ParseNodes(b []byte) ([]Node, error) {
	raw, err := parseList[rawNode](b)
	if err != nil {
		return nil, err
	}
	out := make([]Node, 0, len(raw))
	for _, r := range raw {
		n := Node{Name: r.Metadata.Name, Created: r.Metadata.CreationTimestamp, Version: r.Status.NodeInfo.KubeletVersion, Status: "NotReady"}
		for _, c := range r.Status.Conditions {
			if c.Type == "Ready" && c.Status == "True" {
				n.Status = "Ready"
			}
		}
		if r.Spec.Unschedulable {
			n.Status += ",SchedulingDisabled"
		}
		for k := range r.Metadata.Labels {
			if role, ok := strings.CutPrefix(k, nodeRolePrefix); ok && role != "" {
				n.Roles = append(n.Roles, role)
			}
		}
		sort.Strings(n.Roles)
		out = append(out, n)
	}
	return out, nil
}

// StatefulSets lists the namespace's statefulsets.
func (c Client) StatefulSets(ctx context.Context) ([]StatefulSet, error) {
	b, err := c.get(ctx, "statefulsets", true)
	if err != nil {
		return nil, err
	}
	return ParseStatefulSets(b)
}

// DaemonSets lists the namespace's daemonsets.
func (c Client) DaemonSets(ctx context.Context) ([]DaemonSet, error) {
	b, err := c.get(ctx, "daemonsets", true)
	if err != nil {
		return nil, err
	}
	return ParseDaemonSets(b)
}

// ReplicaSets lists the namespace's replicasets.
func (c Client) ReplicaSets(ctx context.Context) ([]ReplicaSet, error) {
	b, err := c.get(ctx, "replicasets", true)
	if err != nil {
		return nil, err
	}
	return ParseReplicaSets(b)
}

// CronJobs lists the namespace's cronjobs.
func (c Client) CronJobs(ctx context.Context) ([]CronJob, error) {
	b, err := c.get(ctx, "cronjobs", true)
	if err != nil {
		return nil, err
	}
	return ParseCronJobs(b)
}

// ConfigMaps lists the namespace's config maps (key names only).
func (c Client) ConfigMaps(ctx context.Context) ([]ConfigMap, error) {
	b, err := c.get(ctx, "configmaps", true)
	if err != nil {
		return nil, err
	}
	return ParseConfigMaps(b)
}

// Secrets lists the namespace's secrets: names, types and key names. The
// values never leave kubectl (secretsTemplate).
func (c Client) Secrets(ctx context.Context) ([]Secret, error) {
	args := append([]string{"get", "secrets", "-o", "go-template=" + secretsTemplate}, c.scope(true)...)
	out, err := c.capture(ctx, args...)
	if err != nil {
		return nil, err
	}
	ss, err := ParseSecrets([]byte(out))
	if err != nil {
		return nil, err
	}
	for i := range ss {
		ss[i].Namespace = c.Namespace
	}
	return ss, nil
}

// Ingresses lists the namespace's ingresses.
func (c Client) Ingresses(ctx context.Context) ([]Ingress, error) {
	b, err := c.get(ctx, "ingresses", true)
	if err != nil {
		return nil, err
	}
	return ParseIngresses(b)
}

// PVCs lists the namespace's persistent volume claims.
func (c Client) PVCs(ctx context.Context) ([]PVC, error) {
	b, err := c.get(ctx, "persistentvolumeclaims", true)
	if err != nil {
		return nil, err
	}
	return ParsePVCs(b)
}

// Nodes lists the cluster's nodes (cluster scoped: no --namespace).
func (c Client) Nodes(ctx context.Context) ([]Node, error) {
	b, err := c.get(ctx, "nodes", false)
	if err != nil {
		return nil, err
	}
	return ParseNodes(b)
}
