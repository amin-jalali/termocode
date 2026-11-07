package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

func httpMQTTResolved(url, body string, dirs map[string]string) httpclient.Resolved {
	r := httpclient.Request{Method: "MQTT", URL: url, Directives: dirs}
	if body != "" {
		r.Body = &body
	}
	return httpclient.Resolved{Request: r}
}

// The send key treats MQTT method and mqtt(s):// URLs as MQTT.
func TestIsMQTTRequest(t *testing.T) {
	for _, r := range []httpclient.Request{{Method: "MQTT", URL: "broker"}, {Method: "GET", URL: "mqtts://b/t"}} {
		if !isMQTTRequest(r) {
			t.Errorf("%+v: not MQTT", r)
		}
	}
	if isMQTTRequest(httpclient.Request{Method: "GET", URL: "https://x"}) {
		t.Error("https is not MQTT")
	}
}

// The plan: body messages are published to the URL topic, @subscribe
// filters are read, @listen is capped; no body and no @subscribe listens
// on the topic.
func TestHTTPMQTTPlan(t *testing.T) {
	p, err := httpMQTTPlanFor(httpMQTTResolved("mqtt://b/room/1", "on\n===\noff", map[string]string{"subscribe": "room/#, alerts", "listen": "10m", "retain": ""}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.publish, "|") != "on|off" || strings.Join(p.subs, "|") != "room/#|alerts" || p.listen != httpMQTTListenMax || !p.retain {
		t.Fatalf("plan = %+v", p)
	}
	p, err = httpMQTTPlanFor(httpMQTTResolved("mqtt://b/room/1", "", map[string]string{"listen": "2"}))
	if err != nil || strings.Join(p.subs, "|") != "room/1" || p.listen != 2*time.Second || len(p.publish) != 0 {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	if _, err := httpMQTTPlanFor(httpMQTTResolved("mqtt://b", "hi", nil)); err == nil {
		t.Error("publish without a topic: no error")
	}
	if _, err := httpMQTTPlanFor(httpMQTTResolved("mqtt://b/t", "", map[string]string{"listen": "soon"})); err == nil {
		t.Error("bad @listen: no error")
	}
}
