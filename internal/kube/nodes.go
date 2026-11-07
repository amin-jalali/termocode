package kube

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// drainTimeout is kubectl's own --timeout for a drain; the call gets a
// little more so kubectl reports its own error first.
const drainTimeout = 240 * time.Second

func nodeName(name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return errors.New("invalid node name")
	}
	return nil
}

func (c Client) nodeCall(ctx context.Context, d time.Duration, args ...string) error {
	if err := nodeName(args[1]); err != nil {
		return err
	}
	if c.Timeout < d {
		c.Timeout = d
	}
	_, err := c.capture(ctx, append(args, c.scope(false)...)...)
	return err
}

// Cordon is `kubectl cordon <node>`: a write; the app asks first and the
// AI never runs it.
func (c Client) Cordon(ctx context.Context, node string) error {
	return c.nodeCall(ctx, restartTimeout, "cordon", node)
}

// Uncordon is `kubectl uncordon <node>` (a write; asked first).
func (c Client) Uncordon(ctx context.Context, node string) error {
	return c.nodeCall(ctx, restartTimeout, "uncordon", node)
}

// Drain is `kubectl drain <node>`: cordons and evicts its pods (a write;
// asked first). DaemonSet pods stay; emptyDir data is lost, which the
// card must say.
func (c Client) Drain(ctx context.Context, node string) error {
	return c.nodeCall(ctx, drainTimeout+30*time.Second, "drain", node, "--ignore-daemonsets", "--delete-emptydir-data",
		fmt.Sprintf("--timeout=%ds", int(drainTimeout/time.Second)))
}

// HPA is one row of `kubectl get horizontalpodautoscalers`.
type HPA struct {
	Name       string
	Namespace  string
	Created    time.Time
	TargetKind string
	TargetName string
	Min, Max   int
	Current    int
	Desired    int
	Targets    string // "cpu 91%/80%, memory <unknown>/70%", as kubectl shows it
}

// Reference is "Kind/name" of the scaled object.
func (h HPA) Reference() string { return h.TargetKind + "/" + h.TargetName }

type rawHPA struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		ScaleTargetRef struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"scaleTargetRef"`
		MinReplicas *int `json:"minReplicas"`
		MaxReplicas int  `json:"maxReplicas"`
		Metrics     []struct {
			Type     string `json:"type"`
			Resource *struct {
				Name   string `json:"name"`
				Target struct {
					AverageUtilization *int `json:"averageUtilization"`
				} `json:"target"`
			} `json:"resource"`
		} `json:"metrics"`
	} `json:"spec"`
	Status struct {
		CurrentReplicas int `json:"currentReplicas"`
		DesiredReplicas int `json:"desiredReplicas"`
		CurrentMetrics  []struct {
			Resource *struct {
				Name    string `json:"name"`
				Current struct {
					AverageUtilization *int `json:"averageUtilization"`
				} `json:"current"`
			} `json:"resource"`
		} `json:"currentMetrics"`
	} `json:"status"`
}

// ParseHPAs reads `kubectl get horizontalpodautoscalers -o json`.
func ParseHPAs(b []byte) ([]HPA, error) {
	raw, err := parseList[rawHPA](b)
	if err != nil {
		return nil, err
	}
	out := make([]HPA, 0, len(raw))
	for _, r := range raw {
		lo := 1 // the API's default
		if r.Spec.MinReplicas != nil {
			lo = *r.Spec.MinReplicas
		}
		current := map[string]int{}
		for _, m := range r.Status.CurrentMetrics {
			if m.Resource != nil && m.Resource.Current.AverageUtilization != nil {
				current[m.Resource.Name] = *m.Resource.Current.AverageUtilization
			}
		}
		var targets []string
		for _, m := range r.Spec.Metrics {
			if m.Resource == nil || m.Resource.Target.AverageUtilization == nil {
				targets = append(targets, m.Type)
				continue
			}
			now := "<unknown>"
			if v, ok := current[m.Resource.Name]; ok {
				now = fmt.Sprintf("%d%%", v)
			}
			targets = append(targets, fmt.Sprintf("%s %s/%d%%", m.Resource.Name, now, *m.Resource.Target.AverageUtilization))
		}
		out = append(out, HPA{Name: r.Metadata.Name, Namespace: r.Metadata.Namespace, Created: r.Metadata.CreationTimestamp,
			TargetKind: r.Spec.ScaleTargetRef.Kind, TargetName: r.Spec.ScaleTargetRef.Name, Min: lo, Max: r.Spec.MaxReplicas,
			Current: r.Status.CurrentReplicas, Desired: r.Status.DesiredReplicas, Targets: strings.Join(targets, ", ")})
	}
	return out, nil
}

// HPAs lists the namespace's horizontal pod autoscalers (read only).
func (c Client) HPAs(ctx context.Context) ([]HPA, error) {
	b, err := c.get(ctx, "horizontalpodautoscalers", true)
	if err != nil {
		return nil, err
	}
	return ParseHPAs(b)
}
