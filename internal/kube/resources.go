package kube

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// APIResource is one kind the cluster serves (kubectl api-resources),
// built-in or from a CRD (CTX-20).
type APIResource struct {
	Name       string   // plural: "certificates"
	ShortNames []string // "cert", "certs"
	Group      string   // "" = the core group
	Version    string
	Namespaced bool
	Kind       string // "Certificate"
}

// Ref is the name kubectl get takes: "pods", "deployments.apps".
func (r APIResource) Ref() string {
	if r.Group == "" {
		return r.Name
	}
	return r.Name + "." + r.Group
}

// Custom says whether the kind comes from a CRD (its group is not one
// of the built-in groups).
func (r APIResource) Custom() bool {
	switch r.Group {
	case "", "apps", "batch", "autoscaling", "policy", "extensions":
		return false
	}
	return !strings.HasSuffix(r.Group, ".k8s.io")
}

// ParseAPIResources reads the table of `kubectl api-resources` by its
// header columns (NAME SHORTNAMES APIVERSION|APIGROUP NAMESPACED KIND).
func ParseAPIResources(out string) ([]APIResource, error) {
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "NAME") {
		return nil, fmt.Errorf("cannot read kubectl api-resources output")
	}
	head := lines[0]
	type col struct {
		name  string
		start int
	}
	var cols []col
	for i := 0; i < len(head); {
		if head[i] == ' ' {
			i++
			continue
		}
		j := i
		for j < len(head) && head[j] != ' ' {
			j++
		}
		cols = append(cols, col{head[i:j], i})
		i = j
	}
	field := func(l string, k int) string {
		s := cols[k].start
		if s >= len(l) {
			return ""
		}
		e := len(l)
		if k+1 < len(cols) && cols[k+1].start < e {
			e = cols[k+1].start
		}
		return strings.TrimSpace(l[s:e])
	}
	var out2 []APIResource
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		var r APIResource
		for k, c := range cols {
			v := field(l, k)
			switch c.name {
			case "NAME":
				r.Name = v
			case "SHORTNAMES":
				if v != "" {
					r.ShortNames = strings.Split(v, ",")
				}
			case "APIVERSION":
				if g, ver, ok := strings.Cut(v, "/"); ok {
					r.Group, r.Version = g, ver
				} else {
					r.Version = v
				}
			case "APIGROUP":
				r.Group = v
			case "NAMESPACED":
				r.Namespaced = v == "true"
			case "KIND":
				r.Kind = v
			}
		}
		if r.Name != "" {
			out2 = append(out2, r)
		}
	}
	return out2, nil
}

// ResolveKind finds the kind a user typed: plural, singular, short
// name, Kind, name.group or kind.group, or a user alias for one of
// those (one step). The first match in kubectl's order wins (core first).
func ResolveKind(rs []APIResource, aliases map[string]string, q string) (APIResource, bool) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return APIResource{}, false
	}
	if a, ok := aliases[q]; ok {
		q = strings.ToLower(strings.TrimSpace(a))
	}
	for _, r := range rs {
		kind := strings.ToLower(r.Kind)
		names := append([]string{r.Name, kind}, r.ShortNames...)
		for _, n := range names {
			if n == q {
				return r, true
			}
		}
		if r.Group != "" && (q == r.Name+"."+r.Group || q == kind+"."+r.Group) {
			return r, true
		}
	}
	return APIResource{}, false
}

// APIResources lists the kinds the cluster can list (CRDs included).
func (c Client) APIResources(ctx context.Context) ([]APIResource, error) {
	args := append([]string{"api-resources", "--verbs=list"}, c.scope(false)...)
	out, err := c.capture(ctx, args...)
	if err != nil {
		return nil, err
	}
	return ParseAPIResources(out)
}

// Object is one row of any kind: name, namespace, age and a short status.
type Object struct {
	Name      string
	Namespace string
	Created   time.Time
	Status    string // status.phase, else the Ready condition, else ""
}

type rawObject struct {
	Metadata meta `json:"metadata"`
	Status   struct {
		Phase      string `json:"phase"`
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
	} `json:"status"`
}

// ParseObjects reads `kubectl get <any kind> -o json`.
func ParseObjects(b []byte) ([]Object, error) {
	raw, err := parseList[rawObject](b)
	if err != nil {
		return nil, err
	}
	out := make([]Object, 0, len(raw))
	for _, r := range raw {
		o := Object{Name: r.Metadata.Name, Namespace: r.Metadata.Namespace, Created: r.Metadata.CreationTimestamp, Status: r.Status.Phase}
		if o.Status == "" {
			for _, c := range r.Status.Conditions {
				if c.Type == "Ready" {
					o.Status = map[bool]string{true: "Ready", false: "Not ready"}[c.Status == "True"]
				}
			}
		}
		out = append(out, o)
	}
	return out, nil
}

// Objects lists the objects of any kind in the session namespace (or
// all namespaces); a cluster kind never gets a namespace.
func (c Client) Objects(ctx context.Context, r APIResource) ([]Object, error) {
	b, err := c.get(ctx, r.Ref(), r.Namespaced)
	if err != nil {
		return nil, err
	}
	return ParseObjects(b)
}
