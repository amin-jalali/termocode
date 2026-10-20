package app

import "github.com/amin-jalali/termocode/internal/dbclient"

// SetSSHForTest makes SSH tunnels read ~/.ssh from home and dial the SSH
// server through dial (an in-process server); the returned func restores
// the defaults.
func SetSSHForTest(home string, dial dbclient.DialFunc) func() {
	oldHome, oldDial := dbSSHHome, dbTunnelDial
	dbSSHHome = func() string { return home }
	dbTunnelDial = dial
	return func() { dbSSHHome, dbTunnelDial = oldHome, oldDial }
}
