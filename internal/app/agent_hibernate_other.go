//go:build !unix

package app

import (
	"errors"
	"time"
)

// agentKillForeground is not built here: hibernation needs a Unix
// terminal's process groups.
func agentKillForeground(int, time.Duration) error {
	return errors.New("hibernate works on Linux and macOS only")
}
