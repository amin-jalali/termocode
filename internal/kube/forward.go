package kube

import (
	"bufio"
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Forward is one running `kubectl port-forward`. It lives until Stop (or
// until kubectl ends on its own: Done closes).
type Forward struct {
	Pod    string
	Remote int // the pod's port
	Local  int // the port on 127.0.0.1

	once   sync.Once
	cancel context.CancelFunc
	stop   func()
	done   chan struct{}
}

var forwardingRe = regexp.MustCompile(`Forwarding from 127\.0\.0\.1:(\d+) ->`)

// PortForward starts `kubectl port-forward pod/<pod> :<remote>`: kubectl
// picks a free local port, read from its first line. It waits up to the
// call timeout for that line.
func (c Client) PortForward(parent context.Context, pod string, remote int) (*Forward, error) {
	if c.Binary == "" {
		return nil, &Error{Kind: ErrNoKubectl}
	}
	args := append([]string{"port-forward"}, c.scope(true)...)
	args = append(args, "pod/"+pod, ":"+strconv.Itoa(remote))
	// sh joins stderr to stdout, so a failure can be read and shown;
	// "$0" "$@" pass the argv through without any quoting.
	argv := append([]string{"sh", "-c", `exec "$0" "$@" 2>&1`, c.Binary}, args...)
	ctx, cancel := context.WithCancel(context.Background())
	out, stop, err := c.exec().Stream(ctx, "", argv...)
	if err != nil {
		cancel()
		return nil, ErrorLine("", err)
	}
	fw := &Forward{Pod: pod, Remote: remote, cancel: cancel, stop: stop, done: make(chan struct{})}
	port := make(chan int, 1)
	var mu sync.Mutex
	var said strings.Builder
	go func() {
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
	}()
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
