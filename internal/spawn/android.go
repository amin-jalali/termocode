//go:build android

package spawn

import "os"

const onAndroid = true

// Fix os.Args before main and the flag parsing read it (see linkerArgs).
func init() {
	exe, _ := os.Executable()
	os.Args = linkerArgs(os.Args, exe, os.Getenv("TERMUX_EXEC__PROC_SELF_EXE"))
}
