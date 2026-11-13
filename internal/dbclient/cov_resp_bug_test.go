package dbclient

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

// A listen that runs until its deadline ends normally, every time: the
// connection's read deadline may fire a moment before the context's own
// error is set, and that is still the end of the wait, not a failure.
func TestRedisListenUntilTheDeadlineIsNeverAnError(t *testing.T) {
	quiet := newScriptResp(t, func(args []string, _ io.Writer) string {
		if strings.ToUpper(args[0]) == "PSUBSCRIBE" {
			return "*3\r\n" + respBulk("psubscribe") + respBulk(args[1]) + ":1\r\n"
		}
		return scriptBase(args)
	})
	s, err := NewRedisSession(context.Background(), scriptConn(quiet.addr(), nil), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 25; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		msgs, more, err := s.Listen(ctx, "x", 5)
		cancel()
		if err != nil || more || len(msgs) != 0 {
			t.Fatalf("round %d: a quiet channel until the deadline = %+v %v %v", i, msgs, more, err)
		}
	}
}
