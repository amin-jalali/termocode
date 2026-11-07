package ai

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// CMP-85: Claude through Google Vertex AI and Amazon Bedrock.

var cloudEvents = []string{
	`{"type":"message_start","message":{"model":"claude-opus-5-5","usage":{"input_tokens":5,"output_tokens":1}}}`,
	`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi "}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"cloud"}}`,
	`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`,
	`{"type":"message_stop"}`,
}

func TestVertexStreamWithAccessToken(t *testing.T) {
	base := "/v1/projects/p1/locations/us-east5"
	srv := sseServer(t, func(r *http.Request, body map[string]any) {
		if r.URL.Path != base+"/publishers/anthropic/models/claude-opus-5-5:streamRawPredict" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer ya29.test-token-0123456789" || r.Header.Get("x-api-key") != "" {
			t.Errorf("auth headers: %v", r.Header)
		}
		if _, ok := body["model"]; ok {
			t.Error("Vertex takes the model in the URL, not the body")
		}
		if body["anthropic_version"] != vertexAnthropicVersion || body["stream"] != true {
			t.Errorf("body: %v", body)
		}
	}, cloudEvents...)
	defer srv.Close()
	p := NewProvider(Config{Kind: KindVertex, BaseURL: srv.URL + base, APIKey: "ya29.test-token-0123456789", Model: "claude-opus-5-5"})
	if !p.Configured() {
		t.Fatal("not configured")
	}
	text, last := Collect(p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}))
	if text != "Hi cloud" || last.Err != nil {
		t.Fatalf("text=%q err=%v", text, last.Err)
	}
}

func TestVertexNeedsBaseURLAndKey(t *testing.T) {
	if (Config{Kind: KindVertex, APIKey: "tok"}).Configured() {
		t.Error("vertex without base URL is configured")
	}
	if (Config{Kind: KindVertex, BaseURL: "https://x"}).Configured() {
		t.Error("vertex without key is configured")
	}
}

// A service-account key is turned into an access token once and the
// token is used until it is near its end.
func TestVertexServiceAccountToken(t *testing.T) {
	pk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(pk)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	exchanges := 0
	tokSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges++
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Errorf("grant %q", r.Form.Get("grant_type"))
		}
		parts := strings.Split(r.Form.Get("assertion"), ".")
		if len(parts) != 3 {
			t.Fatalf("assertion %q", r.Form.Get("assertion"))
		}
		claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
		var c map[string]any
		_ = json.Unmarshal(claims, &c)
		if c["iss"] != "bot@p1.iam.gserviceaccount.com" || c["scope"] != vertexScope {
			t.Errorf("claims %v", c)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"ya29.minted-0123456789","expires_in":3600,"token_type":"Bearer"}`)
	}))
	defer tokSrv.Close()
	old := vertexTokenURLOK
	vertexTokenURLOK = func(string) bool { return true }
	defer func() { vertexTokenURLOK = old }()
	key, _ := json.Marshal(map[string]string{"type": "service_account", "client_email": "bot@p1.iam.gserviceaccount.com",
		"private_key": pemKey, "token_uri": tokSrv.URL})
	seen := 0
	srv := sseServer(t, func(r *http.Request, _ map[string]any) {
		seen++
		if r.Header.Get("Authorization") != "Bearer ya29.minted-0123456789" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
	}, cloudEvents...)
	defer srv.Close()
	p := NewProvider(Config{Kind: KindVertex, BaseURL: srv.URL, APIKey: string(key), Model: "claude-opus-5-5"})
	for i := 0; i < 2; i++ {
		if _, last := Collect(p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})); last.Err != nil {
			t.Fatal(last.Err)
		}
	}
	if exchanges != 1 || seen != 2 {
		t.Errorf("exchanges=%d requests=%d: the token must be reused", exchanges, seen)
	}
}

func TestVertexTokenURLMustBeGoogle(t *testing.T) {
	for u, want := range map[string]bool{
		"https://oauth2.googleapis.com/token": true,
		"http://oauth2.googleapis.com/token":  false,
		"https://evil.example/token":          false,
		"https://googleapis.com.evil/token":   false,
	} {
		if got := vertexTokenURLOK(u); got != want {
			t.Errorf("%s: %v, want %v", u, got, want)
		}
	}
}

// eventFrame is one AWS event-stream message.
func eventFrame(headers map[string]string, payload []byte) []byte {
	var hb bytes.Buffer
	for k, v := range headers {
		hb.WriteByte(byte(len(k)))
		hb.WriteString(k)
		hb.WriteByte(7)
		_ = binary.Write(&hb, binary.BigEndian, uint16(len(v)))
		hb.WriteString(v)
	}
	total := 12 + hb.Len() + len(payload) + 4
	var out bytes.Buffer
	_ = binary.Write(&out, binary.BigEndian, uint32(total))
	_ = binary.Write(&out, binary.BigEndian, uint32(hb.Len()))
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(out.Bytes()))
	out.Write(hb.Bytes())
	out.Write(payload)
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(out.Bytes()))
	return out.Bytes()
}

func chunkFrame(event string) []byte {
	p, _ := json.Marshal(map[string]string{"bytes": base64.StdEncoding.EncodeToString([]byte(event))})
	return eventFrame(map[string]string{":event-type": "chunk", ":message-type": "event", ":content-type": "application/json"}, p)
}

func TestBedrockStreamWithAPIKey(t *testing.T) {
	model := "us.anthropic.claude-opus-5-5-v1:0"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/model/"+model+"/invoke-with-response-stream" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer ABSK-test-key-0123456789" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		if _, ok := body["model"]; ok {
			t.Error("Bedrock takes the model in the URL")
		}
		if _, ok := body["stream"]; ok {
			t.Error("Bedrock rejects the stream field")
		}
		if body["anthropic_version"] != bedrockAnthropicVersion {
			t.Errorf("body %v", body)
		}
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		for _, e := range cloudEvents {
			_, _ = w.Write(chunkFrame(e))
		}
	}))
	defer srv.Close()
	p := NewProvider(Config{Kind: KindBedrock, BaseURL: srv.URL, APIKey: "ABSK-test-key-0123456789", Model: model})
	text, last := Collect(p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}))
	if text != "Hi cloud" || last.Err != nil {
		t.Fatalf("text=%q err=%v", text, last.Err)
	}
	if last.Usage == nil || last.Usage.Output != 3 {
		t.Errorf("usage %+v", last.Usage)
	}
}

func TestBedrockExceptionAndBadCRC(t *testing.T) {
	exc := eventFrame(map[string]string{":message-type": "exception", ":exception-type": "throttlingException"}, []byte(`{"message":"Too many requests"}`))
	bad := chunkFrame(cloudEvents[2])
	bad[len(bad)-1] ^= 0xff
	for name, frames := range map[string][]byte{"exception": exc, "crc": append(chunkFrame(cloudEvents[0]), bad...)} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(frames)
		}))
		p := NewProvider(Config{Kind: KindBedrock, BaseURL: srv.URL, APIKey: "ABSK-test-key-0123456789", Model: "m"})
		_, last := Collect(p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}))
		srv.Close()
		if last.Err == nil {
			t.Errorf("%s: no error", name)
		}
		if name == "exception" && !strings.Contains(last.Err.Error(), "Too many requests") {
			t.Errorf("exception text lost: %v", last.Err)
		}
	}
}

func TestBedrockNeedsModel(t *testing.T) {
	p := NewProvider(Config{Kind: KindBedrock, APIKey: "ABSK-test-key-0123456789"})
	_, last := Collect(p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}))
	if last.Err == nil || !strings.Contains(last.Err.Error(), "ai_model") {
		t.Errorf("err %v", last.Err)
	}
}

func TestResolveBedrockFromEnv(t *testing.T) {
	env := map[string]string{"AWS_BEARER_TOKEN_BEDROCK": "ABSK-env-0123456789", "AWS_REGION": "eu-west-1"}
	cfg := Resolve(Settings{Provider: KindBedrock, Model: "eu.anthropic.claude-sonnet-5-5-v1:0"}, Credentials{}, func(k string) string { return env[k] })
	if cfg.APIKey != "ABSK-env-0123456789" {
		t.Errorf("key %q", cfg.APIKey)
	}
	if cfg.BaseURL != "https://bedrock-runtime.eu-west-1.amazonaws.com" {
		t.Errorf("base %q", cfg.BaseURL)
	}
	if u, err := url.Parse(cfg.BaseURL); err != nil || u.Scheme != "https" {
		t.Errorf("bad url %q", cfg.BaseURL)
	}
}

func TestCloudPricingStripsTheModelPrefix(t *testing.T) {
	if got := cloudPriceModel(KindBedrock, "us.anthropic.claude-opus-5-5-v1:0"); got != "claude-opus-5-5-v1:0" {
		t.Errorf("bedrock: %q", got)
	}
	if got := cloudPriceModel(KindVertex, "claude-opus-5-5@20261001"); got != "claude-opus-5-5" {
		t.Errorf("vertex: %q", got)
	}
	if got := cloudPriceModel(KindAnthropic, "claude-opus-5-5"); got != "claude-opus-5-5" {
		t.Errorf("anthropic: %q", got)
	}
}
