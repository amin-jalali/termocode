//go:build !unix

package main

import "net/http"

// pageHandler: the relay page needs the panel's asset server (unix only);
// elsewhere the relay serves only its WebSockets.
func pageHandler(bool) http.Handler { return nil }
