package mcp

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
)

// Reconnects (RT-5, RT-6, RT-9): a remote server that can not be reached
// tries again by itself, later each time; "Reconnect all" tries every
// failed server now.

// Backoff is the wait before reconnect try number try (0 first): base
// doubled per try, at most limit, then jittered into [d/2, d]. rnd(n)
// returns a number in [0, n) (nil: math/rand).
func Backoff(try int, base, limit time.Duration, rnd func(int64) int64) time.Duration {
	if rnd == nil {
		rnd = rand.Int64N
	}
	d := base
	for i := 0; i < try && d < limit; i++ {
		d *= 2
	}
	if d > limit || d <= 0 {
		d = limit
	}
	half := d / 2
	return half + time.Duration(rnd(int64(d-half)+1))
}

// Reconnect defaults (ManagerOptions.Reconnect*).
const (
	defaultReconnectBase = 2 * time.Second
	defaultReconnectCap  = 2 * time.Minute
	defaultReconnectMax  = 8
)

// retryable reports a failure that may pass by itself: no connection,
// no answer in time, a 5xx / 408 / 429. Auth answers, missing secrets
// and trust never are (the user has to act).
func retryable(err error) bool {
	var ae *AuthError
	var ne *NeedsError
	switch {
	case err == nil, errors.As(err, &ae), errors.As(err, &ne), errors.Is(err, ErrSignInNeeded), errors.Is(err, ErrNotTrusted):
		return false
	}
	var ce *ConnError
	if errors.As(err, &ce) {
		return true
	}
	var se *StatusError
	if errors.As(err, &se) {
		return se.Code >= 500 || se.Code == http.StatusRequestTimeout || se.Code == http.StatusTooManyRequests
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrCallTimeout)
}

// StateText is the state as the servers list shows it: "reconnecting
// in 4s" while a reconnect waits.
func (s Status) StateText() string {
	if s.State == StateReconnecting {
		sec := int((s.RetryIn + time.Second - 1) / time.Second)
		if sec < 1 {
			sec = 1
		}
		return fmt.Sprintf("reconnecting in %ds", sec)
	}
	return s.State
}

// scheduleReconnect arms the next try of a remote server that failed
// with err, or gives up after ReconnectMax tries.
func (m *Manager) scheduleReconnect(st *serverState, err error) {
	var msg string
	defer func() { // after the unlock below
		if msg != "" {
			m.log(st.cfg.Name, "%s", msg)
		}
	}()
	m.mu.Lock()
	defer m.mu.Unlock()
	base, limit, max := m.opts.ReconnectBase, m.opts.ReconnectCap, m.opts.ReconnectMax
	if m.closed || base < 0 || m.servers[st.cfg.Name] != st {
		return
	}
	if base == 0 {
		base = defaultReconnectBase
	}
	if limit <= 0 {
		limit = defaultReconnectCap
	}
	if max <= 0 {
		max = defaultReconnectMax
	}
	m.stopRetryLocked(st)
	if st.retryN >= max {
		st.state = StateFailed
		st.problem = fmt.Sprintf("%v (gave up after %d tries; AI: MCP: Reconnect All tries again)", err, st.retryN)
		st.retryN = 0
		msg = "no more reconnect tries"
		return
	}
	d := Backoff(st.retryN, base, limit, nil)
	st.retryN++
	st.state, st.problem = StateReconnecting, err.Error()
	st.retryAt = time.Now().Add(d)
	st.retryGen++
	gen := st.retryGen
	st.retryTimer = time.AfterFunc(d, func() { m.retryNow(st, gen) })
	msg = fmt.Sprintf("reconnecting in %v (try %d of %d)", d.Round(time.Second/10), st.retryN, max)
}

// stopRetryLocked ends a waiting reconnect (caller holds m.mu).
func (m *Manager) stopRetryLocked(st *serverState) {
	if st.retryTimer != nil {
		st.retryTimer.Stop()
		st.retryTimer = nil
	}
	st.retryGen++
}

// retryNow is one reconnect try (the timer's goroutine).
func (m *Manager) retryNow(st *serverState, gen int) {
	m.mu.Lock()
	if m.closed || gen != st.retryGen || st.state != StateReconnecting || m.servers[st.cfg.Name] != st {
		m.mu.Unlock()
		return
	}
	st.retryTimer = nil
	m.retrying.Add(1) // Close waits for it
	m.mu.Unlock()
	defer m.retrying.Done()
	ctx, cancel := context.WithTimeout(m.life, initTimeout+5*time.Second)
	defer cancel()
	if _, err := m.list(ctx, st, nil); err == nil {
		m.log(st.cfg.Name, "reconnected")
	}
}

// isConnErr: the network is gone (not a server answer).
func isConnErr(err error) bool {
	var ce *ConnError
	return errors.As(err, &ce)
}

// ReconnectResult says what ReconnectAll did.
type ReconnectResult struct {
	Tried, OK int      // servers that were down and were tried
	Checked   int      // running servers that answered the ping
	Failed    []string // names still down, sorted
}

// ReconnectAll tries every failed or waiting server now (the user's
// "Reconnect all"), and pings the running ones: one that does not answer
// is connected again. Servers that are off, invalid or need a sign-in
// are left alone; nothing asks for trust.
func (m *Manager) ReconnectAll(ctx context.Context) ReconnectResult {
	type job struct {
		st   *serverState
		proc *Process
	}
	m.mu.Lock()
	var jobs []job
	for _, st := range m.servers {
		switch {
		case st.proc != nil:
			jobs = append(jobs, job{st, st.proc})
		case st.state == StateFailed || st.state == StateReconnecting:
			m.stopRetryLocked(st)
			st.retryN = 0
			jobs = append(jobs, job{st: st})
		}
	}
	m.mu.Unlock()
	var mu sync.Mutex
	var res ReconnectResult
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		crash.Go("an MCP reconnect", func() {
			defer wg.Done()
			name := j.st.cfg.Name
			if j.proc != nil {
				pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := j.proc.Client.Ping(pctx)
				cancel()
				if err == nil {
					mu.Lock()
					res.Checked++
					mu.Unlock()
					return
				}
				m.log(name, "no answer to a ping: connecting again")
				m.mu.Lock()
				if j.st.proc == j.proc {
					j.st.proc, j.st.state = nil, StateStopped
				}
				m.mu.Unlock()
				crash.Go("an MCP server stop", func() { j.proc.Close() })
			}
			lctx, cancel := context.WithTimeout(ctx, initTimeout+5*time.Second)
			_, err := m.list(lctx, j.st, nil)
			cancel()
			mu.Lock()
			defer mu.Unlock()
			res.Tried++
			if err == nil {
				res.OK++
			} else {
				res.Failed = append(res.Failed, name)
			}
		})
	}
	wg.Wait()
	sort.Strings(res.Failed)
	return res
}

// idempotent: the tool says it is read-only or idempotent (safe to run
// again after a dropped connection).
func (m *Manager) idempotent(st *serverState, tool string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range st.tools {
		if t.Name == tool {
			a := t.Annotations
			return t.ReadOnly() || (a != nil && a.IdempotentHint != nil && *a.IdempotentHint)
		}
	}
	return false
}
