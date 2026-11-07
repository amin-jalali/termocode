package ai

import (
	"bufio"
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
)

// Claude through a cloud account (CMP-85): Google Vertex AI and Amazon
// Bedrock. Both take the Anthropic Messages body (buildAnthropicBody),
// with the model in the URL instead of the body and their own
// anthropic_version; the answer is parsed by parseAnthropicSSE.
//
//   - vertex: ai_base_url is
//     https://<region>-aiplatform.googleapis.com/v1/projects/<project>/locations/<region>
//     and the key (in the secret store, like every key) is an OAuth
//     access token, or a Google service-account or authorized-user JSON
//     (one line). A JSON key is exchanged for an access token, kept in
//     memory until a minute before it ends. The request goes to
//     <base>/publishers/anthropic/models/<model>:streamRawPredict.
//   - bedrock: ai_base_url is https://bedrock-runtime.<region>.amazonaws.com
//     (AWS_REGION when not set; us-east-1 else) and the key is a Bedrock
//     API key (AWS_BEARER_TOKEN_BEDROCK wins). The request goes to
//     <base>/model/<model>/invoke-with-response-stream and the answer is
//     an AWS event stream whose chunks carry the Anthropic events.

// The cloud provider kinds.
const (
	KindVertex  = "vertex"
	KindBedrock = "bedrock"
)

const (
	vertexAnthropicVersion  = "vertex-2023-10-16"
	bedrockAnthropicVersion = "bedrock-2023-05-31"
	vertexScope             = "https://www.googleapis.com/auth/cloud-platform"
	vertexDefaultTokenURL   = "https://oauth2.googleapis.com/token"
	bedrockDefaultRegion    = "us-east-1"
)

// isCloudClaude: kind is Vertex or Bedrock.
func isCloudClaude(kind string) bool { return kind == KindVertex || kind == KindBedrock }

// bedrockBaseURL is the Bedrock runtime endpoint of region.
func bedrockBaseURL(region string) string {
	region = strings.TrimSpace(region)
	if region == "" {
		region = bedrockDefaultRegion
	}
	return "https://bedrock-runtime." + region + ".amazonaws.com"
}

// cloudURL is the request URL of model on a cloud kind.
func cloudURL(kind, base, model string) string {
	base = trimSlash(base)
	if kind == KindVertex {
		return base + "/publishers/anthropic/models/" + url.PathEscape(model) + ":streamRawPredict"
	}
	return base + "/model/" + url.PathEscape(model) + "/invoke-with-response-stream"
}

// cloudBody fits an Anthropic body to a cloud kind: no model (it is in
// the URL), the kind's anthropic_version, and no stream field on Bedrock
// (the URL says it streams; Bedrock rejects the field).
func cloudBody(kind string, body map[string]any) {
	delete(body, "model")
	if kind == KindVertex {
		body["anthropic_version"] = vertexAnthropicVersion
		return
	}
	delete(body, "stream")
	body["anthropic_version"] = bedrockAnthropicVersion
}

// cloudPriceModel is the model id the price table knows: Bedrock's
// "us.anthropic.claude-…" loses its prefix, Vertex's "claude-…@date"
// its date.
func cloudPriceModel(kind, model string) string {
	switch kind {
	case KindBedrock:
		if i := strings.Index(model, "anthropic."); i >= 0 {
			return model[i+len("anthropic."):]
		}
	case KindVertex:
		if i := strings.IndexByte(model, '@'); i > 0 {
			return model[:i]
		}
	}
	return model
}

// runCloud sends one request to Vertex or Bedrock.
func (p *anthropicProvider) runCloud(ctx context.Context, req Request, model string, ch chan<- Chunk) Chunk {
	if model == "" {
		return Chunk{Done: true, Err: errString("set ai_model to a " + KindLabel(p.cfg.Kind) + " model id (AI: Pick Model… → Custom…)")}
	}
	token := p.cfg.APIKey
	if p.cfg.Kind == KindVertex && p.cfg.Token != nil {
		t, err := p.cfg.Token(ctx)
		if err != nil {
			return Chunk{Done: true, Err: signInError{errString(Redact("Vertex AI sign-in: " + err.Error()))}}
		}
		token = t
	}
	body := anthropicBody(req, model, p.cfg)
	cloudBody(p.cfg.Kind, body)
	payload, err := marshalBody(body)
	if err != nil {
		return Chunk{Done: true, Err: err}
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, cloudURL(p.cfg.Kind, p.cfg.BaseURL, model), bytes.NewReader(payload))
	if err != nil {
		return Chunk{Done: true, Err: err}
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Authorization", "Bearer "+token)
	if p.cfg.Kind == KindBedrock {
		hreq.Header.Set("Accept", "application/vnd.amazon.eventstream")
	} else {
		hreq.Header.Set("Accept", "text/event-stream")
	}
	resp, err := p.cfg.HTTPClient.Do(hreq)
	if err != nil {
		return Chunk{Done: true, Err: requestError(err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return Chunk{Done: true, Err: httpError(resp.StatusCode, b)}
	}
	if p.cfg.Kind == KindVertex {
		return parseAnthropicSSE(ctx, resp.Body, ch)
	}
	pr, pw := io.Pipe()
	defer pr.Close() // stops the decoder when the parser returns early
	crash.Go("the Bedrock stream", func() { pw.CloseWithError(bedrockToSSE(resp.Body, pw)) })
	return parseAnthropicSSE(ctx, pr, ch)
}

// ── Bedrock event stream ──────────────────────────────────────────────

// bedrockMaxFrame bounds one event-stream message (a chunk is small).
const bedrockMaxFrame = 16 << 20

// bedrockToSSE reads AWS event-stream messages from r and writes their
// Anthropic events to w as SSE data lines. An exception message becomes
// an Anthropic error event; a damaged message (bad length or CRC) is an
// error.
func bedrockToSSE(r io.Reader, w io.Writer) error {
	br := bufio.NewReader(r)
	for {
		headers, payload, err := readEventFrame(br)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		var data []byte
		switch headers[":message-type"] {
		case "exception", "error":
			var e struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(payload, &e)
			kind := headers[":exception-type"]
			if kind == "" {
				kind = headers[":error-code"]
			}
			msg := e.Message
			if msg == "" {
				msg = kind
			}
			data, _ = json.Marshal(map[string]any{"type": "error", "error": map[string]string{"type": kind, "message": msg}})
		default:
			if headers[":event-type"] != "chunk" {
				continue
			}
			var c struct {
				Bytes string `json:"bytes"`
			}
			if err := json.Unmarshal(payload, &c); err != nil {
				return errors.New("bedrock: a stream chunk is not JSON")
			}
			data, err = base64.StdEncoding.DecodeString(c.Bytes)
			if err != nil {
				return errors.New("bedrock: a stream chunk is not base64")
			}
			data = bytes.ReplaceAll(data, []byte("\n"), nil)
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return err
		}
	}
}

// readEventFrame reads one event-stream message: its string headers and
// its payload. io.EOF only before the first byte of a message.
func readEventFrame(r io.Reader) (map[string]string, []byte, error) {
	var prelude [12]byte
	if _, err := io.ReadFull(r, prelude[:4]); err != nil {
		if err == io.EOF {
			return nil, nil, io.EOF
		}
		return nil, nil, errors.New("bedrock: the stream ended inside a message")
	}
	if _, err := io.ReadFull(r, prelude[4:]); err != nil {
		return nil, nil, errors.New("bedrock: the stream ended inside a message")
	}
	total := binary.BigEndian.Uint32(prelude[0:4])
	hlen := binary.BigEndian.Uint32(prelude[4:8])
	if crc32.ChecksumIEEE(prelude[:8]) != binary.BigEndian.Uint32(prelude[8:12]) {
		return nil, nil, errors.New("bedrock: a stream message is damaged (prelude CRC)")
	}
	if total < 16 || total > bedrockMaxFrame || uint64(hlen)+16 > uint64(total) {
		return nil, nil, errors.New("bedrock: a stream message has a bad length")
	}
	rest := make([]byte, total-12)
	if _, err := io.ReadFull(r, rest); err != nil {
		return nil, nil, errors.New("bedrock: the stream ended inside a message")
	}
	crc := crc32.NewIEEE()
	crc.Write(prelude[:])
	crc.Write(rest[:len(rest)-4])
	if crc.Sum32() != binary.BigEndian.Uint32(rest[len(rest)-4:]) {
		return nil, nil, errors.New("bedrock: a stream message is damaged (CRC)")
	}
	headers, err := parseEventHeaders(rest[:hlen])
	if err != nil {
		return nil, nil, err
	}
	return headers, rest[hlen : len(rest)-4], nil
}

// parseEventHeaders reads event-stream headers; string values are kept,
// the other types are skipped.
func parseEventHeaders(b []byte) (map[string]string, error) {
	out := map[string]string{}
	bad := errors.New("bedrock: a stream message has bad headers")
	for len(b) > 0 {
		n := int(b[0])
		if len(b) < 1+n+1 {
			return nil, bad
		}
		name := string(b[1 : 1+n])
		typ := b[1+n]
		b = b[2+n:]
		size := 0
		switch typ {
		case 0, 1: // bool
		case 2:
			size = 1
		case 3:
			size = 2
		case 4:
			size = 4
		case 5, 8:
			size = 8
		case 9:
			size = 16
		case 6, 7:
			if len(b) < 2 {
				return nil, bad
			}
			size = int(binary.BigEndian.Uint16(b[:2]))
			b = b[2:]
		default:
			return nil, bad
		}
		if len(b) < size {
			return nil, bad
		}
		if typ == 7 {
			out[name] = string(b[:size])
		}
		b = b[size:]
	}
	return out, nil
}

// ── Vertex AI access tokens ───────────────────────────────────────────

// vertexTokenURLOK reports whether a key's token_uri may get the signed
// assertion: https on a googleapis.com host (a seam for tests).
var vertexTokenURLOK = func(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	h := u.Hostname()
	return h == "googleapis.com" || strings.HasSuffix(h, ".googleapis.com")
}

// vertexKey is a Google credentials JSON: a service account or an
// authorized user (gcloud auth application-default login).
type vertexKey struct {
	Type         string `json:"type"`
	ClientEmail  string `json:"client_email"`
	PrivateKey   string `json:"private_key"`
	TokenURI     string `json:"token_uri"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
}

// vertexTokens turns a Vertex key into access tokens, one at a time.
type vertexTokens struct {
	key    string
	client *http.Client
	now    func() time.Time

	mu      sync.Mutex
	token   string
	expires time.Time
}

func newVertexTokens(key string, client *http.Client) *vertexTokens {
	return &vertexTokens{key: strings.TrimSpace(key), client: client, now: time.Now}
}

// get is an access token: the key itself when it is not JSON, else a
// cached or newly exchanged token.
func (v *vertexTokens) get(ctx context.Context) (string, error) {
	if !strings.HasPrefix(v.key, "{") {
		if v.key == "" {
			return "", errors.New("no key: set a Vertex AI access token or service-account JSON with AI: Configure Provider…")
		}
		return v.key, nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.token != "" && v.now().Before(v.expires.Add(-time.Minute)) {
		return v.token, nil
	}
	var k vertexKey
	if err := json.Unmarshal([]byte(v.key), &k); err != nil {
		return "", errors.New("the key is not valid JSON (paste the key file as one line)")
	}
	RegisterSecret(k.PrivateKey)
	RegisterSecret(k.ClientSecret)
	RegisterSecret(k.RefreshToken)
	form := url.Values{}
	tokenURL := k.TokenURI
	if tokenURL == "" {
		tokenURL = vertexDefaultTokenURL
	}
	switch k.Type {
	case "service_account":
		jwt, err := vertexAssertion(k, tokenURL, v.now())
		if err != nil {
			return "", err
		}
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
		form.Set("assertion", jwt)
	case "authorized_user":
		tokenURL = vertexDefaultTokenURL
		form.Set("grant_type", "refresh_token")
		form.Set("client_id", k.ClientID)
		form.Set("client_secret", k.ClientSecret)
		form.Set("refresh_token", k.RefreshToken)
	default:
		return "", fmt.Errorf("unknown key type %q (want service_account or authorized_user)", k.Type)
	}
	if !vertexTokenURLOK(tokenURL) {
		return "", errors.New("the key's token_uri is not a Google https address")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := v.client.Do(req)
	if err != nil {
		return "", requestError(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", httpError(resp.StatusCode, b)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.AccessToken == "" {
		return "", errors.New("the token answer has no access_token")
	}
	RegisterSecret(out.AccessToken)
	if out.ExpiresIn <= 0 {
		out.ExpiresIn = 3600
	}
	v.token, v.expires = out.AccessToken, v.now().Add(time.Duration(out.ExpiresIn)*time.Second)
	return v.token, nil
}

// vertexAssertion is the signed JWT a service account trades for an
// access token (RS256, one hour).
func vertexAssertion(k vertexKey, aud string, now time.Time) (string, error) {
	block, _ := pem.Decode([]byte(k.PrivateKey))
	if block == nil || k.ClientEmail == "" {
		return "", errors.New("the service-account key has no private_key or client_email")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		if pk, err1 := x509.ParsePKCS1PrivateKey(block.Bytes); err1 == nil {
			parsed = pk
		} else {
			return "", errors.New("the service-account private_key can not be read")
		}
	}
	pk, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return "", errors.New("the service-account private_key is not an RSA key")
	}
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	head := enc(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims := enc(map[string]any{"iss": k.ClientEmail, "scope": vertexScope, "aud": aud,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix()})
	sum := sha256.Sum256([]byte(head + "." + claims))
	sig, err := rsa.SignPKCS1v15(nil, pk, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return head + "." + claims + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
