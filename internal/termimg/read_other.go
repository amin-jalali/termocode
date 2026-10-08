//go:build !unix

package termimg

import (
	"os"
	"time"
)

// readSome is not supported off unix: the probe falls back to environment
// hints.
func readSome(_ *os.File, _ []byte, _ time.Duration) (int, bool) { return 0, false }
