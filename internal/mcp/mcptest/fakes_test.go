package mcptest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func postForm(t *testing.T, u string, v url.Values) (int, map[string]any) {
	t.Helper()
	resp, err := http.PostForm(u, v)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return resp.StatusCode, m
}

// Spec (CX-0): the fake AS runs the device flow: a device code and a
// user code, "authorization_pending" PendingPolls times after one
// slow_down, then tokens; the metadata lists the device and revocation
// endpoints.
func TestAuthServerDeviceRoundTrip(t *testing.T) {
	as := NewAuthServer(t, AuthOptions{Device: true, PreClientID: "dev-app", PendingPolls: 2, SlowDown: true, Revocation: true})
	resp, err := http.Get(as.URL + "/.well-known/oauth-authorization-server")
	if err != nil {
		t.Fatal(err)
	}
	var md map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&md)
	resp.Body.Close()
	if md["device_authorization_endpoint"] != as.Issuer()+"/device/code" || md["revocation_endpoint"] != as.Issuer()+"/revoke" {
		t.Fatalf("metadata %v", md)
	}
	code, dc := postForm(t, as.Issuer()+"/device/code", url.Values{"client_id": {"dev-app"}, "scope": {"read:user"}})
	if code != 200 || dc["device_code"] == nil || !strings.HasPrefix(dc["user_code"].(string), "WDJB-") || dc["interval"].(float64) != 1 {
		t.Fatalf("device code %d %v", code, dc)
	}
	poll := url.Values{"grant_type": {deviceGrant}, "device_code": {dc["device_code"].(string)}, "client_id": {"dev-app"}}
	var errs []string
	for i := 0; i < 5; i++ {
		code, m := postForm(t, as.Issuer()+"/token", poll)
		if e, ok := m["error"].(string); ok {
			if code != 400 {
				t.Errorf("RFC errors are HTTP 400, got %d", code)
			}
			errs = append(errs, e)
			continue
		}
		if m["access_token"] == nil || !as.Valid(m["access_token"].(string)) {
			t.Fatalf("token answer %v", m)
		}
		break
	}
	if strings.Join(errs, ",") != "slow_down,authorization_pending,authorization_pending" {
		t.Errorf("poll errors %v", errs)
	}
	if as.DevicePolls() != 4 {
		t.Errorf("polls %d", as.DevicePolls())
	}
}

// Spec: denied / expired modes and the GitHub style (errors with 200).
func TestAuthServerDeviceModes(t *testing.T) {
	for mode, want := range map[string]string{"denied": "access_denied", "expired": "expired_token"} {
		as := NewAuthServer(t, AuthOptions{Device: true, PreClientID: "dev-app", DeviceMode: mode, GitHubStyle: true})
		_, dc := postForm(t, as.Issuer()+"/device/code", url.Values{"client_id": {"dev-app"}})
		code, m := postForm(t, as.Issuer()+"/token", url.Values{"grant_type": {deviceGrant}, "device_code": {dc["device_code"].(string)}, "client_id": {"dev-app"}})
		if code != 200 || m["error"] != want {
			t.Errorf("%s: %d %v", mode, code, m)
		}
	}
	as := NewAuthServer(t, AuthOptions{Device: true, PreClientID: "dev-app", DeviceMode: "manual", NoRefresh: true})
	_, dc := postForm(t, as.Issuer()+"/device/code", url.Values{"client_id": {"dev-app"}})
	poll := url.Values{"grant_type": {deviceGrant}, "device_code": {dc["device_code"].(string)}, "client_id": {"dev-app"}}
	if _, m := postForm(t, as.Issuer()+"/token", poll); m["error"] != "authorization_pending" {
		t.Errorf("manual before approval: %v", m)
	}
	if !as.ApproveDevice(dc["user_code"].(string)) {
		t.Fatal("approve")
	}
	if _, m := postForm(t, as.Issuer()+"/token", poll); m["access_token"] == nil || m["refresh_token"] != nil {
		t.Errorf("after approval (no refresh token expected): %v", m)
	}
}

// Spec: /revoke forgets a token; NoNone registrations get a secret.
func TestAuthServerRevokeAndConfidentialDCR(t *testing.T) {
	as := NewAuthServer(t, AuthOptions{Revocation: true, NoNone: true})
	resp, err := http.Post(as.Issuer()+"/register", "application/json", strings.NewReader(`{"client_name":"t"}`))
	if err != nil {
		t.Fatal(err)
	}
	var reg map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&reg)
	resp.Body.Close()
	if reg["client_secret"] == nil || reg["token_endpoint_auth_method"] != "client_secret_post" {
		t.Errorf("registration %v", reg)
	}
	if code, _ := postForm(t, as.Issuer()+"/revoke", url.Values{"token": {"at-x"}}); code != 200 || len(as.Revoked()) != 1 {
		t.Errorf("revoke %d %v", code, as.Revoked())
	}
}

// Spec: /mcp/readonly lists only read-only tools; Login adds get_me.
func TestHTTPServerReadOnlyAndIdentity(t *testing.T) {
	s := NewHTTPServer(t, HTTPOptions{Login: "amin-jalali"})
	full, ro := s.tools(false), s.tools(true)
	names := func(l []map[string]any) string {
		var out []string
		for _, t := range l {
			out = append(out, t["name"].(string))
		}
		return strings.Join(out, ",")
	}
	if !strings.Contains(names(full), "get_me") || !strings.Contains(names(full), "write_note") {
		t.Errorf("full %s", names(full))
	}
	if strings.Contains(names(ro), "write_note") || strings.Contains(names(ro), "delete_all") || !strings.Contains(names(ro), "get_me") {
		t.Errorf("readonly %s", names(ro))
	}
}
