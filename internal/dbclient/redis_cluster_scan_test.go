package dbclient

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
)

func TestRedisClusterMastersReadsTheNodeList(t *testing.T) {
	nodes := "a1 10.0.0.1:7000@17000 myself,master - 0 0 1 connected 0-5460\n" +
		"b2 10.0.0.2:7001@17001,node-b master - 0 0 2 connected 5461-10922\n" +
		"c3 10.0.0.3:7002@17002 slave a1 0 0 1 connected\n" +
		"d4 10.0.0.4:7003@17003 master,fail - 0 0 3 disconnected 10923-16383\n" +
		"e5 :7004@17004 master - 0 0 4 connected\n" +
		"f6 :0@0 master,noaddr - 0 0 5 connected\n"
	got := strings.Join(redisClusterMasters(nodes, "seed.local"), ",")
	if got != "10.0.0.1:7000,10.0.0.2:7001,seed.local:7004" {
		t.Fatalf("masters = %s", got)
	}
}

// clusterServer answers SCAN with the keys of the dialed node and
// CLUSTER NODES with nodes (an empty nodes: cluster support is off).
func clusterServer(nodes string, keys map[string][]string, dialed *[]string) RedisDialFunc {
	var mu sync.Mutex
	return func(_ context.Context, _, addr string) (net.Conn, error) {
		mu.Lock()
		*dialed = append(*dialed, addr)
		mu.Unlock()
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			r := bufio.NewReader(b)
			for {
				v, err := redisReadCommand(r)
				if err != nil {
					return
				}
				var reply string
				switch strings.ToUpper(v[0]) {
				case "CLUSTER":
					if nodes == "" {
						reply = "-ERR This instance has cluster support disabled\r\n"
					} else {
						reply = "$" + itoa(len(nodes)) + "\r\n" + nodes + "\r\n"
					}
				case "SCAN":
					ks := keys[addr]
					reply = "*2\r\n$1\r\n0\r\n*" + itoa(len(ks)) + "\r\n"
					for _, k := range ks {
						reply += "$" + itoa(len(k)) + "\r\n" + k + "\r\n"
					}
				case "PING":
					reply = "+PONG\r\n"
				default:
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

// On a cluster node the key list is the keys of every master.
func TestRedisScanKeysListsEveryClusterMaster(t *testing.T) {
	nodes := "a1 10.0.0.1:7000@17000 myself,master - 0 0 1 connected 0-8000\n" +
		"b2 10.0.0.2:7001@17001 master - 0 0 2 connected 8001-16383\n" +
		"c3 10.0.0.3:7002@17002 slave a1 0 0 1 connected\n"
	var dialed []string
	dial := clusterServer(nodes, map[string][]string{
		"10.0.0.1:7000": {"user:1", "user:2"},
		"10.0.0.2:7001": {"user:3", "user:1"},
		"10.0.0.3:7002": {"replica-only"},
	}, &dialed)
	s, err := NewRedisSession(context.Background(), Conn{Driver: "redis", Host: "10.0.0.1", Port: 7000}, "", dial)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	keys, more, err := s.ScanKeys(context.Background(), 0, "user:*", 100)
	if err != nil {
		t.Fatal(err)
	}
	if more || strings.Join(keys, ",") != "user:1,user:2,user:3" {
		t.Fatalf("keys = %v more %v", keys, more)
	}
	for _, a := range dialed {
		if a == "10.0.0.3:7002" {
			t.Fatal("a replica must not be scanned")
		}
	}
}

// The cap counts the keys of all nodes together.
func TestRedisClusterScanStopsAtTheCap(t *testing.T) {
	nodes := "a1 10.0.0.1:7000@17000 myself,master - 0 0 1 connected 0-8000\n" +
		"b2 10.0.0.2:7001@17001 master - 0 0 2 connected 8001-16383\n"
	var dialed []string
	dial := clusterServer(nodes, map[string][]string{
		"10.0.0.1:7000": {"k1", "k2"},
		"10.0.0.2:7001": {"k3", "k4"},
	}, &dialed)
	s, err := NewRedisSession(context.Background(), Conn{Driver: "redis", Host: "10.0.0.1", Port: 7000}, "", dial)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	keys, more, err := s.ScanKeys(context.Background(), 0, "*", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !more || len(keys) != 3 {
		t.Fatalf("keys = %v more %v", keys, more)
	}
}

// A plain server (cluster support off) keeps the one-node scan.
func TestRedisScanKeysOnAPlainServerScansOneNode(t *testing.T) {
	var dialed []string
	dial := clusterServer("", map[string][]string{"plain.local:6379": {"a", "b"}}, &dialed)
	s, err := NewRedisSession(context.Background(), Conn{Driver: "redis", Host: "plain.local"}, "", dial)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	keys, _, err := s.ScanKeys(context.Background(), 0, "*", 100)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(keys, ",") != "a,b" || len(dialed) != 1 {
		t.Fatalf("keys = %v dialed %v", keys, dialed)
	}
}
