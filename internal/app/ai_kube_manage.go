package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/kube"
)

// kube_manage: everything of the Kubernetes view, in any kube context
// and namespace (default the view's).
//
// It asks where the view asks: restart, scale, delete, rollout_undo,
// set_image, apply, cordon, uncordon, drain, the helm writes, upload and
// debug are the view's risky cards and ask every time, Cancel first, with
// no "Always allow". The view's pickers and keys that act at once run at
// once here too: use_context and use_namespace (the view's c and n),
// shell and logs_follow (e and Enter open a terminal tab), port_forward
// and stop_forward (f), and every read. download writes a workspace file,
// so it asks as a file write. A secret's value never reaches the model:
// yaml refuses secrets and there is no op that reads one.

var aiKubeCardOps = []string{"restart", "scale", "delete", "rollout_undo", "set_image", "apply", "cordon", "uncordon", "drain",
	"helm_install", "helm_repo_add", "helm_rollback", "helm_uninstall", "upload", "debug"}

var aiKubeAtOnceOps = []string{"contexts", "namespaces", "use_context", "use_namespace", "yaml", "events", "top", "nodes", "hpa",
	"rbac", "report", "objects", "kustomize", "helm_list", "helm_status", "helm_history", "rollout_history", "pod_files",
	"snippet", "forwards", "shell", "logs_follow", "port_forward", "stop_forward"}

func aiKubeOps() []string {
	out := append(append([]string(nil), aiKubeAtOnceOps...), aiKubeCardOps...)
	return append(out, "download")
}

// aiKubeContextOK checks the form of a kube context name (the cluster's
// own names hold ':', '@', '/' …).
func aiKubeContextOK(s string) error {
	if s == "" || len(s) > 253 || strings.HasPrefix(s, "-") || strings.ContainsFunc(s, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return fmt.Errorf("context %q is not a kube context name (see kube_manage op contexts)", s)
	}
	return nil
}

// aiKubeScope is kubectl in the scope a names (context, namespace), else
// the view's. A context other than the view's must be in kubeconfig.
func aiKubeScope(ctx context.Context, call aiIDECaller, a map[string]any) (kube.Client, error) {
	kctx := strings.TrimSpace(ai.ArgString(a, "context"))
	ns := strings.TrimSpace(ai.ArgString(a, "namespace"))
	if kctx != "" {
		if err := aiKubeContextOK(kctx); err != nil {
			return kube.Client{}, err
		}
	}
	if ns != "" {
		if err := aiKubeValidName("namespace", ns); err != nil {
			return kube.Client{}, err
		}
	}
	c, err := aiKubeClient(ctx, call, ns)
	if err != nil {
		return c, err
	}
	if kctx != "" && kctx != c.Context {
		names, err := c.Contexts(ctx)
		if err != nil {
			return c, err
		}
		if !contains(names, kctx) {
			return c, fmt.Errorf("no kube context %q; contexts: %s", kctx, strings.Join(names, ", "))
		}
		c.Context = kctx
		if ns == "" {
			c.Namespace = "" // the new context's own namespace
		}
	}
	return kubeResolve(ctx, c)
}

func aiKubeWhere(c kube.Client) string {
	return "context " + c.Context + " · namespace " + c.Namespace
}

// aiKubeReq is one checked kube_manage call.
type aiKubeReq struct {
	op        string
	c         kube.Client
	kind      string
	name      string
	container string
	argv      string // the kubectl command the card shows
	note      string // the view's card sentence
	file      string // apply, upload, download: the workspace file
	podPath   string
	n         int // scale: replicas; rollout_undo / helm_rollback: revision
	image     string
	chart     string
	version   string
	url       string
}

func aiKubeParse(ctx context.Context, call aiIDECaller, a map[string]any) (aiKubeReq, error) {
	r := aiKubeReq{op: ai.ArgString(a, "op"), kind: strings.ToLower(strings.TrimSpace(ai.ArgString(a, "kind"))),
		name: strings.TrimSpace(ai.ArgString(a, "name")), container: strings.TrimSpace(ai.ArgString(a, "container"))}
	if !contains(aiKubeOps(), r.op) {
		return r, errors.New("give op: " + strings.Join(aiKubeOps(), ", "))
	}
	if p := strings.TrimSpace(ai.ArgString(a, "pod")); p != "" && r.name == "" {
		r.name = p
	}
	name := func(what string) error {
		if err := aiKubeValidName(what, r.name); err != nil {
			return err
		}
		return nil
	}
	if r.container != "" {
		if err := aiKubeValidName("container", r.container); err != nil {
			return r, err
		}
	}
	root := aiWorkspaceRoot()
	wsFile := func(must bool) error {
		rel := strings.TrimSpace(ai.ArgString(a, "file"))
		p, err := aiDBInside(root, rel)
		if err != nil {
			return fmt.Errorf("file: %v", err)
		}
		if fi, err := os.Stat(p); must && (err != nil || fi.IsDir()) {
			return fmt.Errorf("file: %s is not a file in the workspace", rel)
		}
		r.file = p
		return nil
	}
	podPath := func() error {
		r.podPath = strings.TrimSpace(ai.ArgString(a, "path"))
		if r.podPath == "" && r.op == "pod_files" {
			r.podPath = "/"
		}
		if !path.IsAbs(r.podPath) || strings.ContainsAny(r.podPath, "\n\x00") || strings.HasPrefix(r.podPath, "-") {
			return errors.New("path: an absolute path in the pod, like /app")
		}
		return nil
	}
	var err error
	switch r.op {
	case "use_context":
		if err := aiKubeContextOK(r.name); err != nil {
			return r, err
		}
	case "use_namespace":
		if err := name("namespace"); err != nil {
			return r, err
		}
	case "yaml", "delete":
		if r.kind == "" || strings.HasPrefix(r.kind, "-") || strings.ContainsAny(r.kind, " \t\n/") {
			return r, errors.New("give kind, like pod, deployment, service or configmap")
		}
		if err := name("name"); err != nil {
			return r, err
		}
		k := strings.TrimSuffix(r.kind, "s")
		if r.op == "yaml" && k == "secret" {
			return r, errors.New("a secret's yaml holds its values; secret values never reach the model")
		}
		if r.op == "delete" && (k == "node" || k == "event" || k == "namespace") {
			return r, fmt.Errorf("a %s is not deleted from the Kubernetes view", k)
		}
	case "events":
		if r.name != "" {
			if err := name("name"); err != nil {
				return r, err
			}
		}
	case "objects":
		if r.kind == "" || strings.ContainsAny(r.kind, " \t\n") || strings.HasPrefix(r.kind, "-") {
			return r, errors.New("give kind: any resource, like certificates or cm")
		}
	case "restart", "rollout_history", "rollout_undo", "set_image":
		if err := name("deployment"); err != nil {
			return r, err
		}
		switch r.op {
		case "rollout_undo":
			r.n = ai.ArgInt(a, "revision")
			if r.n < 0 {
				return r, errors.New("revision must be 0 (the one before) or a revision number")
			}
		case "set_image":
			r.image = strings.TrimSpace(ai.ArgString(a, "image"))
			if r.container == "" || r.image == "" || strings.HasPrefix(r.image, "-") || strings.ContainsAny(r.image, " \t\n=") {
				return r, errors.New("set_image: give container and image")
			}
		}
	case "scale":
		if r.kind == "" {
			r.kind = "deployment"
		}
		if r.kind != "deployment" && r.kind != "statefulset" {
			return r, errors.New("only a deployment or a statefulset can be scaled")
		}
		if err := name("name"); err != nil {
			return r, err
		}
		r.n = ai.ArgInt(a, "replicas")
		if r.n < 0 || r.n > 1000 {
			return r, errors.New("replicas must be 0 to 1000")
		}
	case "cordon", "uncordon", "drain":
		if err := name("node"); err != nil {
			return r, err
		}
	case "apply":
		if err := wsFile(true); err != nil {
			return r, err
		}
	case "kustomize":
		rel := strings.TrimSpace(ai.ArgString(a, "dir"))
		if rel == "" {
			rel = "."
		}
		if r.file, err = aiDBInside(root, rel); err != nil {
			return r, fmt.Errorf("dir: %v", err)
		}
	case "helm_status", "helm_history", "helm_uninstall", "helm_rollback":
		if err := name("release"); err != nil {
			return r, err
		}
		r.n = ai.ArgInt(a, "revision")
		if r.op == "helm_rollback" && r.n <= 0 {
			return r, errors.New("helm_rollback: give revision (helm_history lists them)")
		}
	case "helm_install":
		r.chart, r.version = strings.TrimSpace(ai.ArgString(a, "chart")), strings.TrimSpace(ai.ArgString(a, "version"))
		if err := kube.CheckChart(r.name, r.chart, r.version); err != nil {
			return r, err
		}
	case "helm_repo_add":
		r.url = strings.TrimSpace(ai.ArgString(a, "url"))
		if err := kube.CheckRepo(r.name, r.url); err != nil {
			return r, err
		}
	case "pod_files", "shell", "logs_follow", "upload", "download", "debug":
		if err := name("pod"); err != nil {
			return r, err
		}
		switch r.op {
		case "pod_files":
			if err := podPath(); err != nil {
				return r, err
			}
		case "upload", "download":
			if err := podPath(); err != nil {
				return r, err
			}
			if err := wsFile(r.op == "upload"); err != nil {
				return r, err
			}
		case "debug":
			r.image = strings.TrimSpace(ai.ArgString(a, "image"))
		}
	case "port_forward", "stop_forward":
		if r.kind == "" {
			r.kind = "pod"
		}
		if r.kind == "service" {
			r.kind = "svc"
		}
		if r.kind != "pod" && r.kind != "svc" && r.kind != "deployment" {
			return r, errors.New("kind must be pod, svc or deployment")
		}
		if err := name("name"); err != nil {
			return r, err
		}
		r.n = ai.ArgInt(a, "port")
		if r.op == "port_forward" && (r.n <= 0 || r.n > 65535) {
			return r, errors.New("port_forward: give port, the object's port")
		}
	case "snippet":
		if id := strings.TrimSpace(ai.ArgString(a, "id")); id != "" && r.name != "" {
			if err := name("name"); err != nil {
				return r, err
			}
		}
	}
	if r.c, err = aiKubeScope(ctx, call, a); err != nil {
		return r, err
	}
	c := r.c
	switch r.op {
	case "use_context":
		names, err := c.Contexts(ctx)
		if err != nil {
			return r, err
		}
		if !contains(names, r.name) {
			return r, fmt.Errorf("no kube context %q; contexts: %s", r.name, strings.Join(names, ", "))
		}
	case "restart":
		r.argv, r.note = "kubectl rollout restart deployment/"+r.name, "every pod of "+r.name+" restarts, one by one"
	case "scale":
		r.argv = "kubectl scale " + r.kind + "/" + r.name + " --replicas=" + strconv.Itoa(r.n)
		if r.n == 0 {
			r.note = "it stops every pod of " + r.name
		}
	case "delete":
		r.argv, r.note = "kubectl delete "+r.kind+" "+r.name, "this cannot be undone"
		if n := kubeDeleteNote(strings.TrimSuffix(r.kind, "s")); n != "" {
			r.note += "; " + n
		}
	case "rollout_undo":
		r.argv, r.note = "kubectl rollout undo deployment/"+r.name, "its pods go back to the earlier revision"
		if r.n > 0 {
			r.argv += " --to-revision=" + strconv.Itoa(r.n)
		}
	case "set_image":
		r.argv, r.note = "kubectl set image deployment/"+r.name+" "+r.container+"="+r.image, "its pods restart with the new image"
	case "cordon", "uncordon", "drain":
		r.argv = "kubectl " + r.op + " " + r.name
		r.note = map[string]string{"cordon": "no new pods start on it", "uncordon": "new pods may start on it again",
			"drain": "every pod moves off it (daemon sets stay); it is cordoned"}[r.op]
	case "apply":
		r.argv = "kubectl apply -f " + r.file
		if d, err := c.DiffFile(ctx, r.file); err == nil {
			add, del := kubeDiffStat(d.Text)
			r.note = fmt.Sprintf("it writes to the cluster: +%d −%d lines against what runs", add, del)
			if !d.Changed {
				r.note = "the cluster has it already (no change in the dry run)"
			}
		} else {
			r.note = "it writes to the cluster (the dry run failed: " + aiOneLineClip(err.Error(), 100) + ")"
		}
	case "helm_install":
		r.argv, r.note = "helm "+strings.Join(kube.InstallArgs(r.name, r.chart, r.version), " "), "it creates objects in the cluster"
	case "helm_repo_add":
		r.argv, r.note = "helm repo add "+r.name+" "+r.url, "it changes your helm repo list, not the cluster"
	case "helm_rollback":
		r.argv, r.note = "helm rollback "+r.name+" "+strconv.Itoa(r.n), "the release goes back to that revision"
	case "helm_uninstall":
		r.argv, r.note = "helm uninstall "+r.name, "every object of the release goes"
	case "upload":
		r.argv, r.note = "kubectl cp "+r.file+" "+r.name+":"+r.podPath, "a file of the same name in the pod is replaced"
	case "debug":
		cmd, err := c.DebugCommand(r.name, r.container, r.image)
		if err != nil {
			return r, err
		}
		r.argv, r.note = svcShellLine(cmd.Argv), "an ephemeral debug container joins the pod; it stays in the pod's spec"
	}
	return r, nil
}

func aiKubeManageDescribe(call aiIDECaller, a map[string]any) string {
	op, name := ai.ArgString(a, "op"), strings.TrimSpace(ai.ArgString(a, "name")+ai.ArgString(a, "pod"))
	ctx, cancel := aiContainersDescribeCtx()
	defer cancel()
	r, err := aiKubeParse(ctx, call, a)
	title := map[string]string{"restart": "Restart deployment", "scale": "Scale", "delete": "Delete", "rollout_undo": "Roll back deployment",
		"set_image": "Set image of deployment", "apply": "Apply", "cordon": "Cordon node", "uncordon": "Uncordon node", "drain": "Drain node",
		"helm_install": "Install Helm chart as", "helm_repo_add": "Add Helm repo", "helm_rollback": "Roll back Helm release",
		"helm_uninstall": "Uninstall Helm release", "upload": "Upload into pod", "debug": "Debug pod", "download": "Download from pod"}[op]
	if title == "" {
		title = aiTitle(strings.ReplaceAll(op, "_", " "))
	}
	if err != nil {
		return title + " " + name + " (refused: " + aiOneLineClip(err.Error(), 120) + ")"
	}
	what := r.name
	switch r.op {
	case "scale", "delete":
		what = r.kind + " " + r.name
	case "apply":
		what = filepath.Base(r.file)
	}
	s := title + " " + what
	if r.argv != "" {
		s += " (" + r.argv + ")"
	}
	s += " · " + aiKubeWhere(r.c)
	if r.note != "" {
		s += " · " + r.note
	}
	return s
}

// aiKubeContainer is the pod's container to use: the one named, else its
// only one.
func aiKubeContainer(ctx context.Context, c kube.Client, pod, container string) (string, error) {
	if container != "" {
		return container, nil
	}
	pods, err := c.Pods(ctx)
	if err != nil {
		return "", err
	}
	for _, p := range pods {
		if p.Name != pod {
			continue
		}
		if len(p.Containers) == 1 {
			return p.Containers[0], nil
		}
		return "", fmt.Errorf("pod %s has containers %s: name one (container)", pod, strings.Join(p.Containers, ", "))
	}
	return "", fmt.Errorf("no pod %q in %s (see kube_list)", pod, aiKubeWhere(c))
}

func aiKubeManageRun(ctx context.Context, call aiIDECaller, a map[string]any) (string, error) {
	r, err := aiKubeParse(ctx, call, a)
	if err != nil {
		return "", err
	}
	c, where := r.c, aiKubeWhere(r.c)
	head := "Kubernetes " + where + "\n"
	redact := func(s string) string { s, _ = aiServicesRedact(s); return s }
	capped := func(s string) string { return aiCap(redact(s), loadServicesSettings().MaxInspectKB, false) }
	done := func(err error, s string) (string, error) {
		if err != nil {
			return "", fmt.Errorf("%s failed: %s", strings.ReplaceAll(r.op, "_", " "), errLine(redact(err.Error())))
		}
		_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) {
			if m.kube != nil && m.kube.context == c.Context && m.kube.namespace == c.Namespace {
				return "", m.kubeReload(), nil
			}
			return "", nil, nil
		})
		return s + " · " + where, nil
	}
	helm := func() (kube.Helm, error) {
		var h kube.Helm
		_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) {
			h = m.kubeHelm(kubeHelmTarget{context: c.Context, namespace: c.Namespace})
			return "", nil, nil
		})
		if h.Binary == "" {
			return h, errors.New("helm is not installed")
		}
		return h, nil
	}
	switch r.op {
	case "contexts":
		names, err := c.Contexts(ctx)
		if err != nil {
			return "", err
		}
		return "Kube contexts (the view uses " + c.Context + "): " + strings.Join(names, ", "), nil
	case "namespaces":
		names, err := c.Namespaces(ctx)
		if err != nil {
			return "", err
		}
		return "Namespaces in context " + c.Context + ": " + strings.Join(names, ", "), nil
	case "use_context", "use_namespace":
		target := r.name
		if r.op == "use_context" {
			names, err := c.Contexts(ctx)
			if err != nil {
				return "", err
			}
			if !contains(names, target) {
				return "", fmt.Errorf("no kube context %q; contexts: %s", target, strings.Join(names, ", "))
			}
		}
		return call(ctx, func(m *Model) (string, tea.Cmd, error) {
			if m.kube == nil {
				return "", nil, errors.New("the Kubernetes view is not available")
			}
			st := m.kube
			if r.op == "use_context" {
				if target == st.context {
					return "the view already uses context " + target + " (no change)", nil, nil
				}
				return "the Kubernetes view now uses context " + target + " (its own namespace)", m.kubeRescope(target, ""), nil
			}
			if target == st.namespace {
				return "the view already shows namespace " + target + " (no change)", nil, nil
			}
			return "the Kubernetes view now shows namespace " + target + " of context " + st.context, m.kubeRescope(st.context, target), nil
		})
	case "yaml":
		text, err := c.YAML(ctx, r.kind, r.name)
		if err != nil {
			return "", err
		}
		return head + capped(text), nil
	case "events":
		evs, err := c.Events(ctx, r.kind, r.name)
		if err != nil {
			return "", err
		}
		return head + capped(kube.EventsText(evs, clock.Now())), nil
	case "top":
		msg := kubeTopMsg{namespace: c.Namespace}
		msg.nodes, msg.nodesErr = c.TopNodes(ctx)
		msg.pods, msg.podsErr = c.TopPods(ctx)
		return head + kubeTopText(msg, nil), nil
	case "nodes":
		nodes, err := c.Nodes(ctx)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Nodes of context %s: %d\n", c.Context, len(nodes))
		for _, n := range nodes {
			fmt.Fprintf(&b, "- %s · %s · roles %s · %s · age %s\n", n.Name, n.Status, joinOr(n.Roles, "none"), n.Version, kube.Age(n.Created, clock.Now()))
		}
		return strings.TrimRight(b.String(), "\n"), nil
	case "hpa":
		hs, err := c.HPAs(ctx)
		if err != nil {
			return "", err
		}
		return head + kubeHPAText(hs), nil
	case "rbac":
		perms, permErr := c.CanIList(ctx)
		rbs, rbErr := c.RoleBindings(ctx)
		sas, saErr := c.Objects(ctx, kube.APIResource{Name: "serviceaccounts", Namespaced: true, Kind: "ServiceAccount"})
		if permErr != nil && rbErr != nil && saErr != nil {
			return "", permErr
		}
		return head + capped(kubeRBACText(perms, permErr, rbs, rbErr, sas, saErr)), nil
	case "report":
		rep, err := c.Report(ctx)
		if err != nil {
			return "", err
		}
		return head + capped(rep.Text()), nil
	case "objects":
		rs, err := c.APIResources(ctx)
		if err != nil {
			return "", err
		}
		res, ok := kube.ResolveKind(rs, loadKubeAliases(), r.kind)
		if !ok {
			return "", fmt.Errorf("no resource %q in this cluster", r.kind)
		}
		objs, err := c.Objects(ctx, res)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s%s (%s): %d\n", head, res.Name, res.Kind, len(objs))
		for i, o := range objs {
			if i == aiContainersListLimit {
				fmt.Fprintf(&b, "… %d more\n", len(objs)-i)
				break
			}
			fmt.Fprintf(&b, "- %s · %s · age %s\n", o.Name, orStr(o.Status, "-"), kube.Age(o.Created, clock.Now()))
		}
		return redact(strings.TrimRight(b.String(), "\n")), nil
	case "kustomize":
		text, err := c.Kustomize(ctx, r.file)
		if err != nil {
			return "", err
		}
		return "kustomize " + r.file + "\n" + capped(text), nil
	case "rollout_history":
		revs, err := c.RolloutHistory(ctx, r.name)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%sRevisions of deployment %s: %d\n", head, r.name, len(revs))
		for _, v := range revs {
			fmt.Fprintf(&b, "- %d · %s\n", v.Number, orStr(v.Cause, "<none>"))
		}
		return strings.TrimRight(b.String(), "\n"), nil
	case "pod_files":
		ctr, err := aiKubeContainer(ctx, c, r.name, r.container)
		if err != nil {
			return "", err
		}
		files, err := c.ListPodDir(ctx, r.name, ctr, r.podPath)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s%s:%s (%s): %d\n", head, r.name, r.podPath, ctr, len(files))
		for _, f := range files {
			switch {
			case f.Dir:
				b.WriteString("- " + f.Name + "/\n")
			case f.Link != "":
				b.WriteString("- " + f.Name + " → " + f.Link + "\n")
			default:
				fmt.Fprintf(&b, "- %s · %s\n", f.Name, humanBytes(f.Size))
			}
		}
		return strings.TrimRight(b.String(), "\n"), nil
	case "snippet":
		id := strings.TrimSpace(ai.ArgString(a, "id"))
		if id == "" {
			var b strings.Builder
			b.WriteString("Manifest snippets (id · kind · title); pass id (and name) for its text, then write_file it:\n")
			for _, s := range kube.Snippets() {
				fmt.Fprintf(&b, "- %s · %s · %s\n", s.ID, s.Kind, s.Title)
			}
			return strings.TrimRight(b.String(), "\n"), nil
		}
		text, ok := kube.Render(id, r.name, c.Namespace)
		if !ok {
			return "", fmt.Errorf("no snippet %q (op snippet with no id lists them)", id)
		}
		return text, nil
	case "forwards":
		return call(ctx, func(m *Model) (string, tea.Cmd, error) {
			if m.kube == nil || len(m.kube.forwards) == 0 {
				return "No port-forwards run.", nil, nil
			}
			var lines []string
			for _, k := range m.kube.forwardKeys() {
				fw := m.kube.forwards[k]
				lines = append(lines, fmt.Sprintf("- %s · 127.0.0.1:%d → %s:%d", k, fw.Local, fw.Target(), fw.Remote))
			}
			return "Port-forwards:\n" + strings.Join(lines, "\n"), nil, nil
		})
	case "helm_list", "helm_status", "helm_history":
		h, err := helm()
		if err != nil {
			return "", err
		}
		return aiKubeHelmRead(ctx, h, r, head)
	case "shell", "logs_follow", "debug":
		var job terminalJob
		switch r.op {
		case "debug":
			cmd, err := c.DebugCommand(r.name, r.container, r.image)
			if err != nil {
				return "", err
			}
			job = terminalJob{Label: "debug: " + r.name, Cmdline: svcShellLine(cmd.Argv), Service: "k8s:debug:" + c.Context + "/" + c.Namespace + "/" + r.name}
		default:
			ctr, err := aiKubeContainer(ctx, c, r.name, r.container)
			if err != nil {
				return "", err
			}
			kind := "logs"
			cmd := c.LogsCommand(r.name, ctr)
			if r.op == "shell" {
				kind, cmd = "shell", c.ShellCommand(r.name, ctr)
			}
			job = terminalJob{Label: cmd.Label, Cmdline: svcShellLine(cmd.Argv),
				Service: "k8s:" + kind + ":" + c.Context + "/" + c.Namespace + "/" + r.name + "/" + ctr}
		}
		return call(ctx, func(m *Model) (string, tea.Cmd, error) {
			if r.op != "debug" && m.servicesFocusTab(job.Service) {
				return "focused the open tab \"" + job.Label + "\" (no new tab)", nil, nil
			}
			if err := kubeSpawnJob(m, job); err != nil {
				return "", nil, fmt.Errorf("cannot open the tab: %v", err)
			}
			return fmt.Sprintf("opened `%s` in the terminal tab %q · %s; terminal_read reads it", job.Cmdline, job.Label, where), nil, nil
		})
	case "port_forward":
		key := c.Context + "/" + c.Namespace + "/" + r.kind + "/" + r.name
		fw, err := c.PortForwardTo(context.Background(), r.kind, r.name, r.n)
		if err != nil {
			return "", fmt.Errorf("cannot forward the port: %s", errLine(redact(err.Error())))
		}
		return call(ctx, func(m *Model) (string, tea.Cmd, error) {
			if m.kube == nil {
				fw.Stop()
				return "", nil, errors.New("the Kubernetes view is not available")
			}
			if old := m.kube.forwards[key]; old != nil {
				old.Stop()
			}
			m.kube.forwards[key] = fw
			m.kube.touch()
			return fmt.Sprintf("forwarding 127.0.0.1:%d → %s:%d (the view lists it under FORWARDS; stop_forward stops it) · %s", fw.Local, fw.Target(), fw.Remote, where), nil, nil
		})
	case "stop_forward":
		key := c.Context + "/" + c.Namespace + "/" + r.kind + "/" + r.name
		return call(ctx, func(m *Model) (string, tea.Cmd, error) {
			if m.kube == nil || m.kube.forwards[key] == nil {
				return "", nil, fmt.Errorf("no port-forward of %s/%s runs in %s (op forwards lists them)", r.kind, r.name, where)
			}
			return "stopped the port-forward of " + r.kind + "/" + r.name, m.kubeStopForward(key), nil
		})
	case "restart":
		return done(c.RolloutRestart(ctx, r.name), "ran `"+r.argv+"`")
	case "scale":
		return done(c.Scale(ctx, r.kind, r.name, r.n), "ran `"+r.argv+"`")
	case "delete":
		return done(c.Delete(ctx, r.kind, r.name), "ran `"+r.argv+"`")
	case "rollout_undo":
		return done(c.RolloutUndo(ctx, r.name, r.n), "ran `"+r.argv+"`")
	case "set_image":
		return done(c.SetImage(ctx, r.name, r.container, r.image), "ran `"+r.argv+"`")
	case "cordon":
		return done(c.Cordon(ctx, r.name), "ran `"+r.argv+"`")
	case "uncordon":
		return done(c.Uncordon(ctx, r.name), "ran `"+r.argv+"`")
	case "drain":
		return done(c.Drain(ctx, r.name), "ran `"+r.argv+"`")
	case "apply":
		out, err := c.ApplyFile(ctx, r.file)
		return done(err, "ran `"+r.argv+"`: "+aiOneLineClip(redact(out), 400))
	case "upload", "download":
		ctr, err := aiKubeContainer(ctx, c, r.name, r.container)
		if err != nil {
			return "", err
		}
		cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if r.op == "upload" {
			return done(c.CopyToPod(cctx, r.name, ctr, r.file, r.podPath), "uploaded "+filepath.Base(r.file)+" to "+r.name+":"+r.podPath)
		}
		if err := os.MkdirAll(filepath.Dir(r.file), 0o755); err != nil {
			return "", err
		}
		return done(c.CopyFromPod(cctx, r.name, ctr, r.podPath, r.file), "downloaded "+r.name+":"+r.podPath+" to "+r.file)
	}
	h, err := helm()
	if err != nil {
		return "", err
	}
	switch r.op {
	case "helm_install":
		out, err := h.Install(ctx, r.name, r.chart, r.version)
		return done(err, "ran `"+r.argv+"`: "+aiOneLineClip(redact(out), 400))
	case "helm_repo_add":
		return done(h.RepoAdd(ctx, r.name, r.url), "ran `"+r.argv+"`")
	case "helm_rollback":
		return done(h.Rollback(ctx, r.name, r.n), "ran `"+r.argv+"`")
	}
	return done(h.Uninstall(ctx, r.name), "ran `"+r.argv+"`")
}

// aiKubeHelmRead is helm_list, helm_status and helm_history.
func aiKubeHelmRead(ctx context.Context, h kube.Helm, r aiKubeReq, head string) (string, error) {
	switch r.op {
	case "helm_list":
		rs, err := h.Releases(ctx)
		if err != nil {
			return "", err
		}
		sort.Slice(rs, func(i, j int) bool { return rs[i].Name < rs[j].Name })
		var b strings.Builder
		fmt.Fprintf(&b, "%sHelm releases: %d\n", head, len(rs))
		for _, x := range rs {
			fmt.Fprintf(&b, "- %s · %s · revision %d · %s · %s · app %s · %s\n", x.Name, x.Namespace, x.Revision, x.Status, x.Chart, x.AppVersion, x.Updated)
		}
		return strings.TrimRight(b.String(), "\n"), nil
	case "helm_status":
		s, err := h.Status(ctx, r.name)
		if err != nil {
			return "", err
		}
		out, _ := aiServicesRedact(head + kubeHelmStatusText(s))
		return out, nil
	}
	revs, err := h.History(ctx, r.name)
	if err != nil {
		return "", err
	}
	return head + kubeHelmHistoryText(r.name, revs), nil
}

// aiKubeManageTool is kube_manage.
func aiKubeManageTool(call aiIDECaller) ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	num := func(d string) map[string]any { return map[string]any{"type": "integer", "description": d} }
	return ai.Tool{
		Name: "kube_manage",
		Description: "Everything of the Kubernetes view, in a kube context and namespace (default the view's). Reads: contexts, namespaces, " +
			"yaml (kind, name; never a secret), events (kind, name optional), top, nodes, hpa, rbac, report, objects (kind: any resource), " +
			"kustomize (dir), helm_list, helm_status / helm_history (name: release), rollout_history (name), pod_files (pod, path), snippet " +
			"(id, name: a manifest template), forwards. At once, as the view: use_context / use_namespace (name: switch the view), shell and " +
			"logs_follow (pod, container: a terminal tab), port_forward (kind pod|svc|deployment, name, port) and stop_forward. The view's " +
			"risky cards, asked every time: restart (name: deployment), scale (kind deployment|statefulset, name, replicas), delete (kind, name), " +
			"rollout_undo (name, revision), set_image (name, container, image), apply (file: a workspace manifest; the card shows the dry-run " +
			"diff size), cordon / uncordon / drain (name: node), helm_install (name: release, chart, version), helm_repo_add (name, url), " +
			"helm_rollback (name, revision), helm_uninstall (name), upload (pod, file, path) and debug (pod, container: target, image). " +
			"download (pod, path, file) writes a workspace file and asks.",
		Params: map[string]any{
			"op":        map[string]any{"type": "string", "enum": aiKubeOps(), "description": "what to do"},
			"context":   str("kube context (default the view's; op contexts lists them)"),
			"namespace": str("namespace (default the view's, or the context's own)"),
			"kind":      str("the object kind (pod, deployment, service, configmap …)"),
			"name":      str("the object, node, release, repo, context or namespace name"),
			"pod":       str("the pod (shell, logs_follow, pod_files, upload, download, debug)"),
			"container": str("the container (when the pod has more than one); set_image: the container to change"),
			"replicas":  num("scale: how many pods"), "revision": num("rollout_undo / helm_rollback: the revision"),
			"image": str("set_image: the new image; debug: the debug image"),
			"port":  num("port_forward: the object's port"),
			"file":  str("apply: a manifest; upload: the local file; download: where to write it (relative to the workspace)"),
			"path":  str("pod_files / upload / download: an absolute path in the pod"),
			"dir":   str("kustomize: the folder (relative to the workspace)"),
			"chart": str("helm_install: the chart like bitnami/redis"), "version": str("helm_install: the chart version (optional)"),
			"url": str("helm_repo_add: the repo URL"), "id": str("snippet: the snippet id"),
		},
		Required: []string{"op"},
		Mutating: true,
		Gate: func(a map[string]any) (bool, string) {
			op := ai.ArgString(a, "op")
			switch {
			case contains(aiKubeAtOnceOps, op):
				return false, ""
			case op == "download":
				return true, ai.ClassFileWrite
			}
			return true, ""
		},
		Validate: func(a map[string]any) error {
			ctx, cancel := context.WithTimeout(context.Background(), aiContainersCallWait)
			defer cancel()
			_, err := aiKubeParse(ctx, call, a)
			return err
		},
		Describe: func(a map[string]any) string { return aiKubeManageDescribe(call, a) },
		Run:      func(ctx context.Context, a map[string]any) (string, error) { return aiKubeManageRun(ctx, call, a) },
	}
}
