package auth

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// Spec (AU-10): "oauth": {"metadata_url": …} names the authorization
// server's metadata when the server does not say where it is.
func TestDiscoverMetadataURLOverride(t *testing.T) {
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{})
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: as.Valid}) // no protected-resource metadata
	if _, err := Discover(ctx(t), nil, srv.MCPURL(), ""); err == nil {
		t.Fatal("found an AS without the override")
	}
	md, err := DiscoverWith(ctx(t), nil, srv.MCPURL(), "", as.URL+"/.well-known/oauth-authorization-server")
	if err != nil {
		t.Fatal(err)
	}
	if md.Issuer != as.Issuer() || !strings.HasPrefix(md.TokenEndpoint, as.URL) {
		t.Errorf("md %+v", md)
	}
	fl, err := Begin(ctx(t), Config{ServerURL: srv.MCPURL(), MetadataURL: as.URL + "/.well-known/oauth-authorization-server"})
	if err != nil {
		t.Fatal(err)
	}
	defer fl.Close()
	if !strings.HasPrefix(fl.AuthURL(), as.URL) {
		t.Errorf("auth url %s", fl.AuthURL())
	}
	if _, err := DiscoverWith(ctx(t), nil, srv.MCPURL(), "", "http://example.com/meta"); err == nil {
		t.Error("an insecure override was used")
	}
}
