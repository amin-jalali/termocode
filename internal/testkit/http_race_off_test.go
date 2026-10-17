//go:build !race

package testkit

// raceOn: time budgets are not checked under the race detector.
func raceOn() bool { return false }
