package dbclient

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// A jump host logs in with keys only; each reason a key cannot be used
// is told with the hop's name, and the bastion is never reached.
func TestTunnelJumpKeyProblemsNameTheHop(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, c *jumpChain)
		want  string
	}{
		{"passphrase", func(t *testing.T, c *jumpChain) {
			_, priv := newClientKey(t)
			writeKeyFile(t, c.keyPath, priv, "secret words", 0o600)
		}, "needs its passphrase: add it to ssh-agent with ssh-add"},
		{"missing", func(t *testing.T, c *jumpChain) { _ = os.Remove(c.keyPath) }, "no key found"},
		{"open to others", func(t *testing.T, c *jumpChain) { _ = os.Chmod(c.keyPath, 0o644) }, "chmod 600"},
		{"junk", func(t *testing.T, c *jumpChain) {
			_ = os.WriteFile(c.keyPath, []byte("not a key"), 0o600)
		}, "could not read the SSH key id_jump"},
		{"not accepted", func(t *testing.T, c *jumpChain) {
			other, _ := newClientKey(t)
			c.gw1.AuthorizedKeys = []ssh.PublicKey{other.PublicKey()}
		}, "the keys were not accepted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newJumpChain(t, jumpSSHConfig)
			c.trustAll(t)
			tc.setup(t, c)
			_, err := c.open(t)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "jump host gw1") {
				t.Fatalf("err = %v, want %q naming gw1", err, tc.want)
			}
			if c.bastion.AuthAttempts() != 0 {
				t.Fatal("the bastion was reached after a hop failed")
			}
		})
	}
}

// A jump host that cannot reach the next hop: the message names both.
func TestTunnelJumpCannotReachNextHop(t *testing.T) {
	c := newJumpChain(t, jumpSSHConfig)
	c.trustAll(t)
	c.gw1.Forward = func(context.Context, string) (net.Conn, error) { return nil, errors.New("no route") }
	_, err := c.open(t)
	if err == nil || !strings.Contains(err.Error(), "jump host gw1 could not reach gw2.internal:2222") {
		t.Fatalf("err = %v", err)
	}
}

// Jump hosts use the agent's keys when there is an agent.
func TestTunnelJumpUsesAgentKeys(t *testing.T) {
	c := newJumpChain(t, jumpSSHConfig)
	c.trustAll(t)
	_ = os.Remove(c.keyPath) // only the agent has the key
	dir, err := os.MkdirTemp("", "tcja")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	ln, err := net.Listen("unix", filepath.Join(dir, "a.sock"))
	if err != nil {
		t.Skipf("no unix sockets: %v", err)
	}
	defer ln.Close()
	keyring := agent.NewKeyring()
	signer, priv := newClientKey(t)
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatal(err)
	}
	c.gw1.AuthorizedKeys = []ssh.PublicKey{signer.PublicKey()}
	c.gw2.AuthorizedKeys = []ssh.PublicKey{signer.PublicKey()}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _ = agent.ServeAgent(keyring, conn); _ = conn.Close() }()
		}
	}()
	c.firstDial = ""
	tun, err := openTestTunnel(t, c.bastionCfg, TunnelOptions{Home: c.home, Password: c.bastionPw, Dial: c.dial, AgentSocket: ln.Addr().String()})
	if err != nil {
		t.Fatalf("open with agent: %v", err)
	}
	echoThrough(t, tun, c.dbAddr)
}

func TestParseJumpSpec(t *testing.T) {
	for spec, want := range map[string]string{
		"ssh://amy@[::1]:2222": "amy|::1|2222",
		"[fe80::1]":            "|fe80::1|0",
		"gw:22":                "|gw|22",
	} {
		u, h, p, err := parseJumpSpec(spec)
		if err != nil || u+"|"+h+"|"+strconv.Itoa(p) != want {
			t.Errorf("parseJumpSpec(%q) = %q %q %d %v", spec, u, h, p, err)
		}
	}
	for spec, want := range map[string]string{
		"[::1":        "missing ]",
		"gw:0":        "port is not valid",
		"gw:http":     "port is not valid",
		"a b":         "not a host name",
		"[::1]:99999": "port is not valid",
	} {
		if _, _, _, err := parseJumpSpec(spec); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("parseJumpSpec(%q) err = %v, want %q", spec, err, want)
		}
	}
}
