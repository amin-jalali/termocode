package app

import (
	"crypto/ed25519"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/crypto/ssh"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// databaseSSHDemo is the Databases tree with a staging connection behind
// an SSH tunnel, plus one of the DB-11 overlays (dbdemo ssh-host,
// ssh-form, ssl-form). Nothing is dialled.
func (m *Model) databaseSSHDemo(variant string) tea.Cmd {
	m.databasesDemo("")
	s := m.databases
	pg := dbDemoConnection()
	staging := dbclient.Conn{ID: "staging", Name: "staging", Driver: "mysql", Host: "staging.internal", Port: 3306, Environment: "staging",
		SSH: &dbclient.SSHConfig{Host: "bastion.example.com", User: "deploy", Auth: "agent"},
		SSL: &dbclient.SSLConfig{Mode: "verify-full", CA: "~/.certs/staging-ca.pem"}}
	s.conns = []dbclient.Conn{pg, staging}
	s.nodes = map[string]dbNode{}
	s.nodes[pg.ID] = dbNode{kind: dbNodeConnection, name: pg.Name, path: pg.ID, conn: pg.ID, detail: dbConnDetail(pg)}
	s.nodes[staging.ID] = dbNode{kind: dbNodeConnection, name: staging.Name, path: staging.ID, conn: staging.ID, detail: dbConnDetail(staging)}
	s.folded[pg.ID] = true
	s.cursor = 1
	s.touch()
	switch variant {
	case "ssh-host":
		// A fixed key: the fingerprint is the same in every shot.
		seed := make([]byte, ed25519.SeedSize)
		for i := range seed {
			seed[i] = byte(i * 7)
		}
		pub, _ := ssh.NewPublicKey(ed25519.NewKeyFromSeed(seed).Public())
		err := &dbclient.UnknownHostKeyError{Host: "bastion.example.com:22", KeyType: pub.Type(), Fingerprint: ssh.FingerprintSHA256(pub), KnownHosts: filepath.Join(dbSSHHome(), ".ssh", "known_hosts"), Key: pub}
		n := s.nodes[staging.ID]
		n.loading = false
		s.nodes[staging.ID] = n
		cmd, _ := m.databaseTunnelError(err, staging.ID, dbThenConnect)
		return cmd
	case "ssh-form":
		return m.databaseSSHStart(staging.ID)
	case "ssl-form":
		return m.databaseSSLStart(staging.ID)
	}
	return nil
}
