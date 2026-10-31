package mcp

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"
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
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), initTimeout+5*time.Second)
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
