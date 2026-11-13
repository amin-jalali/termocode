package dbclient

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// AWS Signature Version 4 with the standard library only (DBX-66), for
// the AWS JSON APIs (Athena, DynamoDB). Only the access key id appears
// in a request; the secret key signs and is never sent.

// awsCreds is an access key (and the session token of temporary ones).
type awsCreds struct {
	key, secret, token string
}

func hmacSHA256(key []byte, s string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(s))
	return h.Sum(nil)
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// sigV4Sign sets X-Amz-Date (and X-Amz-Security-Token) and the
// Authorization header of r, whose body is body. It signs the host and
// every x-amz-* and content-type header.
func sigV4Sign(r *http.Request, body []byte, c awsCreds, region, service string, now time.Time) {
	now = now.UTC()
	stamp, day := now.Format("20060102T150405Z"), now.Format("20060102")
	r.Header.Set("X-Amz-Date", stamp)
	if c.token != "" {
		r.Header.Set("X-Amz-Security-Token", c.token)
	}
	headers := map[string]string{"host": r.URL.Host}
	if r.Host != "" {
		headers["host"] = r.Host
	}
	for k, v := range r.Header {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "x-amz-") || lk == "content-type" {
			headers[lk] = strings.TrimSpace(strings.Join(v, ","))
		}
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + headers[k] + "\n")
	}
	signed := strings.Join(names, ";")
	path := r.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	canon := strings.Join([]string{r.Method, path, r.URL.Query().Encode(), canonHeaders.String(), signed, sha256Hex(body)}, "\n")
	scope := day + "/" + region + "/" + service + "/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + stamp + "\n" + scope + "\n" + sha256Hex([]byte(canon))
	k := hmacSHA256([]byte("AWS4"+c.secret), day)
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, service)
	k = hmacSHA256(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(k, toSign))
	r.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+c.key+"/"+scope+", SignedHeaders="+signed+", Signature="+sig)
}

// sigV4Transport signs every request it sends.
type sigV4Transport struct {
	creds           awsCreds
	region, service string
	next            http.RoundTripper
}

func (t *sigV4Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	var body []byte
	if r.Body != nil {
		var err error
		if body, err = io.ReadAll(r.Body); err != nil {
			return nil, err
		}
		_ = r.Body.Close()
	}
	r = r.Clone(r.Context())
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Del("Authorization")
	sigV4Sign(r, body, t.creds, t.region, t.service, time.Now())
	return t.next.RoundTrip(r)
}

// awsJSONEngine is the shared part of an AWS JSON API session: a POST
// to / with X-Amz-Target and a JSON body, signed with SigV4. Errors are
// {"__type": …, "message"/"Message": …}.
type awsJSONEngine struct {
	*httpEngine
	target string // "AmazonAthena." or "DynamoDB_20120810."
	ctype  string // application/x-amz-json-1.1 or 1.0
}

// newAWSJSONEngine builds the session of c: the user is the access key
// id, the password the secret key (a session token after a newline or a
// "|" when the key is temporary), the region params.region or the host's.
func newAWSJSONEngine(title, service string, c Conn, secret, target, ctype string) (*awsJSONEngine, error) {
	region := warehouseParam(c, "region")
	if region == "" {
		region = awsHostRegion(c.Host, service)
	}
	if region == "" {
		return nil, errors.New(title + `: no region: set "params": {"region": "eu-west-1"} or use the regional host`)
	}
	if c.URL == "" {
		if c.Host == "" {
			c.Host = service + "." + region + ".amazonaws.com"
		}
		c.TLS = true
	}
	key := c.User
	if key == "" {
		return nil, errors.New(title + ": no access key id: it is the connection's user")
	}
	c.User = ""
	e, err := newHTTPEngine(title, c, "")
	if err != nil {
		return nil, err
	}
	sec, tok, _ := strings.Cut(strings.TrimSpace(secret), "\n")
	if !strings.Contains(secret, "\n") {
		sec, tok, _ = strings.Cut(strings.TrimSpace(secret), "|")
	}
	e.password = sec // scrubbed from errors
	e.client.Transport = &sigV4Transport{creds: awsCreds{key: key, secret: sec, token: strings.TrimSpace(tok)}, region: region, service: service, next: e.client.Transport}
	return &awsJSONEngine{httpEngine: e, target: target, ctype: ctype}, nil
}

// awsHostRegion is the region in a regional host (athena.eu-west-1.amazonaws.com).
func awsHostRegion(host, service string) string {
	rest, ok := strings.CutPrefix(strings.ToLower(host), service+".")
	if !ok {
		return ""
	}
	region, _, ok := strings.Cut(rest, ".")
	if !ok {
		return ""
	}
	return region
}

// call runs one action and decodes its answer into out (nil: ignored).
// An error answer is its type and message.
func (a *awsJSONEngine) call(parent context.Context, action string, in, out any) error {
	ctx, done, err := a.run(parent)
	if err != nil {
		return err
	}
	defer done()
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", a.base+"/", bytes.NewReader(body))
	if err != nil {
		return a.clean(err)
	}
	req.Header.Set("Content-Type", a.ctype)
	req.Header.Set("X-Amz-Target", a.target+action)
	resp, err := a.client.Do(req)
	if err != nil {
		return a.clean(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, httpEngineMaxBody))
	if err != nil {
		return a.clean(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Type    string `json:"__type"`
			Message string `json:"message"`
			Upper   string `json:"Message"`
		}
		_ = json.Unmarshal(data, &e)
		msg := e.Message
		if msg == "" {
			msg = e.Upper
		}
		if _, t, ok := strings.Cut(e.Type, "#"); ok {
			e.Type = t
		}
		switch {
		case msg != "" && e.Type != "":
			return fmt.Errorf("%s: %s: %s", a.title, e.Type, a.scrub(msg))
		case msg != "":
			return fmt.Errorf("%s: %s", a.title, a.scrub(msg))
		}
		return &httpError{Status: resp.StatusCode, Msg: a.scrub(httpErrorMessage(data))}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s: unexpected answer: %v", a.title, err)
	}
	return nil
}
