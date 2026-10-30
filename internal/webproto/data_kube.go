package webproto

import "strings"

// Kubernetes (WEBP-4): the clusters the laptop's kubectl knows, read
// only, as the laptop's Kubernetes view and the AI's kube tools read
// them: contexts, namespaces, the pods, deployments, services and jobs of
// one namespace, a pod's newest log lines and `kubectl describe`. The
// kubeconfig is never written (no context switch on the laptop). The
// one write, restarting a deployment, waits for a laptop card, as on the
// laptop. Every name is checked; a context must be one kubectl lists.

// Kubernetes op names.
const (
	OpKubeContexts   = "kube.contexts"
	OpKubeNamespaces = "kube.namespaces"
	OpKubeList       = "kube.list"
	OpKubeLogs       = "kube.logs"
	OpKubeDescribe   = "kube.describe"
	OpKubeRestart    = "kube.restart"
)

var opKubeRows = []Policy{
	{Op: OpKubeContexts, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpKubeNamespaces, New: newOf[KubeScopeArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpKubeList, New: newOf[KubeScopeArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpKubeLogs, New: newOf[KubeLogsArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 2 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpKubeDescribe, New: newOf[KubeObjectArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 2 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpKubeRestart, New: newOf[KubeObjectArgs](), Role: RoleController, Rate: slow, MaxBytes: 2 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opKubeRows...); return true }()

func init() {
	registerResult(OpKubeContexts, KubeContexts{})
	registerResult(OpKubeNamespaces, KubeNamespaces{})
	registerResult(OpKubeList, KubeList{})
	registerResult(OpKubeLogs, KubeText{})
	registerResult(OpKubeDescribe, KubeText{})
	registerResult(OpKubeRestart, AskRef{})
}

// Kubernetes limits.
const (
	MaxKubeName     = 253
	MaxKubeItems    = 1000 // rows of one kind in kube.list
	MaxKubeContexts = 200
	MaxKubeLogLines = 1000
	MaxKubeText     = 64 << 10 // bytes of logs or describe text, the newest logs kept
	DefaultKubeLog  = 200
)

// KubeContexts answers kube.contexts: kubectl's contexts, the
// kubeconfig's current one, and the scope the laptop's view shows.
type KubeContexts struct {
	Contexts  []string `json:"contexts"`
	Current   string   `json:"current,omitempty"`
	Context   string   `json:"context,omitempty"`   // the laptop view's
	Namespace string   `json:"namespace,omitempty"` // the laptop view's
}

// KubeNamespaces answers kube.namespaces.
type KubeNamespaces struct {
	Namespaces []string `json:"namespaces"`
}

// KubeList answers kube.list: one namespace's objects. Errors has a line
// per kind that could not be listed ("pods": "forbidden …").
type KubeList struct {
	Context     string            `json:"context"`
	Namespace   string            `json:"namespace"`
	Pods        []KubePod         `json:"pods"`
	Deployments []KubeDeployment  `json:"deployments"`
	Services    []KubeService     `json:"services"`
	Jobs        []KubeJob         `json:"jobs"`
	Errors      map[string]string `json:"errors,omitempty"`
	More        int               `json:"more,omitempty"`
}

// KubePod is one pod. Created is Unix ms.
type KubePod struct {
	Name       string   `json:"name"`
	Ready      string   `json:"ready"` // "1/2"
	Status     string   `json:"status"`
	Restarts   int      `json:"restarts"`
	Created    int64    `json:"created,omitempty"`
	Containers []string `json:"containers"`
}

// KubeDeployment is one deployment.
type KubeDeployment struct {
	Name      string `json:"name"`
	Ready     string `json:"ready"`
	Updated   int    `json:"updated"`
	Available int    `json:"available"`
	Created   int64  `json:"created,omitempty"`
}

// KubeService is one service.
type KubeService struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	ClusterIP string `json:"cluster_ip,omitempty"`
	Ports     string `json:"ports,omitempty"` // "80:30080/TCP"
	Created   int64  `json:"created,omitempty"`
}

// KubeJob is one job.
type KubeJob struct {
	Name        string `json:"name"`
	Status      string `json:"status"` // Complete, Failed, Running, Pending
	Completions string `json:"completions"`
	Failed      int    `json:"failed,omitempty"`
	Created     int64  `json:"created,omitempty"`
}

// KubeText answers kube.logs and kube.describe: redacted lines.
type KubeText struct {
	Lines     []string `json:"lines"`
	Redacted  int      `json:"redacted,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
}

// KubeScopeArgs is a context and namespace ("": the laptop view's).
type KubeScopeArgs struct {
	Context   string `json:"context,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// KubeObjectArgs names one object of a kind in a scope.
type KubeObjectArgs struct {
	Context   string `json:"context,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Kind      string `json:"kind" enum:"pod|deployment|service|job"`
	Name      string `json:"name"`
}

// KubeLogsArgs asks for a pod's newest log lines.
type KubeLogsArgs struct {
	Context   string `json:"context,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Pod       string `json:"pod"`
	Container string `json:"container,omitempty"`
	Lines     int    `json:"lines,omitempty"` // 0: 200
}

// validKubeName is a Kubernetes object name (a DNS subdomain).
func validKubeName(s string) bool {
	if s == "" || len(s) > MaxKubeName {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || (c == '-' || c == '.') && i > 0 && i < len(s)-1
		if !ok {
			return false
		}
	}
	return true
}

// validKubeContext is a kubeconfig context name: printable, not a flag.
func validKubeContext(s string) bool {
	return s == "" || len(s) <= MaxKubeName && !hasControl(s) && !strings.HasPrefix(s, "-") && !strings.ContainsAny(s, " \t")
}

func (a *KubeScopeArgs) validate(int) error {
	if !validKubeContext(a.Context) || a.Namespace != "" && !validKubeName(a.Namespace) {
		return invalid("bad context or namespace")
	}
	return nil
}

func (a *KubeObjectArgs) validate(int) error {
	if !validKubeContext(a.Context) || a.Namespace != "" && !validKubeName(a.Namespace) || !validKubeName(a.Name) {
		return invalid("bad Kubernetes name")
	}
	switch a.Kind {
	case "pod", "deployment", "service", "job":
		return nil
	}
	return invalid("bad Kubernetes kind")
}

func (a *KubeLogsArgs) validate(int) error {
	if !validKubeContext(a.Context) || a.Namespace != "" && !validKubeName(a.Namespace) || !validKubeName(a.Pod) ||
		a.Container != "" && !validKubeName(a.Container) || a.Lines < 0 || a.Lines > MaxKubeLogLines {
		return invalid("bad Kubernetes name or lines")
	}
	return nil
}
