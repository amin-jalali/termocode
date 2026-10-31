package app

import (
	"os"
	"runtime"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
)

// aiEnvInfo is what the chat's env block says (OMX-10; a var: the turn
// golden pins the OS and shell).
var aiEnvInfo = func() ai.EnvInfo {
	sh := os.Getenv("SHELL")
	if sh == "" && runtime.GOOS == "windows" {
		sh = os.Getenv("COMSPEC")
	}
	return ai.EnvInfo{Now: clock.Now(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Shell: sh}
}
