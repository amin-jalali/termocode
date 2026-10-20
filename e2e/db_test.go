//go:build e2e

package e2e

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// DB-14: the Databases view in the real binary. SQLite runs through the
// real sqlite3 CLI (skipped when it is missing or older than 3.33); Redis
// talks to a small fake server on a loopback socket.

func dbWorkspace(t *testing.T, conns string) string {
	t.Helper()
	ws := NewRepo(t)
	if err := os.MkdirAll(filepath.Join(ws, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".termocode", "databases.json"), []byte(conns), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws
}

func openDatabases(tm *Term) {
	tm.t.Helper()
	tm.Palette("Databases: Focus")
	tm.WaitText("DATABASES", uiTimeout)
	tm.settle()
}

func requireSQLite(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not in PATH")
	}
	out, _ := exec.Command(bin, "-version").Output()
	f := strings.Fields(string(out))
	if len(f) == 0 {
		t.Skip("sqlite3 gives no version")
	}
	parts := strings.Split(f[0], ".")
	major, _ := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if major < 3 || (major == 3 && minor < 33) {
		t.Skipf("sqlite3 %s is older than 3.33", f[0])
	}
	return bin
}

// Spec: a workspace SQLite connection lists its tables and columns, the
// data view shows the real rows, and nothing in the file changes.
func TestDatabasesSQLiteBrowseAndOpenTable(t *testing.T) {
	bin := requireSQLite(t)
	ws := dbWorkspace(t, `{"connections":[{"id":"app","name":"app","driver":"sqlite","path":"app.db"}]}`)
	seed := exec.Command(bin, filepath.Join(ws, "app.db"),
		"CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL, plan TEXT);"+
			"INSERT INTO users (email, plan) VALUES ('ada@example.com','pro'),('grace@example.com','free');")
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("seed: %v\n%s", err, out)
	}
	before, _ := os.ReadFile(filepath.Join(ws, "app.db"))

	tm := Start(t, StartOpts{Cols: 120, Rows: 36, Workspace: ws})
	tm.settle()
	openDatabases(tm)
	tm.WaitText("app", uiTimeout)
	tm.Send(KeyEnter) // connect
	// "main" alone is also the git branch in the status bar: wait for the
	// database row of the tree.
	tm.WaitFor("the main database row", 10*time.Second, func(ls []string) bool {
		for _, l := range ls {
			if strings.Contains(l, "⛁ main") && !strings.Contains(l, "⌥") {
				return true
			}
		}
		return false
	})
	tm.Send(KeyDown)
	tm.settle()
	tm.Send(KeyEnter) // load the main database
	tm.WaitText("users", 10*time.Second)
	tm.CheckScreen("sqlite tree", mainFrame)
	if tm.Has("SUGGESTED") {
		t.Errorf("the saved app.db is suggested again:\n%s", tm.Screen())
	}
	// Go down to the table row and open its data.
	for i := 0; i < 6 && !cursorOn(tm, "users"); i++ {
		tm.Send(KeyDown)
		tm.settle()
	}
	if !cursorOn(tm, "users") {
		t.Fatalf("could not reach the users table:\n%s", tm.Screen())
	}
	tm.Send("o")
	tm.WaitText("ada@example.com", 10*time.Second)
	tm.WaitText("grace@example.com", uiTimeout)
	tm.CheckScreen("sqlite data view", mainFrame)
	if !tm.Has("pro") || !tm.Has("free") {
		t.Errorf("plan column missing from the data view:\n%s", tm.Screen())
	}
	after, _ := os.ReadFile(filepath.Join(ws, "app.db"))
	if string(before) != string(after) {
		t.Error("browsing changed the database file")
	}
}

// cursorOn reports whether the footer of the Databases sidebar names
// name (the selected row is repeated there, two rows above the bottom).
func cursorOn(tm *Term, name string) bool {
	ls := tm.Lines()
	for i := len(ls) - 1; i >= 0 && i >= len(ls)-4; i-- {
		if strings.Contains(ls[i], "  "+name) {
			return true
		}
	}
	return false
}

// fakeRedisTCP is a minimal Redis on a loopback socket: strings only.
type fakeRedisTCP struct {
	ln   net.Listener
	mu   sync.Mutex
	keys map[string]string
	log  []string
}

func newFakeRedisTCP(t *testing.T, keys map[string]string) *fakeRedisTCP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback socket: %v", err)
	}
	f := &fakeRedisTCP{ln: ln, keys: keys}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeRedisTCP) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeRedisTCP) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}

func readRESP(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "*")))
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		l, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		size, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(l, "$")))
		buf := make([]byte, size+2)
		if _, err := readFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:size]))
	}
	return args, nil
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := r.Read(buf[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func bulk(s string) string { return fmt.Sprintf("$%d\r\n%s\r\n", len(s), s) }

func (f *fakeRedisTCP) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	for {
		args, err := readRESP(r)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.log = append(f.log, strings.Join(args, " "))
		out := f.reply(args)
		f.mu.Unlock()
		if _, err := c.Write([]byte(out)); err != nil {
			return
		}
	}
}

func (f *fakeRedisTCP) reply(a []string) string {
	switch strings.ToUpper(a[0]) {
	case "PING":
		return "+PONG\r\n"
	case "SELECT", "AUTH", "CLIENT":
		return "+OK\r\n"
	case "INFO":
		return bulk(fmt.Sprintf("# Keyspace\r\ndb0:keys=%d,expires=0,avg_ttl=0\r\n", len(f.keys)))
	case "SCAN":
		var names []string
		for k := range f.keys {
			if ok, _ := path.Match(a[3], k); ok {
				names = append(names, k)
			}
		}
		out := fmt.Sprintf("*2\r\n%s*%d\r\n", bulk("0"), len(names))
		for _, k := range names {
			out += bulk(k)
		}
		return out
	case "TYPE":
		if _, ok := f.keys[a[1]]; ok {
			return "+string\r\n"
		}
		return "+none\r\n"
	case "PTTL":
		return ":-1\r\n"
	case "STRLEN":
		return fmt.Sprintf(":%d\r\n", len(f.keys[a[1]]))
	case "GETRANGE", "GET":
		return bulk(f.keys[a[1]])
	case "MEMORY":
		return ":64\r\n"
	}
	return "-ERR unknown command '" + a[0] + "'\r\n"
}

// Spec: a Redis connection lists the keys the server holds, a key opens
// as a value page, and only read commands reach the server.
func TestDatabasesRedisBrowseKeysReadOnly(t *testing.T) {
	f := newFakeRedisTCP(t, map[string]string{"session:ab12": "alice-token", "session:cd34": "bob-token", "greeting": "hello-world"})
	ws := dbWorkspace(t, fmt.Sprintf(`{"connections":[{"id":"cache","name":"cache","driver":"redis","host":"127.0.0.1","port":%d}]}`, f.port()))
	tm := Start(t, StartOpts{Cols: 120, Rows: 36, Workspace: ws})
	tm.settle()
	openDatabases(tm)
	tm.WaitText("cache", uiTimeout)
	tm.Send(KeyEnter) // connect
	tm.WaitText("db0", 10*time.Second)
	tm.Send(KeyDown)
	tm.settle()
	tm.Send(KeyEnter) // expand db0
	tm.WaitText("greeting", 10*time.Second)
	tm.CheckScreen("redis keys", mainFrame)
	tm.Send(KeyDown + KeyDown) // session: namespace, then greeting
	tm.settle()
	tm.Send(KeyEnter) // open the key
	tm.WaitText("hello-world", 10*time.Second)
	tm.CheckScreen("redis value", mainFrame)
	for _, c := range f.commands() {
		switch strings.ToUpper(strings.Fields(c)[0]) {
		case "SET", "DEL", "HSET", "EXPIRE", "FLUSHDB", "FLUSHALL", "RPUSH", "SADD", "ZADD", "XADD", "PERSIST", "UNLINK":
			t.Errorf("browsing sent a write command: %q", c)
		}
	}
}
