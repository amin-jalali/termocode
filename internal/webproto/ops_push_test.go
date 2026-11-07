package webproto

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestPushSubArgsValidate(t *testing.T) {
	b64 := base64.RawURLEncoding
	key := make([]byte, 65)
	key[0] = 4
	good := PushSubArgs{Endpoint: "https://fcm.googleapis.com/fcm/send/abc", P256dh: b64.EncodeToString(key), Auth: b64.EncodeToString(make([]byte, 16))}
	if err := good.validate(0); err != nil {
		t.Fatalf("good args refused: %v", err)
	}
	for name, a := range map[string]PushSubArgs{
		"not a push service": {Endpoint: "https://evil.example/x", P256dh: good.P256dh, Auth: good.Auth},
		"http":               {Endpoint: "http://fcm.googleapis.com/x", P256dh: good.P256dh, Auth: good.Auth},
		"short key":          {Endpoint: good.Endpoint, P256dh: b64.EncodeToString(key[:10]), Auth: good.Auth},
		"long auth":          {Endpoint: good.Endpoint, P256dh: good.P256dh, Auth: strings.Repeat("A", 200)},
		"empty":              {},
	} {
		if err := a.validate(0); err == nil {
			t.Errorf("%s: taken", name)
		}
	}
}
