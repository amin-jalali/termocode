package kube

import (
	"bufio"
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/lazyre"
)

// Forward is one running `kubectl port-forward`. It lives until Stop (or
// until kubectl ends on its own: Done closes).
type Forward struct {
	Kind   string // "pod", "svc" or "deployment"
	Pod    string // the object's name (a pod, service or deployment)
	Remote int    // the object's port
	Local  int    // the port on 127.0.0.1

	once   sync.Once
	cancel context.CancelFunc
	stop   func()
	done   chan struct{}
}

var forwardingRe = lazyre.New(`Forwarding from 127\.0\.0\.1:(\d+) ->`)

// Target is kubectl's name of the forwarded object: "svc/web".
func (f *Forward) Target() string { return f.Kind + "/" + f.Pod }

// PortForward starts `kubectl port-forward pod/<pod> :<remote>`: kubectl
// picks a free local port, read from its first line. It waits up to the
// call timeout for that line.
func (c Client) PortForward(parent context.Context, pod string, remote int) (*Forward, error) {
	return c.PortForwardTo(parent, "pod", pod, remote)
}

// PortForwardTo is PortForward to a pod ("pod"), a service ("svc") or a
// deployment ("deployment"): kubectl picks one of its pods.
func (c Client) PortForwardTo(parent context.Context, kind, name string, remote int) (*Forward, error) {
	if c.Binary == "" {
		return nil, &Error{Kind: ErrNoKubectl}
	}
	switch kind {
	case "pod", "svc", "deployment":
	default:
		return nil, ErrorLine("cannot port-forward a "+kind, nil)
	}
	args := append([]string{"port-forward"}, c.scope(true)...)
	args = append(args, kind+"/"+name, ":"+strconv.Itoa(remote))
	// sh joins stderr to stdout, so a failure can be read and shown;
	// "$0" "$@" pass the argv through without any quoting.
	argv := append([]string{"sh", "-c", `exec "$0" "$@" 2>&1`, c.Binary}, args...)
	ctx, cancel := context.WithCancel(context.Background())
	out, stop, err := c.exec().Stream(ctx, "", argv...)
	if err != nil {
		cancel()
		return nil, ErrorLine("", err)
	}
	fw := &Forward{Kind: kind, Pod: name, Remote: remote, cancel: cancel, stop: stop, done: make(chan struct{})}
	port := make(chan int, 1)
	var mu sync.Mutex
	var said strings.Builder
	crash.GoWith("kubectl port-forward", func() {
		defer close(fw.done)
		s := bufio.NewScanner(out)
		found := false
		for s.Scan() {
			line := s.Text()
			if !found {
				if m := forwardingRe.FindStringSubmatch(line); m != nil {
					n, _ := strconv.Atoi(m[1])
					found = true
					port <- n
					continue
				}
				mu.Lock()
				said.WriteString(line + "\n")
				mu.Unlock()
			}
		}
		if !found {
			close(port)
		}
	}, cancel)
	d := c.Timeout
	if d <= 0 {
		d = DefaultTimeout
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case n, ok := <-port:
		if ok {
			fw.Local = n
			return fw, nil
		}
		fw.Stop()
		mu.Lock()
		text := said.String()
		mu.Unlock()
		if strings.TrimSpace(text) == "" {
			text = "port-forward stopped"
		}
		return nil, ErrorLine(text, nil)
	case <-timer.C:
		fw.Stop()
		return nil, &Error{Kind: ErrTimeout}
	case <-parent.Done():
		fw.Stop()
		return nil, parent.Err()
	}
}

// Done closes when kubectl has ended (Stop, or the pod went away).
func (f *Forward) Done() <-chan struct{} { return f.done }

// Stop ends kubectl and waits for it; safe to call more than once.
func (f *Forward) Stop() {
	f.once.Do(func() {
		f.cancel()
		f.stop()
	})
}
