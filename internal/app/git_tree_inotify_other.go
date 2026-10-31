//go:build !linux

package app

// No file events here: the work tree watch uses the stat scan
// (git_tree_watch.go).

const gitTreeWatchSupported = false

func gitTreeWatchDirs(*gitPollGate, []string) bool { return false }

func gitTreeUnwatch(*gitPollGate) {}

func gitTreeOnNetworkFS(string) bool { return false }
