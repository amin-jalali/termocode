package clock

import (
	"sync"
	"testing"
	"time"
)

// TestSetWhileReading: a test may freeze or restore the clock while a
// background command still reads it (a git refresh started by the test).
// Run with -race: swapping must not race with Now.
func TestSetWhileReading(t *testing.T) {
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = Now()
			}
		}
	}()
	for i := 0; i < 100; i++ {
		restore := Set(func() time.Time { return fixed })
		if !Now().Equal(fixed) {
			t.Fatal("Set did not freeze Now")
		}
		restore()
	}
	close(stop)
	wg.Wait()
	if Now().Equal(fixed) {
		t.Error("restore did not bring back the real clock")
	}
}
