//go:build !unix

package app

// No persistent sessions here (MOB-51): the door never starts a project
// on this system, so nothing is attached and nothing stops by itself.
var (
	webDoorAttached = func() bool { return false }
	webDoorQuit     = func() {}
)
