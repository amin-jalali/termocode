//go:build !unix

package app

import "github.com/amin-jalali/termocode/internal/session"

// webProjectsSessions: sessions run on unix only.
func webProjectsSessions() []session.Info { return nil }
