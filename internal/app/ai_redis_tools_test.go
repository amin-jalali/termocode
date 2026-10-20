package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

const aiRedisConns = `{"connections":[
 {"id":"cache","driver":"redis","host":"cache.internal","port":6379,"password":"${env:TC_REDIS_TEST_PW}"},
 {"id":"prod-cache","driver":"redis","host":"prod.internal","port":6379,"environment":"production"},
 {"id":"ro-cache","driver":"redis","host":"ro.internal","port":6379,"readOnly":true}]}`

type aiRedisEnv struct {
	f     *dbtest.FakeRedis
	m     *Model
	tools []ai.Tool
}

func newAIRedisEnv(t *testing.T) *aiRedisEnv {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TC_REDIS_TEST_PW", "s3cret-redis-pw")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".termocode", "databases.json"), []byte(aiRedisConns), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &aiRedisEnv{f: dbtest.NewFakeRedis(), m: &Model{}}
	e.f.Password = "s3cret-redis-pw"
	e.f.HSet(0, "session:ab12", "plan", "pro", "token", "s3cret-redis-pw", "evil", "\x1b]52;c;x\x07")
	e.f.Expire(0, "session:ab12", time.Hour)
	e.f.SetString(0, "session:cd34", "x")
	e.f.SetString(0, "counter", "41")
	t.Cleanup(SetRedisDialForTest(e.f.Dial))
	t.Cleanup(e.f.Close)
	e.m.databases = newDBState()
	e.m.databases.load(root)
	var mu sync.Mutex
	call := func(ctx context.Context, fn aiIDEFunc) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		out, _, err := fn(e.m)
		return out, err
	}
	e.tools = aiRedactResults(aiDBTools(root, call))
	return e
}

func (e *aiRedisEnv) tool(t *testing.T, name string) ai.Tool {
	t.Helper()
	tl, ok := ai.FindTool(e.tools, name)
	if !ok {
		t.Fatalf("no tool %s", name)
	}
	return tl
}

func (e *aiRedisEnv) gate(t *testing.T, a map[string]any) (bool, string, error) {
	t.Helper()
	tl := e.tool(t, "redis_command")
	if err := tl.Validate(a); err != nil {
		return false, "", err
	}
	ask, class := tl.Gate(a)
	return ask, class, nil
}

// Plan §4: reads run at once, writes ask with db:<conn>/write (always on
// production), read-only connections refuse writes, the never-list is
// refused before any card.
func TestAIRedisCommandGateAndNeverList(t *testing.T) {
	e := newAIRedisEnv(t)
	for _, tc := range []struct {
		conn, cmd string
		ask       bool
		class     string
		err       string
	}{
		{"cache", "GET counter", false, "", ""},
		{"cache", "HGETALL session:ab12", false, "", ""},
		{"cache", "KEYS *", false, "", ""},
		{"cache", "SET counter 42", true, "db:cache/write", ""},
		{"cache", "CONFIG GET requirepass", true, "db:cache/write", ""},
		{"prod-cache", "GET counter", false, "", ""},
		{"prod-cache", "DEL counter", true, "", ""},
		{"ro-cache", "SET a b", false, "", "read-only"},
		{"cache", "FLUSHALL", false, "", "never"},
		{"cache", "CONFIG SET requirepass x", false, "", "never"},
		{"cache", "EVAL \"redis.call('flushall')\" 0", false, "", "never"},
		{"cache", "MONITOR", false, "", "not sent"},
		{"cache", "GET a\nFLUSHALL", false, "", "one command"},
		{"pg", "GET a", false, "", "no connection"},
	} {
		ask, class, err := e.gate(t, map[string]any{"conn": tc.conn, "command": tc.cmd})
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%s %s: err %v, want %q", tc.conn, tc.cmd, err, tc.err)
			}
			continue
		}
		if err != nil || ask != tc.ask || class != tc.class {
			t.Errorf("%s %s: ask %v class %q err %v", tc.conn, tc.cmd, ask, class, err)
		}
	}
	if e.f.Sent("FLUSHALL") || e.f.Sent("MONITOR") {
		t.Fatal("a refused command reached the server")
	}
	if got := e.tool(t, "redis_command").Human(map[string]any{"conn": "prod-cache", "command": "DEL counter"}); !strings.Contains(got, "DEL counter") || !strings.Contains(got, "production") {
		t.Fatalf("card = %q", got)
	}
}

func TestAIRedisToolsReadPagedSanitizedAndHideThePassword(t *testing.T) {
	e := newAIRedisEnv(t)
	ctx := context.Background()
	out, err := e.tool(t, "redis_keys").Run(ctx, map[string]any{"conn": "cache", "pattern": "session:*"})
	if err != nil || !strings.Contains(out, "session:ab12 · hash · TTL") || !strings.Contains(out, "session:cd34 · string") || strings.Contains(out, "counter") {
		t.Fatalf("redis_keys = %q, %v", out, err)
	}
	out, err = e.tool(t, "redis_get").Run(ctx, map[string]any{"conn": "cache", "key": "session:ab12"})
	if err != nil || !strings.Contains(out, "plan | pro") || !strings.Contains(out, "hash · 3 fields") {
		t.Fatalf("redis_get = %q, %v", out, err)
	}
	if strings.Contains(out, "s3cret-redis-pw") || strings.ContainsAny(out, "\x1b\x07") {
		t.Fatalf("password or escapes reach the model: %q", out)
	}
	out, err = e.tool(t, "redis_command").Run(ctx, map[string]any{"conn": "cache", "command": "GET counter"})
	if err != nil || !strings.Contains(out, "41") {
		t.Fatalf("redis_command read = %q, %v", out, err)
	}
	if e.f.Sent("KEYS") || e.f.Sent("HGETALL") {
		t.Fatalf("KEYS or a whole value was read: %q", e.f.Commands())
	}
	// The user sees the AI's run on the result page and in the history.
	if s := e.m.databases.console; s == nil || s.shown == nil || s.shown.by != "ai" {
		t.Fatal("the AI run is not on the result page")
	}
}

func TestAIRedisReadRunsInAReadOnlySessionEvenIfTheGateIsSkipped(t *testing.T) {
	e := newAIRedisEnv(t)
	// A write sent to Run without the gate still runs only after the
	// user's card in the agent; here we check a read never writes: the
	// read path refuses anything off the allow list on its own.
	out, err := e.tool(t, "redis_command").Run(context.Background(), map[string]any{"conn": "ro-cache", "command": "SET a b"})
	if err == nil {
		t.Fatalf("a write ran on a read-only connection: %q", out)
	}
	if e.f.Sent("SET") {
		t.Fatal("SET reached the server")
	}
}

func TestAIRedisPromptLine(t *testing.T) {
	e := newAIRedisEnv(t)
	root := e.m.databases.root
	if line := aiDBPromptLine(root); !strings.Contains(line, "redis_keys") || !strings.Contains(line, "refused") {
		t.Fatalf("prompt line = %q", line)
	}
}
