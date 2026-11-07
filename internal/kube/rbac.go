package kube

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// Permission is one row of `kubectl auth can-i --list`: what the
// session's user may do in the namespace.
type Permission struct {
	Resource string   // "pods", "deployments.apps" ("" for a URL row)
	URLs     []string // non-resource URLs such as /healthz
	Names    []string // limited to these resource names (empty: all)
	Verbs    []string // get, list, …; "*" is every verb
}

// Allows reports whether verb is in the row (or the row has "*").
func (p Permission) Allows(verb string) bool {
	for _, v := range p.Verbs {
		if v == "*" || v == verb {
			return true
		}
	}
	return false
}

// RoleBinding is one row of `kubectl get rolebindings`.
type RoleBinding struct {
	Name      string
	Namespace string
	Role      string   // "Role/admin" or "ClusterRole/view"
	Subjects  []string // "User/amy", "Group/dev", "ServiceAccount/<ns>/<name>"
}

// CanIList asks kubectl what the user may do in the session's namespace
// (read only: a self-review, nothing changes).
func (c Client) CanIList(ctx context.Context) ([]Permission, error) {
	args := append([]string{"auth", "can-i", "--list"}, c.scope(true)...)
	out, err := c.capture(ctx, args...)
	if err != nil {
		return nil, err
	}
	return ParseCanIList(out)
}

// RoleBindings lists the namespace's role bindings.
func (c Client) RoleBindings(ctx context.Context) ([]RoleBinding, error) {
	b, err := c.get(ctx, "rolebindings", true)
	if err != nil {
		return nil, err
	}
	return ParseRoleBindings(b)
}

// ParseCanIList reads the `auth can-i --list` table. Bracket lists hold
// spaces ("[get list watch]"), so the columns are cut at the header's
// column starts.
func ParseCanIList(out string) ([]Permission, error) {
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return nil, nil
	}
	head := lines[0]
	cols := []string{"Resources", "Non-Resource URLs", "Resource Names", "Verbs"}
	starts := make([]int, len(cols))
	for i, name := range cols {
		starts[i] = strings.Index(head, name)
		if starts[i] < 0 || (i > 0 && starts[i] <= starts[i-1]) {
			return nil, errors.New("kubectl auth can-i: unknown table header: " + clip(head))
		}
	}
	var perms []Permission
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		field := func(i int) string {
			from := starts[i]
			if from >= len(l) {
				return ""
			}
			to := len(l)
			if i+1 < len(starts) && starts[i+1] < to {
				to = starts[i+1]
			}
			return strings.TrimSpace(l[from:to])
		}
		perms = append(perms, Permission{
			Resource: field(0),
			URLs:     bracketList(field(1)),
			Names:    bracketList(field(2)),
			Verbs:    bracketList(field(3)),
		})
	}
	return perms, nil
}

// bracketList reads "[a b c]" ("[]" is nil).
func bracketList(s string) []string {
	s = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(s), "["), "]")
	return strings.Fields(s)
}

// ParseRoleBindings reads `get rolebindings -o json`.
func ParseRoleBindings(b []byte) ([]RoleBinding, error) {
	var doc struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			RoleRef struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"roleRef"`
			Subjects []struct {
				Kind      string `json:"kind"`
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"subjects"`
		} `json:"items"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, errors.New("kubectl get rolebindings: bad JSON: " + clip(err.Error()))
	}
	out := make([]RoleBinding, 0, len(doc.Items))
	for _, it := range doc.Items {
		rb := RoleBinding{Name: it.Metadata.Name, Namespace: it.Metadata.Namespace, Role: it.RoleRef.Kind + "/" + it.RoleRef.Name}
		for _, s := range it.Subjects {
			name := s.Name
			if s.Kind == "ServiceAccount" && s.Namespace != "" {
				name = s.Namespace + "/" + s.Name
			}
			rb.Subjects = append(rb.Subjects, s.Kind+"/"+name)
		}
		out = append(out, rb)
	}
	return out, nil
}
