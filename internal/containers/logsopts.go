package containers

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
)

// LogsOptions shape a followed container log in a terminal tab.
type LogsOptions struct {
	Tail       int    // 0 = 200 (the default), -1 = all lines
	Since      string // Go duration ("10m") or RFC 3339 date/time; "" = no limit
	Timestamps bool
}

const logsMaxTail = 100000

// ParseLogsSince checks a since value so only a duration or a date/time
// reaches the engine argv.
func ParseLogsSince(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		if d <= 0 {
			return "", fmt.Errorf("since must be a positive duration like 10m")
		}
		return s, nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if _, err := time.Parse(layout, s); err == nil {
			return s, nil
		}
	}
	return "", fmt.Errorf("since %q is not a duration (10m, 2h) or a date (2026-11-07, 2026-11-07T10:00:00Z)", s)
}

// LogsFollowWith is LogsFollow with tail, since and timestamps options.
func (e *Engine) LogsFollowWith(id string, o LogsOptions) (services.Command, error) {
	tail := "200"
	switch {
	case o.Tail < 0:
		tail = "all"
	case o.Tail > logsMaxTail:
		return services.Command{}, fmt.Errorf("tail must be at most %d lines", logsMaxTail)
	case o.Tail > 0:
		tail = strconv.Itoa(o.Tail)
	}
	since, err := ParseLogsSince(o.Since)
	if err != nil {
		return services.Command{}, err
	}
	argv := []string{e.Binary, "logs", "-f", "--tail", tail}
	if o.Timestamps {
		argv = append(argv, "--timestamps")
	}
	if since != "" {
		argv = append(argv, "--since", since)
	}
	argv = append(argv, id)
	return services.Command{Label: "logs: " + id, Argv: argv}, nil
}
