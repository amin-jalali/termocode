//go:build browser

package web

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Kubernetes (WEBP-4) against the fake hub.

func kubeList(ctx string) webproto.KubeList {
	now := time.Now().Add(-2 * time.Hour).UnixMilli()
	return webproto.KubeList{Context: ctx, Namespace: "shop",
		Pods: []webproto.KubePod{{Name: "api-7d9f-x2", Ready: "2/2", Status: "Running", Restarts: 1, Created: now, Containers: []string{"api", "sidecar"}},
			{Name: "worker-5c6b-q9", Ready: "0/1", Status: "CrashLoopBackOff", Restarts: 7, Created: now, Containers: []string{"worker"}}},
		Deployments: []webproto.KubeDeployment{{Name: "api", Ready: "1/1", Updated: 1, Available: 1, Created: now}},
		Services:    []webproto.KubeService{{Name: "api", Type: "ClusterIP", ClusterIP: "10.96.0.12", Ports: "80/TCP", Created: now}},
		Jobs:        []webproto.KubeJob{{Name: "migrate", Status: "Complete", Completions: "1/1", Created: now}},
	}
}

// kubeAnswer is the fake hub's kube reads; ok is false for other ops.
func kubeAnswer(r *webproto.Req) (any, bool) {
	switch r.Op {
	case webproto.OpKubeContexts:
		return webproto.KubeContexts{Contexts: []string{"dev", "prod"}, Current: "dev", Context: "dev", Namespace: "shop"}, true
	case webproto.OpKubeNamespaces:
		return webproto.KubeNamespaces{Namespaces: []string{"default", "shop"}}, true
	case webproto.OpKubeList:
		var a webproto.KubeScopeArgs
		_ = json.Unmarshal(r.Args, &a)
		return kubeList(a.Context), true
	case webproto.OpKubeLogs:
		return webproto.KubeText{Lines: []string{"listening on :8080", "<b>GET /health 200</b>"}}, true
	case webproto.OpKubeDescribe:
		return webproto.KubeText{Lines: []string{"Name:         api", "Namespace:    shop"}}, true
	}
	return nil, false
}

// kubeUntil answers the page's kube reads until it asks op; that one it
// returns unanswered (args checked).
func kubeUntil(t *testing.T, c *HubConn, op, args string) *webproto.Req {
	t.Helper()
	for {
		r, ok := c.Next(t).(*webproto.Req)
		if !ok {
			continue
		}
		if r.Op == op && (args == "" || canonJSON(t, r.Args) == canonJSON(t, []byte(args))) {
			return r
		}
		d, ok := kubeAnswer(r)
		if !ok {
			t.Fatalf("page sent %s; want %s %s", show(r), op, args)
		}
		reply(t, c, r, d)
	}
}

func TestKubeViewReadsAndRestarts(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			c := p.Login("c-kube")
			if vp.W >= 1200 {
				p.Tap(`nav .act-item[data-act="kube"]`)
			} else {
				p.GoTab("more")
				p.Tap(`.more-link[data-sub="kube"]`)
			}
			r := kubeUntil(t, c, webproto.OpKubeList, `{"context":"dev","namespace":"shop"}`)
			reply(t, c, r, kubeList("dev"))
			p.WaitJS(`document.querySelectorAll(".kb-row").length === 5`)
			p.Tap(`.kb-row[data-kube="pod/api-7d9f-x2"]`)
			reply(t, c, kubeUntil(t, c, webproto.OpKubeLogs, `{"context":"dev","namespace":"shop","pod":"api-7d9f-x2","container":"api"}`), webproto.KubeText{Lines: []string{"listening", "<b>x</b>"}})
			p.WaitJS(`document.querySelectorAll(".kb-text .trow").length === 2`)
			if Eval[int](p, `document.querySelectorAll(".kb-text b").length`) != 0 {
				t.Fatal("logs became HTML")
			}
			p.Tap(`.kb-row[data-kube="deployment/api"]`)
			reply(t, c, kubeUntil(t, c, webproto.OpKubeDescribe, `{"context":"dev","namespace":"shop","kind":"deployment","name":"api"}`), webproto.KubeText{Lines: []string{"Name: api"}})
			p.Tap(`.kb-bar [data-act="restart"]`)
			p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
			p.TapJS(ButtonByText("Ask the laptop"))
			reply(t, c, kubeUntil(t, c, webproto.OpKubeRestart, `{"context":"dev","namespace":"shop","kind":"deployment","name":"api"}`), webproto.AskRef{Card: "k-1"})
			Eval[bool](p, `(document.querySelector('.kb-sel').value = "prod", document.querySelector('.kb-sel').dispatchEvent(new Event("change", {bubbles: true})), true)`)
			reply(t, c, kubeUntil(t, c, webproto.OpKubeList, `{"context":"prod","namespace":""}`), kubeList("prod"))
			if len(p.Problems()) > 0 {
				t.Errorf("problems: %v", p.Problems())
			}
		})
	}
}
