//go:build unix

package app

import "github.com/amin-jalali/termocode/internal/session"

// webProjectsSessions are the running sessions, from their Info files.
func webProjectsSessions() []session.Info {
	dir, err := session.Dir()
	if err != nil {
		return nil
	}
	return session.ReadInfos(dir, pidAlive)
}
