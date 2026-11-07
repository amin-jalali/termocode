package dbclient

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/gomodule/redigo/redis"
)

func TestRedisMovedAddrReadsTheRedirect(t *testing.T) {
	for in, want := range map[string]string{
		"MOVED 3999 10.0.0.2:6381":                  "10.0.0.2:6381",
		"this is a cluster node: ASK 12 [::1]:7000": "[::1]:7000",
		"ERR unknown command":                       "",
		"MOVED 3999":                                "",
	} {
		got, ok := redisMovedAddr(errors.New(in))
		if got != want || ok != (want != "") {
			t.Errorf("%q = %q %v, want %q", in, got, ok, want)
		}
	}
	if _, ok := redisMovedAddr(nil); ok {
		t.Fatal("nil error is no redirect")
	}
}

func TestRedisSentinelAddrsAddsTheDefaultPort(t *testing.T) {
	got := strings.Join(redisSentinelAddrs("s1:26379", "s2, s3:26380,s1:26379"), ",")
	if got != "s1:26379,s2:26379,s3:26380" {
		t.Fatalf("addrs = %s", got)
	}
}

// respServer answers each command on a pipe with replies[cmd].
func respServer(t *testing.T, replies map[string]string, dialed *[]string) RedisDialFunc {
	return func(_ context.Context, _, addr string) (net.Conn, error) {
		*dialed = append(*dialed, addr)
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			r := bufio.NewReader(b)
			for {
				v, err := redisReadCommand(r)
				if err != nil {
					return
				}
				reply, ok := replies[strings.ToUpper(v[0])]
				if !ok {
					reply = "-ERR unknown\r\n"
				}
				if _, err := b.Write([]byte(reply)); err != nil {
					return
				}
			}
		}()
		return a, nil
	}
}

// redisReadCommand reads one RESP array of bulk strings.
func redisReadCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "*") {
		return nil, errors.New("bad")
	}
	n := 0
	for _, c := range strings.TrimSpace(line[1:]) {
		n = n*10 + int(c-'0')
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		if _, err := r.ReadString('\n'); err != nil {
			return nil, err
		}
		v, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		out = append(out, strings.TrimRight(v, "\r\n"))
	}
	return out, nil
}

// A "sentinel" connection asks the sentinel for the master and connects
// there.
func TestRedisSentinelConnectsToTheMaster(t *testing.T) {
	var dialed []string
	dial := respServer(t, map[string]string{
		"SENTINEL": "*2\r\n$8\r\n10.0.0.9\r\n$4\r\n6390\r\n",
		"PING":     "+PONG\r\n",
	}, &dialed)
	c := Conn{Driver: "redis", Host: "sentinel.local", Port: 26379, Params: map[string]string{"sentinel": "mymaster"}}
	s, err := NewRedisSession(context.Background(), c, "", dial)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(dialed) < 2 || dialed[0] != "sentinel.local:26379" || dialed[1] != "10.0.0.9:6390" {
		t.Fatalf("dialed = %v", dialed)
	}
}

var _ = redis.ErrNil
