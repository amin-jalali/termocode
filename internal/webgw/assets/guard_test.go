package assets

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The browser's schema guard (src/proto/guard.ts) keeps its own copies of
// the protocol's closed sets, so a message the Go side would never send
// is refused. These tests keep the copies equal to internal/webproto.

func tsList(t *testing.T, src, name string) []string {
	t.Helper()
	m := regexp.MustCompile(`(?s)export const ` + name + `\s*=\s*\[(.*?)\]\s*as const`).FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("guard.ts has no %s list", name)
	}
	var out []string
	for _, f := range strings.Split(m[1], ",") {
		f = strings.Trim(strings.TrimSpace(f), `"`)
		if f != "" {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

func readGuard(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("src/proto/guard.ts")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func sameList(t *testing.T, what string, got, want []string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s in guard.ts = %v, webproto has %v", what, got, want)
	}
}

func TestGuardKnowsEveryTopic(t *testing.T) {
	src := readGuard(t)
	var names, keyed []string
	for _, d := range webproto.Topics() {
		names = append(names, d.Name)
		if d.Keyed {
			keyed = append(keyed, d.Name)
		}
	}
	sameList(t, "TOPIC_NAMES", tsList(t, src, "TOPIC_NAMES"), names)
	sameList(t, "KEYED_TOPICS", tsList(t, src, "KEYED_TOPICS"), keyed)
}

func TestGuardKnowsEveryServerKind(t *testing.T) {
	sameList(t, "SERVER_KINDS", tsList(t, readGuard(t), "SERVER_KINDS"), webproto.ServerKinds())
}

func TestGuardKnowsEveryErrAndCloseCode(t *testing.T) {
	src := readGuard(t)
	var errs, closes []string
	for _, c := range webproto.ErrCodes() {
		errs = append(errs, string(c))
	}
	for _, c := range webproto.CloseCodes() {
		closes = append(closes, strconv.Itoa(int(c)))
	}
	sameList(t, "ERR_CODES", tsList(t, src, "ERR_CODES"), errs)
	sameList(t, "CLOSE_CODES", tsList(t, src, "CLOSE_CODES"), closes)
	if !strings.Contains(src, "PROTOCOL_VERSION = "+strconv.Itoa(webproto.Version)) {
		t.Errorf("guard.ts does not pin PROTOCOL_VERSION = %d", webproto.Version)
	}
}
