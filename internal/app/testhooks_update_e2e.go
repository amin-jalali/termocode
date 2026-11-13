//go:build e2etest

package app

import (
	"crypto/tls"
	"net/http"
	"os"
	"time"

	"github.com/amin-jalali/termocode/internal/update"
)

// A test-only hook for the real-binary e2e tests (e2e/update_test.go):
// with TERMOCODE_E2E_UPDATE_URL set, the update code asks that (https)
// address for the latest release instead of GitHub and trusts the fake
// server's self-signed certificate. It exists only in binaries built with
// `-tags e2etest`; a release build has neither the variable nor this code.
func init() {
	u := os.Getenv("TERMOCODE_E2E_UPDATE_URL")
	if u == "" {
		return
	}
	prev := updateDepsFor
	updateDepsFor = func() updateDeps {
		d := prev()
		d.client = update.Client{
			HTTP: &http.Client{
				Timeout:   30 * time.Second,
				Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec // fake release server of a test
			},
			LatestURL: u,
			Version:   termocodeVersion,
		}
		return d
	}
}
