//go:build !unix

package app

import (
	"crypto/sha256"
	"errors"
)

var errWebPanelOS = errors.New("the web panel needs unix sockets")

func webNewLinkSecret() (string, [sha256.Size]byte, error) {
	return "", [sha256.Size]byte{}, errWebPanelOS
}

func webWriteRedirect(string, string) error { return errWebPanelOS }

func webRedirectPath() (string, error) { return "", errWebPanelOS }

var webHTTPSocketPath = func() (string, error) { return "", errWebPanelOS }

func startWebGatewayProcess(*webHub, []string) (*webGatewayProc, error) {
	return nil, errWebPanelOS
}
