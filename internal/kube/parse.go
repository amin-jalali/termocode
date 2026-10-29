// Package kube reads a Kubernetes cluster through the kubectl CLI.
//
// Rules:
//   - kubectl is the engine: no Go client library. Every call goes through
//     services.Exec (internal/spawn underneath) with a timeout and
//     `-o json`; the answer is parsed here.
//   - The session's context and namespace are passed as flags
//     (--context, --namespace). kubeconfig is only read, never written:
//     no `config use-context`, no `config set-context`. KUBECONFIG is
//     respected because kubectl reads it from the environment.
//   - Errors are one quiet line (ErrorLine): no kubectl, no cluster,
//     forbidden, a timeout.
//   - The only write is Client.RolloutRestart (the app asks first).
package kube

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Pod is one row of `kubectl get pods`.
type Pod struct {
	Name       string
	Namespace  string
	Created    time.Time
	Status     string // what kubectl's STATUS column says: Running, CrashLoopBackOff, …
	Phase      string
	ReadyCount int
	Total      int
	Restarts   int
	Containers []string // spec order
	Ports      []int    // declared container ports, spec order
}

// Ready is "ready/total", as kubectl shows it.
func (p Pod) Ready() string { return fmt.Sprintf("%d/%d", p.ReadyCount, p.Total) }

// Deployment is one row of `kubectl get deployments`.
type Deployment struct {
	Name      string
	Namespace string
	Created   time.Time
	Replicas  int // wanted (spec)
	ReadyN    int
	Updated   int
	Available int
}

// Ready is "ready/wanted".
func (d Deployment) Ready() string { return fmt.Sprintf("%d/%d", d.ReadyN, d.Replicas) }

// Service is one row of `kubectl get services`.
type Service struct {
	Name      string
	Namespace string
	Created   time.Time
	Type      string
	ClusterIP string
	Ports     []ServicePort
}

// ServicePort is one port of a Service.
type ServicePort struct {
	Port     int
	NodePort int
	Protocol string
}

// PortsText is kubectl's PORT(S) column: "80:30080/TCP,443/TCP".
func (s Service) PortsText() string {
	parts := make([]string, 0, len(s.Ports))
	for _, p := range s.Ports {
		v := strconv.Itoa(p.Port)
		if p.NodePort > 0 {
			v += ":" + strconv.Itoa(p.NodePort)
		}
		proto := p.Protocol
		if proto == "" {
			proto = "TCP"
		}
		parts = append(parts, v+"/"+proto)
	}
	return strings.Join(parts, ",")
}

// Job is one row of `kubectl get jobs`.
type Job struct {
	Name      string
	Namespace string
	Created   time.Time
	Wanted    int
	Succeeded int
	Failed    int
	Active    int
	failedCnd bool
}

// Completions is "succeeded/wanted".
func (j Job) Completions() string { return fmt.Sprintf("%d/%d", j.Succeeded, j.Wanted) }

// Status is Complete, Failed, Running or Pending.
func (j Job) Status() string {
	switch {
	case j.Wanted > 0 && j.Succeeded >= j.Wanted:
		return "Complete"
	case j.failedCnd:
		return "Failed"
	case j.Active > 0:
		return "Running"
	}
	return "Pending"
}

type meta struct {
	Name              string    `json:"name"`
	Namespace         string    `json:"namespace"`
	CreationTimestamp time.Time `json:"creationTimestamp"`
	DeletionTimestamp *string   `json:"deletionTimestamp"`
}

type list[T any] struct {
	Items []T `json:"items"`
}

func parseList[T any](b []byte) ([]T, error) {
	var l list[T]
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("cannot read kubectl output: %w", err)
	}
	return l.Items, nil
}

type rawPod struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		Containers []struct {
			Name  string `json:"name"`
			Ports []struct {
				ContainerPort int `json:"containerPort"`
			} `json:"ports"`
		} `json:"containers"`
	} `json:"spec"`
	Status struct {
		Phase             string `json:"phase"`
		Reason            string `json:"reason"`
		ContainerStatuses []struct {
			Name         string `json:"name"`
			Ready        bool   `json:"ready"`
			RestartCount int    `json:"restartCount"`
			State        struct {
				Waiting *struct {
					Reason string `json:"reason"`
				} `json:"waiting"`
				Terminated *struct {
					Reason   string `json:"reason"`
					ExitCode int    `json:"exitCode"`
				} `json:"terminated"`
			} `json:"state"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

// ParsePods reads `kubectl get pods -o json`.
func ParsePods(b []byte) ([]Pod, error) {
	raw, err := parseList[rawPod](b)
	if err != nil {
		return nil, err
	}
	out := make([]Pod, 0, len(raw))
	for _, r := range raw {
		p := Pod{Name: r.Metadata.Name, Namespace: r.Metadata.Namespace, Created: r.Metadata.CreationTimestamp,
			Phase: r.Status.Phase, Total: len(r.Spec.Containers)}
		for _, c := range r.Spec.Containers {
			p.Containers = append(p.Containers, c.Name)
			for _, port := range c.Ports {
				if port.ContainerPort > 0 {
					p.Ports = append(p.Ports, port.ContainerPort)
				}
			}
		}
		status := r.Status.Phase
		if r.Status.Reason != "" {
			status = r.Status.Reason
		}
		for _, cs := range r.Status.ContainerStatuses {
			if cs.Ready {
				p.ReadyCount++
			}
			p.Restarts += cs.RestartCount
			switch {
			case cs.State.Waiting != nil && cs.State.Waiting.Reason != "":
				status = cs.State.Waiting.Reason
			case cs.State.Terminated != nil && cs.State.Terminated.Reason != "":
				status = cs.State.Terminated.Reason
			}
		}
		if r.Metadata.DeletionTimestamp != nil {
			status = "Terminating"
		}
		if status == "" {
			status = "Unknown"
		}
		p.Status = status
		out = append(out, p)
	}
	return out, nil
}

type rawDeployment struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		Replicas *int `json:"replicas"`
	} `json:"spec"`
	Status struct {
		ReadyReplicas     int `json:"readyReplicas"`
		UpdatedReplicas   int `json:"updatedReplicas"`
		AvailableReplicas int `json:"availableReplicas"`
	} `json:"status"`
}

// ParseDeployments reads `kubectl get deployments -o json`.
func ParseDeployments(b []byte) ([]Deployment, error) {
	raw, err := parseList[rawDeployment](b)
	if err != nil {
		return nil, err
	}
	out := make([]Deployment, 0, len(raw))
	for _, r := range raw {
		want := 1 // the API's default
		if r.Spec.Replicas != nil {
			want = *r.Spec.Replicas
		}
		out = append(out, Deployment{Name: r.Metadata.Name, Namespace: r.Metadata.Namespace, Created: r.Metadata.CreationTimestamp,
			Replicas: want, ReadyN: r.Status.ReadyReplicas, Updated: r.Status.UpdatedReplicas, Available: r.Status.AvailableReplicas})
	}
	return out, nil
}

type rawService struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		Type      string `json:"type"`
		ClusterIP string `json:"clusterIP"`
		Ports     []struct {
			Port     int    `json:"port"`
			NodePort int    `json:"nodePort"`
			Protocol string `json:"protocol"`
		} `json:"ports"`
	} `json:"spec"`
}

// ParseServices reads `kubectl get services -o json`.
func ParseServices(b []byte) ([]Service, error) {
	raw, err := parseList[rawService](b)
	if err != nil {
		return nil, err
	}
	out := make([]Service, 0, len(raw))
	for _, r := range raw {
		s := Service{Name: r.Metadata.Name, Namespace: r.Metadata.Namespace, Created: r.Metadata.CreationTimestamp,
			Type: r.Spec.Type, ClusterIP: r.Spec.ClusterIP}
		for _, p := range r.Spec.Ports {
			s.Ports = append(s.Ports, ServicePort{Port: p.Port, NodePort: p.NodePort, Protocol: p.Protocol})
		}
		out = append(out, s)
	}
	return out, nil
}

type rawJob struct {
	Metadata meta `json:"metadata"`
	Spec     struct {
		Completions *int `json:"completions"`
	} `json:"spec"`
	Status struct {
		Succeeded  int `json:"succeeded"`
		Failed     int `json:"failed"`
		Active     int `json:"active"`
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
	} `json:"status"`
}

// ParseJobs reads `kubectl get jobs -o json`.
func ParseJobs(b []byte) ([]Job, error) {
	raw, err := parseList[rawJob](b)
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(raw))
	for _, r := range raw {
		want := 1
		if r.Spec.Completions != nil {
			want = *r.Spec.Completions
		}
		j := Job{Name: r.Metadata.Name, Namespace: r.Metadata.Namespace, Created: r.Metadata.CreationTimestamp,
			Wanted: want, Succeeded: r.Status.Succeeded, Failed: r.Status.Failed, Active: r.Status.Active}
		for _, c := range r.Status.Conditions {
			if c.Type == "Failed" && c.Status == "True" {
				j.failedCnd = true
			}
		}
		out = append(out, j)
	}
	return out, nil
}

// ParseNamespaces reads `kubectl get namespaces -o json` (names only).
func ParseNamespaces(b []byte) ([]string, error) {
	raw, err := parseList[struct {
		Metadata meta `json:"metadata"`
	}](b)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		out = append(out, r.Metadata.Name)
	}
	return out, nil
}

// Age is kubectl's short age: "40s", "12m", "3h", "2d" ("" for no time).
func Age(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < 0:
		return "0s"
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d"
}
