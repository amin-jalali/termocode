//go:build !unix

package bench

import "os"

// killGroup kills the program (no process groups here).
func killGroup(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}

// alive is unknown without signal 0; the bench runs on unix only.
func alive(int) bool { return false }
