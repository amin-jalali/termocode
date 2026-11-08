package dbclient

import (
	"context"
	"errors"
)

// ContainerRef is the container behind a connection on a remote engine
// (a docker context or podman connection over ssh://). The container's
// IP changes when it is recreated, so it is found again by Name every
// time the connection opens.
type ContainerRef struct {
	Name    string `json:"name"`
	Context string `json:"context,omitempty"` // the docker context / podman connection
	Engine  string `json:"engine,omitempty"`  // docker (default) or podman
	Network string `json:"network,omitempty"` // the network whose IP is used ("": the first)
}

// ContainerAddress is the IP of ref's container as seen from t's server.
func ContainerAddress(ctx context.Context, t *Tunnel, ref ContainerRef) (string, error) {
	return "", errors.New("not built yet")
}
