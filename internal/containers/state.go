package containers

import "strings"

// StateFromPS normalizes the state and status words returned by docker/podman ps.
func StateFromPS(state, status string) string {
	s := strings.ToLower(strings.TrimSpace(state))
	if s == "" {
		s = strings.ToLower(strings.TrimSpace(status))
	}
	switch {
	case strings.Contains(s, "restarting"):
		return "restarting"
	case strings.Contains(s, "paused"):
		return "paused"
	case strings.Contains(s, "created"):
		return "created"
	case strings.Contains(s, "running"), strings.HasPrefix(s, "up "):
		return "running"
	case strings.Contains(s, "exited"), strings.Contains(s, "stopped"):
		return "exited"
	case strings.Contains(s, "dead"):
		return "dead"
	case strings.Contains(s, "removing"):
		return "removing"
	case s == "" || s == "unknown":
		return "unknown"
	default:
		return s
	}
}
