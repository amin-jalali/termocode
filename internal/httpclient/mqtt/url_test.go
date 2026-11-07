package mqtt

import "testing"

// mqtt:// and mqtts:// URLs give the broker, the topic and the user
// info; mqtts means TLS on 8883.
func TestParseURL(t *testing.T) {
	tg, err := ParseURL("mqtts://ann:s3cret@broker.local/sensors/kitchen")
	if err != nil {
		t.Fatal(err)
	}
	if !tg.TLS || tg.Addr != "broker.local:8883" || tg.Host != "broker.local" || tg.Topic != "sensors/kitchen" ||
		tg.Username != "ann" || tg.Password != "s3cret" {
		t.Fatalf("target = %+v", tg)
	}
	if got := tg.String(); got != "mqtts://broker.local:8883" {
		t.Fatalf("String = %q (no user info)", got)
	}
	tg, err = ParseURL("mqtt://127.0.0.1:1999")
	if err != nil || tg.TLS || tg.Addr != "127.0.0.1:1999" || tg.Topic != "" {
		t.Fatalf("target = %+v, %v", tg, err)
	}
	for _, bad := range []string{"http://x/t", "mqtt:///t", "::"} {
		if _, err := ParseURL(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}
