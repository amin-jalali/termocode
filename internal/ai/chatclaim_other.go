//go:build !unix

package ai

import "os"

// lockChat: no flock here; every claim succeeds (one window per chat is
// not enforced).
func lockChat(string) (*os.File, error) { return nil, nil }
