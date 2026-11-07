package webproto

import (
	"encoding/base64"
	"strings"
	"testing"
)

// Web Push (MOB-30): reads for any paired device, every change written
// down, and an address that is not a push service is refused before the
// hub sees it.
func TestWebPushOpRows(t *testing.T) {
	for op, audit := range map[string]Audit{OpPushKey: AuditNo, OpPushSubscribe: AuditYes, OpPushUnsubscribe: AuditYes} {
		p, ok := Lookup(op)
		if !ok {
			t.Fatalf("%s has no row", op)
		}
		if p.Role != RoleViewer || p.LaptopCard || p.Gateway || p.Audit != audit {
			t.Errorf("%s: %+v", op, p)
		}
		if p.MaxBytes > 4*kib {
			t.Errorf("%s: max bytes %d", op, p.MaxBytes)
		}
	}
}

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
