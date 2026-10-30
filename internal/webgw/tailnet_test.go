//go:build unix

package webgw

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeTailscale answers the tailscale CLI from fixed outputs by argv.
type fakeTailscale struct {
	out   map[string]string
	err   map[string]error
	calls []string
}

func (f *fakeTailscale) run(_ context.Context, args ...string) ([]byte, error) {
	k := strings.Join(args, " ")
	f.calls = append(f.calls, k)
	if err := f.err[k]; err != nil {
		return nil, err
	}
	out, ok := f.out[k]
	if !ok {
		return nil, errors.New("unexpected tailscale " + k)
	}
	return []byte(out), nil
}

const tsStatusRunning = `{"Version":"1.80.0","BackendState":"Running",
 "Self":{"DNSName":"box.tail1234.ts.net.","HostName":"box","Online":true},
 "CertDomains":["box.tail1234.ts.net"],
 "CurrentTailnet":{"Name":"me@example.com","MagicDNSSuffix":"tail1234.ts.net","MagicDNSEnabled":true}}`

func tsWith(status string) *fakeTailscale {
	return &fakeTailscale{out: map[string]string{"status --json": status}}
}

func TestDetectTailnetHost(t *testing.T) {
	got, err := DetectTailnet(context.Background(), tsWith(tsStatusRunning).run)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "box.tail1234.ts.net" {
		t.Fatalf("host = %q", got.Host)
	}
}

// Every way of not being on a tailnet is refused with one plain sentence
// that says what to do (B1 English, no stack of errors).
func TestDetectTailnetRefuses(t *testing.T) {
	for _, tc := range []struct {
		name string
		ts   *fakeTailscale
		want error
		text string
	}{
		{"not installed", &fakeTailscale{err: map[string]error{"status --json": ErrNoTailscale}}, ErrNoTailscale, "not installed"},
		{"logged out", tsWith(`{"BackendState":"NeedsLogin","Self":{"DNSName":""}}`), ErrNotOnTailnet, "tailscale up"},
		{"stopped", tsWith(`{"BackendState":"Stopped","Self":{"DNSName":"box.tail1234.ts.net."}}`), ErrNotOnTailnet, "tailscale up"},
		{"no https", tsWith(`{"BackendState":"Running","Self":{"DNSName":"box.tail1234.ts.net."},"CertDomains":null}`), ErrNoTailnetHTTPS, "HTTPS"},
		{"cert for another name", tsWith(`{"BackendState":"Running","Self":{"DNSName":"box.tail1234.ts.net."},"CertDomains":["other.tail1234.ts.net"]}`), ErrNoTailnetHTTPS, "HTTPS"},
		{"odd name", tsWith(`{"BackendState":"Running","Self":{"DNSName":"box.example.com."},"CertDomains":["box.example.com"]}`), ErrNotTailnetName, "tailnet"},
		{"garbage", tsWith(`not json`), ErrNotOnTailnet, "tailscale up"},
		{"cli failed", &fakeTailscale{err: map[string]error{"status --json": errors.New("exit status 1")}}, ErrNotOnTailnet, "tailscale up"},
	} {
		_, err := DetectTailnet(context.Background(), tc.ts.run)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.text) {
			t.Errorf("%s: %q does not say %q", tc.name, err, tc.text)
		}
	}
}
