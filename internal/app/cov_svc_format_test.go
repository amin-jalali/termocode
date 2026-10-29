package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Each container state has its own glyph and right column text.
func TestCovSvcContainerStateLook(t *testing.T) {
	cases := []struct {
		state, status string
		glyph         theme.Icon
		uptime        string
	}{
		{"running", "Up 2 hours (unhealthy)", theme.GlyphSvcBad, "2h"},
		{"running", "Up 5 seconds (health: starting)", theme.GlyphSvcStarting, "5s"},
		{"running", "Up Less than a second", theme.GlyphSvcUp, "<1s"},
		{"running", "Up About an hour", theme.GlyphSvcUp, "1h"},
		{"running", "Up 3 fortnights", theme.GlyphSvcUp, ""},
		{"running", "Up x minutes", theme.GlyphSvcUp, ""},
		{"running", "Up", theme.GlyphSvcUp, ""},
		{"paused", "Up 1 hour (Paused)", theme.GlyphContPaused, "paused"},
		{"restarting", "Restarting (1) 3 seconds ago", theme.GlyphSvcRestarting, "—"},
		{"created", "Created", theme.GlyphSvcStarting, "new"},
		{"removing", "Removal In Progress", theme.GlyphSvcStarting, ""},
		{"exited", "Exited (137) 1 minute ago", theme.GlyphSvcExited, "exit 137"},
		{"exited", "Exited (0) 1 minute ago", theme.GlyphSvcStopped, "done"},
		{"exited", "Exited (broken", theme.GlyphSvcStopped, "done"},
		{"dead", "Dead", theme.GlyphSvcExited, "done"},
		{"weird", "", theme.GlyphSvcStopped, ""},
	}
	for _, c := range cases {
		ct := containers.Container{State: c.state, Status: c.status}
		if g, _ := containerGlyph(ct); g != c.glyph {
			t.Errorf("%s %q: glyph %v, want %v", c.state, c.status, g, c.glyph)
		}
		if u, _ := containerUptime(ct); u != c.uptime {
			t.Errorf("%s %q: uptime %q, want %q", c.state, c.status, u, c.uptime)
		}
	}
	for d, want := range map[time.Duration]string{
		10 * time.Second: "1m", 3 * time.Hour: "3h", 5 * 24 * time.Hour: "5d", 21 * 24 * time.Hour: "3w",
		90 * 24 * time.Hour: "3mo", 800 * 24 * time.Hour: "2y",
	} {
		if got := shortAge(d); got != want {
			t.Errorf("shortAge(%v) = %q, want %q", d, got, want)
		}
	}
	if !imageCreated(containers.Image{CreatedAt: "2026-10-01 10:00:00 +0000 UTC"}).Equal(time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)) {
		t.Error("docker created time")
	}
	if imageCreated(containers.Image{CreatedAt: "2026-10-01T10:00:00Z"}).IsZero() || !imageCreated(containers.Image{CreatedAt: "3 weeks ago"}).IsZero() {
		t.Error("created time layouts")
	}
	if got := shortRemoteError("error during connect: ssh: connect to host prod port 22: Connection timed out"); got != "ssh: Connection timed out" {
		t.Errorf("shortRemoteError = %q", got)
	}
}

// A session on a remote context that does not answer names the context:
// CONTAINERS has the cause, the other sections point to it.
func TestCovSvcRemoteContextRows(t *testing.T) {
	st := svcHostState()
	if st.remoteContextRows("containers", "host/containers", "x") != nil {
		t.Error("no session context: rows")
	}
	st.sessionContext = "prod"
	rows := st.remoteContextRows("images", "host/images", "x")
	if len(rows) != 1 || rows[0].text != "context prod is not reachable" {
		t.Errorf("images rows = %+v", rows)
	}
	for detail, want := range map[string]string{
		"context deadline exceeded": "context prod: timed out",
		"error during connect: ssh: connect to host p port 22: No route to host": "context prod: ssh: No route to host",
		"": "context prod: timed out",
	} {
		rows = st.remoteContextRows("containers", "host/containers", detail)
		if len(rows) != 2 || rows[0].text != want {
			t.Errorf("%q: %+v", detail, rows)
		}
	}
	// The section body: an error per section, loading, no contexts.
	st.sessionContext = ""
	st.containerErrors["contexts"] = "context ls: broken file\nmore"
	if rows := st.contextSectionBody("host/contexts"); len(rows) != 2 || rows[0].text != "context ls: broken file" {
		t.Errorf("error rows = %+v", rows)
	}
	delete(st.containerErrors, "contexts")
	st.hostLoaded["contexts"] = false
	if rows := st.contextSectionBody("host/contexts"); len(rows) != 1 || rows[0].text != "Loading…" {
		t.Errorf("loading rows = %+v", rows)
	}
	st.hostLoaded["contexts"] = true
	st.contextRows = nil
	if rows := st.contextSectionBody("host/contexts"); len(rows) != 1 || !strings.HasPrefix(rows[0].text, "No context") {
		t.Errorf("empty rows = %+v", rows)
	}
}

// The Redis tree texts.
func TestCovSvcRedisTexts(t *testing.T) {
	for _, c := range []struct {
		n    int64
		more bool
		want string
	}{{1, false, "1 key"}, {1, true, "1+ keys"}, {5, false, "5 keys"}} {
		if got := dbRedisCount(c.n, c.more); got != c.want {
			t.Errorf("count = %q, want %q", got, c.want)
		}
	}
	if dbRedisTypeName("none") != "gone" || dbRedisTypeName("hash") != "hash" {
		t.Error("type name")
	}
	for ms, want := range map[int64]string{-1: "", 5000: "5 s", 300000: "5 min", 7200000: "2 h", 5 * 86400000: "5 days"} {
		if got := dbRedisTTL(ms); got != want {
			t.Errorf("ttl(%d) = %q, want %q", ms, got, want)
		}
	}
	for n, want := range map[int64]string{-1: "", 10: "10 B", 2048: "2.0 KB", 3 << 20: "3.0 MB"} {
		if got := dbRedisSize(n); got != want {
			t.Errorf("size(%d) = %q, want %q", n, got, want)
		}
	}
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	for _, mode := range []theme.IconMode{theme.IconModeASCII, theme.IconModeUnicode} {
		theme.CurrentIconMode = mode
		seen := map[string]bool{}
		for _, typ := range []string{"string", "hash", "list", "set", "zset", "stream"} {
			g := dbRedisTypeGlyph(typ)
			if g == "?" || seen[g] {
				t.Errorf("mode %v: %s glyph %q", mode, typ, g)
			}
			seen[g] = true
		}
		if dbRedisTypeGlyph("module") != "?" {
			t.Errorf("mode %v: unknown type glyph", mode)
		}
	}
	sep := "/"
	if (dbConsoleSettings{}).redisSeparator() != ":" || (dbConsoleSettings{RedisSep: &sep}).redisSeparator() != "/" {
		t.Error("separator")
	}
	zero, big := 0, 5000000
	if (dbConsoleSettings{RedisMaxKeys: &zero}).redisMaxKeys() != 10000 || (dbConsoleSettings{RedisMaxKeys: &big}).redisMaxKeys() != 1000000 {
		t.Error("max keys")
	}
}
