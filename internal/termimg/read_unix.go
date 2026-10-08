//go:build unix

package termimg

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// readSome waits up to timeout for input on f and reads what is there. Uses
// poll(2) so no goroutine is left blocked on the TTY after the probe (it
// would steal Bubble Tea's input).
func readSome(f *os.File, buf []byte, timeout time.Duration) (int, bool) {
	fd := int(f.Fd())
	ms := int(timeout / time.Millisecond)
	if ms < 1 {
		ms = 1
	}
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, ms)
	if err != nil || n <= 0 || fds[0].Revents&unix.POLLIN == 0 {
		return 0, false
	}
	r, err := unix.Read(fd, buf)
	if err != nil || r <= 0 {
		return 0, false
	}
	return r, true
}
