package app

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/kube"
)

// ── Kubernetes: RBAC page (CTX-28) ───────────────────────────────────────
//
// "Kubernetes: Permissions (RBAC)" (P in the view) opens a read-only page
// of the namespace: what you may do (`kubectl auth can-i --list`, a
// self-review that changes nothing), the role bindings (role → subjects)
// and the service accounts. A part that fails (forbidden) says so in its
// place; the other parts still show. The AI may open it.

// kubeRBACText is the page: three parts, each with its rows or its error.
func kubeRBACText(perms []kube.Permission, permErr error, rbs []kube.RoleBinding, rbErr error, sas []kube.Object, saErr error) string {
	var b strings.Builder
	part := func(title string, err error, n int, empty string) bool {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s\n", title)
		switch {
		case err != nil:
			fmt.Fprintf(&b, "  cannot read: %s\n", err.Error())
			return false
		case n == 0:
			fmt.Fprintf(&b, "  %s\n", empty)
			return false
		}
		return true
	}
	if part("What you may do (kubectl auth can-i --list)", permErr, len(perms), "nothing") {
		w := len("RESOURCE")
		for _, p := range perms {
			w = max(w, len(kubePermWhat(p)))
		}
		fmt.Fprintf(&b, "  %-*s  %s\n", w, "RESOURCE", "VERBS")
		for _, p := range perms {
			fmt.Fprintf(&b, "  %-*s  %s\n", w, kubePermWhat(p), strings.Join(p.Verbs, " "))
		}
	}
	if part("Role bindings", rbErr, len(rbs), "no role bindings in this namespace") {
		for _, rb := range rbs {
			fmt.Fprintf(&b, "  %s → %s\n", rb.Name, rb.Role)
			for _, s := range rb.Subjects {
				fmt.Fprintf(&b, "      %s\n", s)
			}
		}
	}
	if part("Service accounts", saErr, len(sas), "no service accounts") {
		for _, sa := range sas {
			fmt.Fprintf(&b, "  %s\n", sa.Name)
		}
	}
	return b.String()
}

// kubePermWhat is a permission row's resource, its names, or its URLs.
func kubePermWhat(p kube.Permission) string {
	what := p.Resource
	if what == "" {
		what = strings.Join(p.URLs, " ")
	}
	if len(p.Names) > 0 {
		what += " [" + strings.Join(p.Names, " ") + "]"
	}
	return what
}

// kubeRBAC opens the RBAC page of the session's namespace.
func (m *Model) kubeRBAC() tea.Cmd {
	return m.kubePage("the permissions", "rbac", func(ctx context.Context, c kube.Client) (string, error) {
		perms, permErr := c.CanIList(ctx)
		rbs, rbErr := c.RoleBindings(ctx)
		sas, saErr := c.Objects(ctx, kube.APIResource{Name: "serviceaccounts", Namespaced: true, Kind: "ServiceAccount"})
		if permErr != nil && rbErr != nil && saErr != nil {
			return "", permErr // nothing to show
		}
		return kubeRBACText(perms, permErr, rbs, rbErr, sas, saErr), nil
	})
}
