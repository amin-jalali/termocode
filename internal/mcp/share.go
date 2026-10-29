package mcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

// Shared stdio servers (G-MCP): one server process per config and user,
// used by every termocode session on the machine. The first session that
// needs a server starts a small broker — the termocode binary itself with
// ShareEnv set — that owns the server and listens on a private unix
// socket; later sessions connect to that socket. The broker multiplexes
// the JSON-RPC traffic and stops the server a grace period after the
// last session left. Where this is not possible (no unix sockets, an
// error), a session runs its own process as before.
//
// Files: share.go (key, options), share_unix.go (socket dir, dial or
// spawn), share_broker_unix.go (the broker), share_other.go (fallback).

// ShareEnv turns a termocode binary into a share broker: its value is the
// socket path. RunShareIfHelper checks it at the top of main.
const ShareEnv = "TERMOCODE_MCP_SHARE"

// DefaultShareGrace is how long a shared server stays after the last
// session left.
const DefaultShareGrace = 30 * time.Second

// ShareOptions turn on shared stdio servers (ManagerOptions.Share).
type ShareOptions struct {
	// Exe and Args start the broker: the termocode binary (a test binary
	// passes "-test.run=^$" and calls RunShareIfHelper in TestMain).
	Exe  string
	Args []string
	// Dir holds the sockets ("" = <session dir>/mcp). It must be private.
	Dir string
	// Grace is how long the server stays after the last session left
	// (0 = DefaultShareGrace).
	Grace time.Duration
}

// shareConfig is what the broker needs to run one server. It goes to the
// broker over its stdin pipe — never argv, never a file: Env can hold
// secrets.
type shareConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Cwd     string            `json:"cwd"`
	Roots   []string          `json:"roots"`
	Source  string            `json:"source"`
	GraceMS int64             `json:"grace_ms"`
}

// shareKey names the socket of one server config: an HMAC with a private
// per-user salt, so the file name tells nothing about the secrets in the
// env (not even to a brute force). The same command, args, env (names
// and values), folder, roots and source give the same key; anything else
// gives another process.
func shareKey(salt []byte, c shareConfig) string {
	envKeys := make([]string, 0, len(c.Env))
	for k := range c.Env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	env := make([][2]string, 0, len(envKeys))
	for _, k := range envKeys {
		env = append(env, [2]string{k, c.Env[k]})
	}
	roots := append([]string(nil), c.Roots...)
	sort.Strings(roots)
	b, _ := json.Marshal(struct {
		Command string      `json:"c"`
		Args    []string    `json:"a"`
		Env     [][2]string `json:"e"`
		Cwd     string      `json:"d"`
		Roots   []string    `json:"r"`
		Source  string      `json:"s"`
	}{c.Command, c.Args, env, c.Cwd, roots, c.Source})
	mac := hmac.New(sha256.New, salt)
	mac.Write(b)
	return hex.EncodeToString(mac.Sum(nil))[:32]
}
