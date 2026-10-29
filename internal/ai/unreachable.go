package ai

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/amin-jalali/termocode/internal/netmsg"
)

// unreachablePrefix starts every "could not reach the AI server" error.
const unreachablePrefix = "No connection to the AI server"

// IsUnreachable reports whether an error text says the AI server could
// not be reached (no network, refused, name not found, no answer).
func IsUnreachable(text string) bool { return strings.HasPrefix(text, unreachablePrefix) }

// requestError is the error of a failed HTTP request to the AI server:
// a network failure in plain words with the host, anything else (a stop
// by the user, a bad URL) as it was. The text is redacted.
func requestError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return errString(Redact(err.Error()))
	}
	var ue *url.Error
	why, ok := netmsg.Why(err)
	if !errors.As(err, &ue) || !ok {
		return errString(Redact(err.Error()))
	}
	host := ue.URL
	if u, perr := url.Parse(ue.URL); perr == nil && u.Host != "" {
		host = u.Host
	}
	return errString(Redact(unreachablePrefix + " (" + host + "): " + why))
}
