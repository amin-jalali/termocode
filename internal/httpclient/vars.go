package httpclient

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// placeholderRe matches {{name}}, {{ name }} and {{$fn args}}.
var placeholderRe = lazyre.New(`\{\{\s*([^{}]*?)\s*\}\}`)

// maxNesting is how many times values that hold other {{names}} are
// expanded (a cycle stops there and is reported as unresolved).
const maxNesting = 5

// Resolver substitutes {{name}} placeholders. Sources, first hit wins:
// file variables, the selected environment (private file wins over the
// public one), $shared, then the $-functions ($dotenv, $processEnv,
// $secret, $guid, $uuid, $timestamp, $isoTimestamp, $datetime,
// $localDatetime, $randomInt).
type Resolver struct {
	FileVars []Var
	Env      string
	Envs     *Envs
	// LookupEnv reads the process environment ($processEnv); nil =
	// os.LookupEnv.
	LookupEnv func(string) (string, bool)
	// Secret reads the credentials store ($secret NAME).
	Secret func(string) (string, bool)
	// OnSecret is called with every value that came from a private env
	// file or $secret (the app passes ai.RegisterSecret).
	OnSecret func(string)
	// Now is the clock for $timestamp / $datetime; nil = time.Now.
	Now func() time.Time
}

// Unresolved is one placeholder that is still raw after resolving.
type Unresolved struct {
	Name   string
	Field  string // "url", "header:<Name>" or "body"
	Offset int    // byte offset in the resolved field text
}

// Resolved is a request with every known placeholder substituted.
type Resolved struct {
	Request
	// Template is the request as written (placeholders kept).
	Template Request
	// Env is the environment that was used ("" = none).
	Env string
	// Secrets lists the private / $secret values that were substituted,
	// so callers can hide them again.
	Secrets []string
}

// sendScope keeps the values of the dynamic functions for one send, so
// {{$guid}} used twice gives the same value.
type sendScope struct {
	rv      *Resolver
	dynamic map[string]string
	secrets []string
}

// Resolve substitutes URL, header values and body. Unknown names stay
// as-is (mobocode behaviour) and are listed in the result.
func (rv *Resolver) Resolve(req Request) (Resolved, []Unresolved) {
	sc := &sendScope{rv: rv, dynamic: map[string]string{}}
	out := Resolved{Template: req, Env: rv.Env}
	out.Request = req
	var un []Unresolved
	out.URL = sc.expand(req.URL)
	un = appendUnresolved(un, out.URL, "url")
	if len(req.Headers) > 0 {
		out.Headers = make([]Header, len(req.Headers))
		for i, h := range req.Headers {
			v := sc.expand(h.Value)
			out.Headers[i] = Header{Name: h.Name, Value: v}
			un = appendUnresolved(un, v, "header:"+h.Name)
		}
	}
	if req.Body != nil {
		b := sc.expand(*req.Body)
		out.Body = &b
		un = appendUnresolved(un, b, "body")
	}
	out.Secrets = sc.secrets
	return out, un
}

func appendUnresolved(un []Unresolved, s, field string) []Unresolved {
	if !strings.Contains(s, "{{") {
		return un
	}
	for _, m := range placeholderRe.FindAllStringSubmatchIndex(s, -1) {
		un = append(un, Unresolved{Name: s[m[2]:m[3]], Field: field, Offset: m[0]})
	}
	return un
}

func (sc *sendScope) expand(s string) string {
	for range maxNesting {
		if !strings.Contains(s, "{{") {
			return s
		}
		changed := false
		s = placeholderRe.ReplaceAllStringFunc(s, func(m string) string {
			expr := placeholderRe.FindStringSubmatch(m)[1]
			if v, ok := sc.lookup(expr); ok {
				changed = true
				return v
			}
			return m
		})
		if !changed {
			break
		}
	}
	return s
}

// Known reports whether expr (the text inside {{ }}) resolves, without
// evaluating dynamic functions twice or registering secrets. The `.http`
// buffer uses it for the undercurl on unknown names.
func (rv *Resolver) Known(expr string) bool {
	expr = strings.TrimSpace(expr)
	if strings.HasPrefix(expr, "$") {
		fn, arg, _ := strings.Cut(expr, " ")
		arg = strings.TrimSpace(arg)
		switch fn {
		case "$dotenv":
			_, ok := rv.dotenv(arg)
			return ok
		case "$processEnv":
			_, ok := rv.lookupEnv(arg)
			return ok
		case "$secret":
			if rv.Secret == nil {
				return false
			}
			_, ok := rv.Secret(arg)
			return ok
		}
		return knownDynamic(fn, arg, rv.now)
	}
	_, _, ok := rv.named(expr)
	return ok
}

func (sc *sendScope) lookup(expr string) (string, bool) {
	rv := sc.rv
	if !strings.HasPrefix(expr, "$") {
		v, private, ok := rv.named(expr)
		if ok && private {
			sc.secret(v)
		}
		return v, ok
	}
	fn, arg, _ := strings.Cut(expr, " ")
	arg = strings.TrimSpace(arg)
	switch fn {
	case "$dotenv":
		return rv.dotenv(arg)
	case "$processEnv":
		return rv.lookupEnv(arg)
	case "$secret":
		if rv.Secret == nil {
			return "", false
		}
		v, ok := rv.Secret(arg)
		if ok {
			sc.secret(v)
		}
		return v, ok
	}
	if v, ok := sc.dynamic[expr]; ok {
		return v, true
	}
	v, ok := dynamicValue(fn, arg, rv.now())
	if ok {
		sc.dynamic[expr] = v
	}
	return v, ok
}

func (sc *sendScope) secret(v string) {
	sc.secrets = append(sc.secrets, v)
	if sc.rv.OnSecret != nil {
		sc.rv.OnSecret(v)
	}
}

// named looks a plain name up: file variables (last definition wins),
// then the environment.
func (rv *Resolver) named(name string) (val string, private, ok bool) {
	if !isPlainName(name) {
		return "", false, false
	}
	for i := len(rv.FileVars) - 1; i >= 0; i-- {
		if rv.FileVars[i].Name == name {
			return rv.FileVars[i].Value, false, true
		}
	}
	if rv.Envs != nil {
		return rv.Envs.Lookup(rv.Env, name)
	}
	return "", false, false
}

func (rv *Resolver) dotenv(name string) (string, bool) {
	if rv.Envs == nil || rv.Envs.Dotenv == nil {
		return "", false
	}
	v, ok := rv.Envs.Dotenv[name]
	return v, ok
}

func (rv *Resolver) lookupEnv(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if rv.LookupEnv != nil {
		return rv.LookupEnv(name)
	}
	return os.LookupEnv(name)
}

func (rv *Resolver) now() time.Time {
	if rv.Now != nil {
		return rv.Now()
	}
	return time.Now()
}

// dynamicValue evaluates the $-functions that need no outside source.
func dynamicValue(fn, arg string, now time.Time) (string, bool) {
	args := strings.Fields(arg)
	switch fn {
	case "$guid", "$uuid", "$random.uuid":
		return newUUID(), true
	case "$timestamp":
		t, ok := applyOffset(now, args)
		if !ok {
			return "", false
		}
		return strconv.FormatInt(t.Unix(), 10), true
	case "$isoTimestamp":
		return now.UTC().Format(time.RFC3339), true
	case "$datetime", "$localDatetime":
		if len(args) == 0 {
			return "", false
		}
		t, ok := applyOffset(now, args[1:])
		if !ok {
			return "", false
		}
		if fn == "$datetime" {
			t = t.UTC()
		} else {
			t = t.Local()
		}
		switch strings.ToLower(args[0]) {
		case "iso8601":
			return t.Format(time.RFC3339), true
		case "rfc1123":
			if fn == "$datetime" {
				return t.Format(http.TimeFormat), true
			}
			return t.Format(time.RFC1123Z), true
		}
		return "", false
	case "$randomInt":
		if len(args) != 2 {
			return "", false
		}
		lo, err1 := strconv.ParseInt(args[0], 10, 64)
		hi, err2 := strconv.ParseInt(args[1], 10, 64)
		if err1 != nil || err2 != nil || hi <= lo {
			return "", false
		}
		n, err := rand.Int(rand.Reader, big.NewInt(hi-lo))
		if err != nil {
			return "", false
		}
		return strconv.FormatInt(lo+n.Int64(), 10), true
	}
	return "", false
}

// applyOffset reads "<n> <unit>" (VS Code units: y M w d h m s ms).
func applyOffset(t time.Time, args []string) (time.Time, bool) {
	if len(args) == 0 {
		return t, true
	}
	if len(args) != 2 {
		return t, false
	}
	n, err := strconv.Atoi(args[0])
	if err != nil {
		return t, false
	}
	switch args[1] {
	case "y":
		return t.AddDate(n, 0, 0), true
	case "M":
		return t.AddDate(0, n, 0), true
	case "w":
		return t.AddDate(0, 0, 7*n), true
	case "d":
		return t.AddDate(0, 0, n), true
	case "h":
		return t.Add(time.Duration(n) * time.Hour), true
	case "m":
		return t.Add(time.Duration(n) * time.Minute), true
	case "s":
		return t.Add(time.Duration(n) * time.Second), true
	case "ms":
		return t.Add(time.Duration(n) * time.Millisecond), true
	}
	return t, false
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}

// isPlainName is ^[\w.-]+$ without the regexp.
func isPlainName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(isLetter(c) || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

// knownDynamic is dynamicValue's ok without making the value (no UUID
// or clock text for an undercurl check).
func knownDynamic(fn, arg string, now func() time.Time) bool {
	switch fn {
	case "$guid", "$uuid", "$random.uuid", "$isoTimestamp":
		return true
	}
	_, ok := dynamicValue(fn, arg, now())
	return ok
}
