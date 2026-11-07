package dbclient

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
)

// fakeMemcached serves the text protocol on net.Pipe over a map.
func fakeMemcached(kv map[string]string, sent *[]string) DialFunc {
	return func(_ context.Context, _, _ string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			r := bufio.NewReader(b)
			for {
				line, err := r.ReadString('\n')
				if err != nil {
					return
				}
				line = strings.TrimRight(line, "\r\n")
				*sent = append(*sent, line)
				f := strings.Fields(line)
				var out string
				switch f[0] {
				case "version":
					out = "VERSION 1.6.21\r\n"
				case "get":
					for _, k := range f[1:] {
						if v, ok := kv[k]; ok {
							out += fmt.Sprintf("VALUE %s 0 %d\r\n%s\r\n", k, len(v), v)
						}
					}
					out += "END\r\n"
				case "set":
					data, _ := r.ReadString('\n')
					kv[f[1]] = strings.TrimRight(data, "\r\n")
					out = "STORED\r\n"
				case "delete":
					if _, ok := kv[f[1]]; ok {
						delete(kv, f[1])
						out = "DELETED\r\n"
					} else {
						out = "NOT_FOUND\r\n"
					}
				case "lru_crawler":
					for k, v := range kv {
						out += fmt.Sprintf("key=%s exp=-1 la=1 cas=1 fetch=no cls=1 size=%d\r\n", k, len(v)+50)
					}
					out += "END\r\n"
				case "stats":
					out = "STAT pid 1\r\nSTAT curr_items 2\r\nEND\r\n"
				default:
					out = "ERROR\r\n"
				}
				if _, err := b.Write([]byte(out)); err != nil {
					return
				}
			}
		}()
		return a, nil
	}
}

func TestMemcachedSessionConsoleAndTree(t *testing.T) {
	kv := map[string]string{"user:1": "ann", "user:2": "bob"}
	var sent []string
	s, err := NewMemcachedSession(Conn{Driver: "memcached", Host: "h", Dial: fakeMemcached(kv, &sent)})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	res := s.Run(ctx, "get user:1 user:2 nope", RunOptions{})
	if res.Err != nil || len(res.Rows) != 2 || res.Rows[0][1].Text != "ann" {
		t.Fatalf("get = %+v", res)
	}
	if w := s.Run(ctx, `set greet "hi there" 60`, RunOptions{}); w.Err != nil || kv["greet"] != "hi there" || sent[len(sent)-1] != "set greet 0 60 8" {
		t.Fatalf("set = %+v sent %v", w, sent)
	}
	if d := s.Run(ctx, "delete nope", RunOptions{}); d.Err == nil || !strings.Contains(d.Err.Error(), "NOT_FOUND") {
		t.Fatalf("delete = %+v", d)
	}
	in, err := s.Introspect(ctx, IntrospectionRequest{Database: "keys"})
	if err != nil || len(in.Objects) != 3 || in.Objects[0].Name != "greet" {
		t.Fatalf("keys = %+v %v", in.Objects, err)
	}
	if st := s.Run(ctx, "stats", RunOptions{}); st.Err != nil || len(st.Rows) != 2 {
		t.Fatalf("stats = %+v", st)
	}
	if f := s.Run(ctx, "flush_all", RunOptions{}); f.Err == nil {
		t.Fatal("flush_all must not be sent")
	}
	if b := s.Run(ctx, "get \"a b\"", RunOptions{}); b.Err == nil {
		t.Fatal("a key with a space must be refused")
	}
	ro, _ := s.ReadOnlySession()
	if r := ro.Run(ctx, "set k v", RunOptions{}); r.Err == nil {
		t.Fatal("read-only set must be refused")
	}
}

func TestClassifyMemcached(t *testing.T) {
	for in, want := range map[string]Kind{"get a": KindRead, "keys": KindRead, "set a b": KindWrite, "delete a": KindWrite, "flush_all": KindUnknown} {
		if got := ClassifyMemcached(in); got != want {
			t.Errorf("%q = %v, want %v", in, got, want)
		}
	}
}
