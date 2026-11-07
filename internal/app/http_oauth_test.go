package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/secrets/secretstest"
)

// A request with `# @oauth2 grant=client_credentials` gets a token
// first, sends with it, and caches it in the secret store: the next
// send uses the cache.
func TestHTTPOAuth2ClientCredentialsSendsAndCaches(t *testing.T) {
	m, root, _ := covSvcHTTP(t)
	store := secrets.NewStore(secretstest.NewMemory(), secrets.Options{Dir: t.TempDir()}) // its index file stays out of the source tree
	old := httpOAuthStore
	httpOAuthStore = func() secrets.Store { return store }
	t.Cleanup(func() { httpOAuthStore = old })
	var mu sync.Mutex
	var tokens int
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/token" {
			tokens++
			_, _ = w.Write([]byte(`{"access_token":"tok-1","expires_in":3600}`))
			return
		}
		auth = append(auth, r.Header.Get("Authorization"))
	}))
	t.Cleanup(srv.Close)
	file := filepath.Join(root, "o.http")
	src := "###\n# @name api\n# @oauth2 grant=client_credentials token_url=" + srv.URL + "/token client_id=app client_secret=s\nGET " + srv.URL + "/api\n"
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	f, req := chainDepReq(t, m, file, "api")
	covSvcHTTPRun(t, m, m.httpSendRequest(file, f.Vars, req, ""))
	covSvcHTTPRun(t, m, m.httpSendRequest(file, f.Vars, req, ""))
	mu.Lock()
	defer mu.Unlock()
	if tokens != 1 || len(auth) != 2 || auth[0] != "Bearer tok-1" || auth[1] != "Bearer tok-1" {
		t.Fatalf("tokens=%d auth=%q", tokens, auth)
	}
	var cached bool
	for _, n := range store.Names() {
		if strings.HasPrefix(n, "http.oauth2.") {
			v, _, _ := store.Get(n)
			var tok struct {
				AccessToken string `json:"access_token"`
			}
			cached = json.Unmarshal([]byte(v), &tok) == nil && tok.AccessToken == "tok-1"
		}
	}
	if !cached {
		t.Error("token not cached in the secret store")
	}
}

// Device code shows a card with the user code; Cancel is first and
// stops the wait.
func TestHTTPOAuth2DeviceCard(t *testing.T) {
	m, _, _ := covSvcHTTP(t)
	m.httpOAuthDeviceCard(&httpOAuthWait{}, "ABCD-EFGH", "https://idp/device")
	if !m.confirmOpen || m.confirmKind != confirmKindHTTPOAuthDevice || !strings.Contains(cardText(m), "ABCD-EFGH") {
		t.Fatalf("card: %s", cardText(m))
	}
	if b := m.confirm.Buttons(); b[0].ID != "cancel" {
		t.Fatalf("buttons %+v", b)
	}
}
