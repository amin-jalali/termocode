package dbclient

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgproto3"
)

// fakePostgres is a PostgreSQL server on a random local port that knows
// the startup and simple queries. With authErr set it refuses the login
// like a server with a wrong password.
type fakePostgres struct {
	ln      net.Listener
	authErr *pgproto3.ErrorResponse
	queries atomic.Int32
}

func startFakePostgres(t *testing.T, authErr *pgproto3.ErrorResponse) *fakePostgres {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakePostgres{ln: ln, authErr: authErr}
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

func (f *fakePostgres) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakePostgres) serve(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	be := pgproto3.NewBackend(c, c)
	msg, err := be.ReceiveStartupMessage()
	if err != nil {
		return
	}
	if _, ok := msg.(*pgproto3.SSLRequest); ok {
		if _, err := c.Write([]byte{'N'}); err != nil {
			return
		}
		if _, err := be.ReceiveStartupMessage(); err != nil {
			return
		}
	}
	if f.authErr != nil {
		be.Send(f.authErr)
		_ = be.Flush()
		return
	}
	be.Send(&pgproto3.AuthenticationOk{})
	be.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "16.4"})
	be.Send(&pgproto3.BackendKeyData{ProcessID: 1, SecretKey: []byte{0, 0, 0, 2}})
	be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if be.Flush() != nil {
		return
	}
	for {
		msg, err := be.Receive()
		if err != nil {
			return
		}
		switch msg.(type) {
		case *pgproto3.Query:
			f.queries.Add(1)
			be.Send(&pgproto3.EmptyQueryResponse{})
			be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
			if be.Flush() != nil {
				return
			}
		case *pgproto3.Terminate:
			return
		}
	}
}

// fakeMySQL is a MySQL server on a random local port that refuses every
// login with an error packet (1045 Access denied), as a real server does
// for a wrong password. accepted counts the connections it saw.
func startFakeMySQL(t *testing.T) (port int, accepted *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted = &atomic.Int32{}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			msg := "Access denied for user 'app'@'localhost' (using password: YES)"
			payload := []byte{0xff, 0, 0, '#', '2', '8', '0', '0', '0'}
			binary.LittleEndian.PutUint16(payload[1:3], 1045)
			payload = append(payload, msg...)
			head := []byte{byte(len(payload)), byte(len(payload) >> 8), byte(len(payload) >> 16), 0}
			_, _ = c.Write(append(head, payload...))
			_ = c.Close()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, accepted
}

// closedPort is a local port with nothing listening (connection refused).
func closedPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func connectCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestNewSQLSessionConnectionRefused(t *testing.T) {
	port := closedPort(t)
	for _, open := range []func(context.Context, Conn, string) (*SQLSession, error){NewPostgresSession, NewMySQLSession} {
		conn := Conn{Host: "127.0.0.1", Port: port, User: "app", Database: "db", Params: map[string]string{"sslmode": "disable"}}
		s, err := open(connectCtx(t), conn, "s3cret-pw")
		if err == nil {
			_ = s.Close()
			t.Fatal("connected to a closed port")
		}
		if !strings.Contains(err.Error(), "refused") || strings.Contains(err.Error(), "s3cret-pw") {
			t.Errorf("refused error = %q", err)
		}
	}
}

func TestNewSQLSessionRejectsUnknownDriver(t *testing.T) {
	if _, err := NewSQLSession(context.Background(), Conn{Driver: "oracle"}, ""); err == nil || !strings.Contains(err.Error(), `unsupported SQL driver "oracle"`) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := openSQLDB(Conn{Driver: "oracle"}, ""); err == nil {
		t.Fatal("openSQLDB took an unknown driver")
	}
}

// A wrong password: the server's reason reaches the user, the password
// does not.
func TestNewSQLSessionAuthFailure(t *testing.T) {
	pg := startFakePostgres(t, &pgproto3.ErrorResponse{Severity: "FATAL", Code: "28P01", Message: `password authentication failed for user "app"`})
	conn := Conn{Driver: "postgres", Host: "127.0.0.1", Port: pg.port(), User: "app", Database: "db", Params: map[string]string{"sslmode": "disable"}}
	_, err := NewSQLSession(connectCtx(t), conn, "s3cret-pw")
	if err == nil || !strings.Contains(err.Error(), "password authentication failed") || strings.Contains(err.Error(), "s3cret-pw") {
		t.Errorf("postgres auth error = %v", err)
	}
	port, _ := startFakeMySQL(t)
	conn = Conn{Driver: "mariadb", Host: "127.0.0.1", Port: port, User: "app", Database: "db"}
	_, err = NewSQLSession(connectCtx(t), conn, "s3cret-pw")
	if err == nil || !strings.Contains(err.Error(), "Access denied") || strings.Contains(err.Error(), "s3cret-pw") {
		t.Errorf("mysql auth error = %v", err)
	}
}

// A good login gives a working session; its read-only clone opens its
// own connection with the same settings.
func TestNewSQLSessionPostgresConnects(t *testing.T) {
	pg := startFakePostgres(t, nil)
	conn := Conn{Driver: "postgres", Host: "127.0.0.1", Port: pg.port(), User: "app", Database: "db", Params: map[string]string{"sslmode": "disable"}}
	s, err := NewSQLSession(connectCtx(t), conn, "pw")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Ping(connectCtx(t)); err != nil {
		t.Fatal(err)
	}
	ro, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	if err := ro.Ping(connectCtx(t)); err != nil {
		t.Fatalf("read-only clone ping: %v", err)
	}
	_ = ro.Close()
	if pg.queries.Load() < 2 {
		t.Errorf("server saw %d pings", pg.queries.Load())
	}
}

// Through an SSH tunnel the database host name is resolved on the far
// side: the dialer gets the name as written, even one this machine
// cannot resolve.
func TestOpenSQLDBDialsThroughTunnel(t *testing.T) {
	pg := startFakePostgres(t, nil)
	myPort, myAccepted := startFakeMySQL(t)
	var mu sync.Mutex
	var dialed []string
	tunnel := func(target int) DialFunc {
		return func(ctx context.Context, network, addr string) (net.Conn, error) {
			mu.Lock()
			dialed = append(dialed, addr)
			mu.Unlock()
			var d net.Dialer
			return d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(target)))
		}
	}
	conn := Conn{Driver: "postgres", Host: "db.internal.invalid", Port: 5432, User: "app", Database: "db", Params: map[string]string{"sslmode": "disable"}, Dial: tunnel(pg.port())}
	s, err := NewSQLSession(connectCtx(t), conn, "pw")
	if err != nil {
		t.Fatalf("postgres through tunnel: %v", err)
	}
	_ = s.Close()
	conn = Conn{Driver: "mysql", Host: "db.internal.invalid", Port: 3306, User: "app", Dial: tunnel(myPort)}
	if _, err := NewSQLSession(connectCtx(t), conn, "pw"); err == nil || !strings.Contains(err.Error(), "Access denied") {
		t.Fatalf("mysql through tunnel: %v", err)
	}
	if myAccepted.Load() == 0 {
		t.Fatal("mysql did not use the tunnel")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, a := range dialed {
		if !strings.HasPrefix(a, "db.internal.invalid:") {
			t.Errorf("tunnel dialed %q, want the name as written", a)
		}
	}
}

// A MySQL connection given as a URL uses the URL's host, port and
// parameters.
func TestOpenSQLDBMySQLFromURL(t *testing.T) {
	port, accepted := startFakeMySQL(t)
	conn := Conn{Driver: "mysql", URL: "mysql://app@127.0.0.1:" + strconv.Itoa(port) + "/shop?charset=utf8mb4&tls=false"}
	_, err := NewSQLSession(connectCtx(t), conn, "pw")
	if err == nil || !strings.Contains(err.Error(), "Access denied") || accepted.Load() == 0 {
		t.Fatalf("URL connection did not reach the URL's server: %v", err)
	}
	if _, _, err := openSQLDB(Conn{Driver: "mysql", URL: "mysql://app@[::1"}, "pw"); err == nil || !strings.Contains(err.Error(), "invalid MySQL connection settings") {
		t.Errorf("bad URL = %v", err)
	}
}

// The MySQL "tls" parameter takes the libpq-like names; an unknown value
// is refused before any connection.
func TestOpenSQLDBMySQLTLSParam(t *testing.T) {
	for _, v := range []string{"true", "verify-full", "skip-verify", "require", "preferred", "false", "disable"} {
		db, release, err := openSQLDB(Conn{Driver: "mysql", Host: "127.0.0.1", Port: 1, Params: map[string]string{"TLS": v}}, "")
		if err != nil {
			t.Errorf("tls=%s: %v", v, err)
			continue
		}
		_ = db.Close()
		release()
	}
	_, _, err := openSQLDB(Conn{Driver: "mysql", Host: "127.0.0.1", Params: map[string]string{"tls": "maybe\x1b[31m"}}, "")
	if err == nil || !strings.Contains(err.Error(), `unknown MySQL tls value "maybe`) || strings.Contains(err.Error(), "\x1b") {
		t.Errorf("unknown tls = %v", err)
	}
}

// A broken "ssl" object (missing CA file) stops the connect with the
// file's problem, for both servers.
func TestOpenSQLDBBadSSLFiles(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "ca.pem")
	for _, driver := range []string{"postgres", "mysql"} {
		_, _, err := openSQLDB(Conn{Driver: driver, Host: "db", SSL: &SSLConfig{Mode: "verify-full", CA: missing}}, "")
		if err == nil || !strings.Contains(err.Error(), "could not read the CA file") || !strings.Contains(err.Error(), "no such file") {
			t.Errorf("%s: %v", driver, err)
		}
	}
}

func TestTLSConfigFileErrors(t *testing.T) {
	dir := t.TempDir()
	junk := filepath.Join(dir, "junk.pem")
	if err := os.WriteFile(junk, []byte("not a pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := TLSConfig(&SSLConfig{Cert: filepath.Join(dir, "nope.crt"), Key: junk}, "db")
	if err == nil || !strings.Contains(err.Error(), "could not read the client certificate") {
		t.Errorf("missing cert = %v", err)
	}
	_, err = TLSConfig(&SSLConfig{Cert: junk, Key: filepath.Join(dir, "nope.key")}, "db")
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("missing key = %v", err)
	}
	_, err = TLSConfig(&SSLConfig{Cert: junk, Key: junk}, "db")
	if err == nil || !strings.Contains(err.Error(), "do not load") {
		t.Errorf("bad pair = %v", err)
	}
	if got := errText(errors.New("plain")); got != "plain" {
		t.Errorf("errText = %q", got)
	}
}
