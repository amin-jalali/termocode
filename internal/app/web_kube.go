package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Kubernetes from the page (WEBP-4): read only, through the laptop's
// kubectl, like the laptop's Kubernetes view and the AI's kube tools
// (kube.Client: every call with a timeout, --context and --namespace as
// flags, the kubeconfig only read). A context from the page must be one
// kubectl lists; "" is the laptop view's scope (or kubectl's own). Logs
// and describe pass the AI's redaction. Restarting a deployment, the
// laptop view's one write, waits for a laptop card. Every kubectl call
// runs off the UI thread.

func init() {
	webOps[webproto.OpKubeContexts] = (*Model).webKubeContexts
	webOps[webproto.OpKubeNamespaces] = (*Model).webKubeNamespaces
	webOps[webproto.OpKubeList] = (*Model).webKubeList
	webOps[webproto.OpKubeLogs] = (*Model).webKubeLogs
	webOps[webproto.OpKubeDescribe] = (*Model).webKubeDescribe
	webAskOps[webproto.OpKubeRestart] = (*Model).webKubeRestart
}

// webKubeWait bounds one page request (several kubectl calls).
const webKubeWait = 30 * time.Second

// errWebKubeContext: the page named a context kubectl does not list.
var errWebKubeContext = webErr(webproto.CodeNotFound, "kubectl does not list that context.")

// webKubeClient is kubectl in the laptop view's scope, read on Update.
func (m *Model) webKubeClient(call *webCall) (kube.Client, bool) {
	var c kube.Client
	if m.kube != nil {
		c = m.kube.client()
	}
	c.X = servicesExec
	if c.Binary == "" {
		c.Binary = kube.Find(nil)
	}
	if c.Binary == "" {
		call.fail(webproto.CodeNotFound, (&kube.Error{Kind: kube.ErrNoKubectl}).Error())
		return c, false
	}
	return c, true
}

// webKubeScope sets the page's context and namespace on c (off the UI
// thread): a context must be one kubectl lists; a context without a
// namespace takes the context's own.
func webKubeScope(ctx context.Context, c kube.Client, kctx, ns string) (kube.Client, error) {
	if kctx != "" && kctx != c.Context {
		names, err := c.Contexts(ctx)
		if err != nil {
			return c, err
		}
		if !slices.Contains(names, kctx) {
			return c, errWebKubeContext
		}
		c.Context, c.Namespace = kctx, ""
		if ns == "" {
			if ns, err = c.ContextNamespace(ctx); err != nil {
				return c, err
			}
		}
	}
	if ns != "" {
		c.Namespace = ns
	}
	return c, nil
}

// webKubeFail answers a kubectl problem as one redacted line.
func webKubeFail(call *webCall, err error) {
	var we *webError
	if errors.As(err, &we) {
		call.fail(we.code, we.msg)
		return
	}
	msg, _ := aiServicesRedact(webName(err.Error()))
	call.fail(webproto.CodeConflict, webSentence(msg))
}

// webKubeRun runs read off the UI thread with the page's scope and
// answers what it returns.
func (m *Model) webKubeRun(call *webCall, kctx, ns string, read func(ctx context.Context, c kube.Client) (any, error)) tea.Cmd {
	c, ok := m.webKubeClient(call)
	if !ok {
		return nil
	}
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, webKubeWait)
		defer cancel()
		c, err := webKubeScope(ctx, c, kctx, ns)
		if err != nil {
			webKubeFail(call, err)
			return nil
		}
		out, err := read(ctx, c)
		if err != nil {
			webKubeFail(call, err)
			return nil
		}
		call.reply(out)
		return nil
	}
}

// webKubeContexts answers kubectl's contexts, the current one and the
// laptop view's scope.
func (m *Model) webKubeContexts(call *webCall) tea.Cmd {
	var viewCtx, viewNS string
	if m.kube != nil {
		viewCtx, viewNS = m.kube.context, m.kube.namespace
	}
	return m.webKubeRun(call, "", "", func(ctx context.Context, c kube.Client) (any, error) {
		names, err := c.Contexts(ctx)
		if err != nil {
			return nil, err
		}
		out := webproto.KubeContexts{Contexts: []string{}, Context: viewCtx, Namespace: viewNS}
		for i, n := range names {
			if i == webproto.MaxKubeContexts {
				break
			}
			out.Contexts = append(out.Contexts, webName(n))
		}
		out.Current, _ = c.CurrentContext(ctx)
		if out.Context == "" {
			out.Context = out.Current
		}
		if out.Namespace == "" && out.Context != "" {
			cc := c
			cc.Context = out.Context
			out.Namespace, _ = cc.ContextNamespace(ctx)
		}
		return out, nil
	})
}

// webKubeNamespaces answers a context's namespaces.
func (m *Model) webKubeNamespaces(call *webCall) tea.Cmd {
	a := call.args.(*webproto.KubeScopeArgs)
	return m.webKubeRun(call, a.Context, a.Namespace, func(ctx context.Context, c kube.Client) (any, error) {
		names, err := c.Namespaces(ctx)
		if err != nil {
			return nil, err
		}
		if len(names) > webproto.MaxKubeItems {
			names = names[:webproto.MaxKubeItems]
		}
		return webproto.KubeNamespaces{Namespaces: append([]string{}, names...)}, nil
	})
}

// webKubeMs is t as Unix ms (0: unknown).
func webKubeMs(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// webKubeList answers one namespace's pods, deployments, services and
// jobs; a kind that cannot be listed says why, a cluster that cannot be
// reached fails the whole answer.
func (m *Model) webKubeList(call *webCall) tea.Cmd {
	a := call.args.(*webproto.KubeScopeArgs)
	return m.webKubeRun(call, a.Context, a.Namespace, func(ctx context.Context, c kube.Client) (any, error) {
		out := webproto.KubeList{Context: c.Context, Namespace: c.Namespace, Pods: []webproto.KubePod{}, Deployments: []webproto.KubeDeployment{},
			Services: []webproto.KubeService{}, Jobs: []webproto.KubeJob{}}
		if out.Context == "" {
			out.Context, _ = c.CurrentContext(ctx)
		}
		if out.Namespace == "" {
			out.Namespace, _ = c.ContextNamespace(ctx)
		}
		fail := func(kind string, err error) error {
			if k := kube.KindOf(err); k == kube.ErrNoCluster || k == kube.ErrNoConfig || k == kube.ErrTimeout {
				return err
			}
			if out.Errors == nil {
				out.Errors = map[string]string{}
			}
			msg, _ := aiServicesRedact(webName(err.Error()))
			out.Errors[kind] = msg
			return nil
		}
		more := func(n int) bool {
			if n >= webproto.MaxKubeItems {
				out.More++
				return true
			}
			return false
		}
		pods, err := c.Pods(ctx)
		if err != nil {
			if err := fail("pods", err); err != nil {
				return nil, err
			}
		}
		for _, p := range pods {
			if more(len(out.Pods)) {
				continue
			}
			out.Pods = append(out.Pods, webproto.KubePod{Name: p.Name, Ready: p.Ready(), Status: webName(p.Status), Restarts: p.Restarts,
				Created: webKubeMs(p.Created), Containers: append([]string{}, p.Containers...)})
		}
		deps, err := c.Deployments(ctx)
		if err != nil {
			if err := fail("deployments", err); err != nil {
				return nil, err
			}
		}
		for _, d := range deps {
			if more(len(out.Deployments)) {
				continue
			}
			out.Deployments = append(out.Deployments, webproto.KubeDeployment{Name: d.Name, Ready: d.Ready(), Updated: d.Updated, Available: d.Available, Created: webKubeMs(d.Created)})
		}
		svcs, err := c.Services(ctx)
		if err != nil {
			if err := fail("services", err); err != nil {
				return nil, err
			}
		}
		for _, s := range svcs {
			if more(len(out.Services)) {
				continue
			}
			out.Services = append(out.Services, webproto.KubeService{Name: s.Name, Type: webName(s.Type), ClusterIP: webName(s.ClusterIP), Ports: s.PortsText(), Created: webKubeMs(s.Created)})
		}
		jobs, err := c.Jobs(ctx)
		if err != nil {
			if err := fail("jobs", err); err != nil {
				return nil, err
			}
		}
		for _, j := range jobs {
			if more(len(out.Jobs)) {
				continue
			}
			out.Jobs = append(out.Jobs, webproto.KubeJob{Name: j.Name, Status: j.Status(), Completions: j.Completions(), Failed: j.Failed, Created: webKubeMs(j.Created)})
		}
		return out, nil
	})
}

// webKubeText is text for the page: redacted, split in lines, capped
// (tail: the newest lines are kept, else the first ones).
func webKubeText(text string, tail bool) webproto.KubeText {
	text, n := aiServicesRedact(strings.TrimRight(text, "\n"))
	out := webproto.KubeText{Lines: []string{}, Redacted: n}
	if len(text) > webproto.MaxKubeText {
		out.Truncated = true
		if tail {
			text = text[len(text)-webproto.MaxKubeText:]
			if i := strings.IndexByte(text, '\n'); i >= 0 {
				text = text[i+1:]
			}
		} else {
			text = text[:webproto.MaxKubeText]
			if i := strings.LastIndexByte(text, '\n'); i >= 0 {
				text = text[:i]
			}
		}
	}
	if text != "" {
		for _, l := range strings.Split(text, "\n") {
			out.Lines = append(out.Lines, sanitizeOutputLine(l))
		}
	}
	return out
}

// webKubeLogs answers a pod's newest log lines.
func (m *Model) webKubeLogs(call *webCall) tea.Cmd {
	a := call.args.(*webproto.KubeLogsArgs)
	lines := a.Lines
	if lines == 0 {
		lines = webproto.DefaultKubeLog
	}
	return m.webKubeRun(call, a.Context, a.Namespace, func(ctx context.Context, c kube.Client) (any, error) {
		text, err := c.Logs(ctx, a.Pod, a.Container, lines)
		if err != nil {
			return nil, err
		}
		return webKubeText(text, true), nil
	})
}

// webKubeDescribe answers `kubectl describe` of one object.
func (m *Model) webKubeDescribe(call *webCall) tea.Cmd {
	a := call.args.(*webproto.KubeObjectArgs)
	return m.webKubeRun(call, a.Context, a.Namespace, func(ctx context.Context, c kube.Client) (any, error) {
		text, err := c.Describe(ctx, a.Kind, a.Name)
		if err != nil {
			return nil, err
		}
		return webKubeText(text, false), nil
	})
}

// webKubeRestart is a laptop card for `kubectl rollout restart` of a
// deployment, in the scope the page named (shown on the card).
func (m *Model) webKubeRestart(call *webCall) *webAsk {
	a := call.args.(*webproto.KubeObjectArgs)
	if a.Kind != "deployment" {
		call.fail(webproto.CodeBadRequest, "Only a deployment can be restarted.")
		return nil
	}
	c, ok := m.webKubeClient(call)
	if !ok {
		return nil
	}
	kctx, ns := a.Context, a.Namespace
	shown := func(s, def string) string {
		if s == "" {
			return def
		}
		return s
	}
	viewCtx, viewNS := c.Context, c.Namespace
	if kctx != "" && kctx != viewCtx {
		viewNS = ""
	}
	body := "This restarts every pod of " + a.Name + ", one by one.\n\n" +
		"Context: " + shown(shown(kctx, viewCtx), "kubectl's current") + " · Namespace: " + shown(shown(ns, viewNS), "the context's") + "\n" +
		"Command: kubectl rollout restart deployment/" + a.Name
	device := webName(call.conn.device)
	if device == "" {
		device = "web"
	}
	ask := &webAsk{summary: shown(kctx, viewCtx) + "/" + shown(ns, viewNS) + " deployment/" + a.Name, title: "Restart deployment?", action: "Restart", body: body}
	name := a.Name
	ask.run = func(m *Model) tea.Cmd {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), webKubeWait)
			defer cancel()
			sc, err := webKubeScope(ctx, c, kctx, ns)
			if err == nil {
				err = sc.RolloutRestart(ctx, name)
			}
			return applyMsg(func(m *Model) tea.Cmd {
				if err != nil {
					msg, _ := aiServicesRedact(webName(err.Error()))
					return m.servicesToast(toast.Errr, "Cannot restart "+name, msg)
				}
				var reload tea.Cmd
				if m.kube != nil && m.kube.visible {
					reload = m.kubeReload()
				}
				return tea.Batch(m.servicesToast(toast.Info, "Restarting "+name, "Asked from "+device+". Its pods are replaced one by one."), reload)
			})
		}
	}
	return ask
}
