//go:build !unix

package sdk

import "errors"

// workspaceSocket needs unix sockets; elsewhere only $TERMOCODE_SOCKET works.
func workspaceSocket(string) (string, error) {
	return "", errors.New("termocode sdk: finding a workspace's socket needs a unix system; set TERMOCODE_SOCKET")
}
