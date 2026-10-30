package webproto

import (
	"errors"
	"fmt"
)

// Limits of browser messages (ADR 0020). The gateway and the hub keep
// their own state limits (in-flight requests, rates); these are the ones a
// single message can be checked against.
const (
	MaxHello         = 4 << 10 // bytes of a whole hello message
	MaxDeviceName    = 64      // bytes
	MaxTopicsPerMsg  = 32      // topics in one sub / unsub
	MaxSubscriptions = 64      // live subscriptions per connection (also resume)
	MaxPingN         = 16      // bytes of ping.n
	MaxOpName        = 64      // bytes of req.op
)

// ErrInvalid is returned for a well-formed message that breaks a limit or
// a rule of its type.
var ErrInvalid = errors.New("webproto: invalid message")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// validator is implemented by browser messages with rules beyond JSON.
type validator interface {
	validate(size int) error
}

func (h *Hello) validate(size int) error {
	if size > MaxHello {
		return invalid("hello is %d bytes", size)
	}
	if len(h.DeviceName) > MaxDeviceName || hasControl(h.DeviceName) {
		return invalid("bad device name")
	}
	if h.Resume != nil {
		if h.Resume.Conn == "" || hasControl(h.Resume.Conn) {
			return invalid("resume without a connection id")
		}
		if len(h.Resume.Topics) > MaxSubscriptions {
			return invalid("resume has %d topics", len(h.Resume.Topics))
		}
		for name := range h.Resume.Topics {
			if _, err := ParseTopic(name); err != nil {
				return invalid("resume topic %q", name)
			}
		}
	}
	return nil
}

func (s *Sub) validate(int) error   { return validTopicList(s.Topics) }
func (u *Unsub) validate(int) error { return validTopicList(u.Topics) }

func validTopicList(topics []string) error {
	if len(topics) == 0 || len(topics) > MaxTopicsPerMsg {
		return invalid("%d topics", len(topics))
	}
	seen := make(map[string]bool, len(topics))
	for _, name := range topics {
		if _, err := ParseTopic(name); err != nil || seen[name] {
			return invalid("topic %q", name)
		}
		seen[name] = true
	}
	return nil
}

func (r *Req) validate(int) error {
	if r.ID == 0 {
		return invalid("request id 0")
	}
	if r.Op == "" || len(r.Op) > MaxOpName {
		return invalid("bad op name")
	}
	return nil
}

func (p *Ping) validate(int) error {
	if len(p.N) > MaxPingN {
		return invalid("ping n is %d bytes", len(p.N))
	}
	return nil
}

// hasControl reports a C0 control character or DEL.
func hasControl(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}
