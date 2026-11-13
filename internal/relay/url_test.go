package relay

import "testing"

func TestCheckURLWantsHTTPSOrLoopback(t *testing.T) {
	for _, ok := range []string{"https://relay.example.org", "https://relay.example.org/", "https://relay.example.org:8443", "http://127.0.0.1:8787", "http://localhost:9", "http://[::1]:80"} {
		if _, err := CheckURL(ok); err != nil {
			t.Errorf("CheckURL(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "relay.example.org", "http://relay.example.org", "http://192.168.1.2:80", "wss://relay.example.org",
		"https://relay.example.org/path", "https://user@relay.example.org", "https://relay.example.org/?q=1", "https://relay.example.org/#x", "ftp://x"} {
		if _, err := CheckURL(bad); err == nil {
			t.Errorf("CheckURL(%q) worked", bad)
		}
	}
}

func TestWSURL(t *testing.T) {
	room, _ := NewRoomID()
	got, err := WSURL("https://relay.example.org:8443/", room, SideGateway)
	if err != nil || got != "wss://relay.example.org:8443"+PathPrefix+room+"?side=gw" {
		t.Fatalf("WSURL = %q %v", got, err)
	}
	got, err = WSURL("http://127.0.0.1:8787", room, SidePage)
	if err != nil || got != "ws://127.0.0.1:8787"+PathPrefix+room+"?side=page" {
		t.Fatalf("WSURL = %q %v", got, err)
	}
	if _, err := WSURL("https://relay.example.org", "bad", SidePage); err == nil {
		t.Fatal("a bad room worked")
	}
}
