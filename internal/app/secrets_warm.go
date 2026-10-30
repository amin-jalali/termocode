package app

import "github.com/amin-jalali/termocode/internal/crash"

// WarmSecretStore opens the secret store in the background, so the AI
// state built in New finds it open. The first start writes the store's
// index file (fsync, a few ms) and on a desktop may ask the OS keyring;
// main calls this before it probes the terminal, so that time overlaps
// the rest of startup. The returned func waits until it is open.
func WarmSecretStore() (wait func()) {
	done := make(chan struct{})
	crash.Go("opening the secret store", func() {
		defer close(done) // also after a panic, so the wait never hangs
		_, _ = configureSecretStore()
	})
	return func() { <-done }
}
