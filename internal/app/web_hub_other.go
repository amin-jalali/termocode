//go:build !unix

package app

import (
	"errors"
	"net"
)

var errWebHubOS = errors.New("the web hub needs unix sockets")

// errWebHubBusy: another termocode already serves the web hub here.
var errWebHubBusy = errors.New("another termocode already serves the web for this folder")

func webHubSocketPath(string) (string, error) { return "", errWebHubOS }

var webHubListen = func(string) (net.Listener, error) { return nil, errWebHubOS }

var webHubPeer = func(net.Conn) error { return errWebHubOS }
