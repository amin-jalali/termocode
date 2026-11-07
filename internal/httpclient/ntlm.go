package httpclient

// NTLM (`# @auth ntlm [DOMAIN\]USER PASS`): the three-message NTLMv2
// handshake (MS-NLMP) over one keep-alive connection. MD4 comes from
// golang.org/x/crypto (already a dependency).

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/crypto/md4" //nolint:staticcheck // NTLM needs MD4
)

// AuthNTLM is the `# @auth ntlm` scheme.
const AuthNTLM = "ntlm"

// NTLMAuth holds the credentials of an NTLM send. Never written to JSON.
type NTLMAuth struct {
	Domain string
	User   string
	Pass   string
}

// splitNTLMUser reads DOMAIN\user or user@domain.
func splitNTLMUser(s string) (domain, user string) {
	if d, u, ok := strings.Cut(s, `\`); ok {
		return d, u
	}
	if u, d, ok := strings.Cut(s, "@"); ok {
		return d, u
	}
	return "", s
}

func ntlmUnicode(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, r := range u {
		binary.LittleEndian.PutUint16(b[2*i:], r)
	}
	return b
}

func hmacMD5(key []byte, data ...[]byte) []byte {
	m := hmac.New(md5.New, key)
	for _, d := range data {
		m.Write(d)
	}
	return m.Sum(nil)
}

// ntowfv2 is the NTLMv2 key: HMAC-MD5(MD4(pass), UPPER(user)+domain).
func ntowfv2(user, domain, pass string) []byte {
	h := md4.New()
	h.Write(ntlmUnicode(pass))
	return hmacMD5(h.Sum(nil), ntlmUnicode(strings.ToUpper(user)+domain))
}

const (
	ntlmUnicodeFlag = 0x00000001
	ntlmNegotiate   = 0x00088207 // unicode, OEM, request target, NTLM, always sign, extended session security
)

func ntlmNegotiateMsg() []byte {
	b := make([]byte, 32)
	copy(b, "NTLMSSP\x00")
	binary.LittleEndian.PutUint32(b[8:], 1)
	binary.LittleEndian.PutUint32(b[12:], ntlmNegotiate)
	return b
}

// ntlmChallenge reads a type 2 message: server challenge, flags and
// target info.
func ntlmChallenge(msg []byte) (chal []byte, flags uint32, info []byte, ok bool) {
	if len(msg) < 32 || string(msg[:8]) != "NTLMSSP\x00" || binary.LittleEndian.Uint32(msg[8:]) != 2 {
		return nil, 0, nil, false
	}
	flags = binary.LittleEndian.Uint32(msg[20:])
	chal = msg[24:32]
	if len(msg) >= 48 {
		n := int(binary.LittleEndian.Uint16(msg[40:]))
		off := int(binary.LittleEndian.Uint32(msg[44:]))
		if off >= 0 && n >= 0 && off+n <= len(msg) {
			info = msg[off : off+n]
		}
	}
	return chal, flags, info, true
}

// ntlmAuthenticateMsg builds the type 3 message (NTLMv2 + LMv2).
func ntlmAuthenticateMsg(a NTLMAuth, chal []byte, flags uint32, info []byte, now time.Time) []byte {
	key := ntowfv2(a.User, a.Domain, a.Pass)
	client := make([]byte, 8)
	_, _ = rand.Read(client)
	ts := make([]byte, 8)
	binary.LittleEndian.PutUint64(ts, uint64(now.UnixNano()/100+116444736000000000))
	var blob bytes.Buffer
	blob.Write([]byte{1, 1, 0, 0, 0, 0, 0, 0})
	blob.Write(ts)
	blob.Write(client)
	blob.Write([]byte{0, 0, 0, 0})
	blob.Write(info)
	blob.Write([]byte{0, 0, 0, 0})
	proof := hmacMD5(key, chal, blob.Bytes())
	nt := append(proof, blob.Bytes()...)
	lm := append(hmacMD5(key, chal, client), client...)
	str := func(s string) []byte {
		if flags&ntlmUnicodeFlag != 0 {
			return ntlmUnicode(s)
		}
		return []byte(s)
	}
	fields := [][]byte{lm, nt, str(a.Domain), str(a.User), str(""), nil} // LM, NT, domain, user, workstation, session key
	const head = 64
	out := make([]byte, head)
	copy(out, "NTLMSSP\x00")
	binary.LittleEndian.PutUint32(out[8:], 3)
	off := head
	for i, f := range fields {
		at := 12 + 8*i
		binary.LittleEndian.PutUint16(out[at:], uint16(len(f)))
		binary.LittleEndian.PutUint16(out[at+2:], uint16(len(f)))
		binary.LittleEndian.PutUint32(out[at+4:], uint32(off))
		off += len(f)
	}
	binary.LittleEndian.PutUint32(out[60:], flags&^0x40000000) // no key exchange
	for _, f := range fields {
		out = append(out, f...)
	}
	return out
}

// ntlmTransport runs the handshake when the server asks for NTLM.
type ntlmTransport struct {
	base http.RoundTripper
	cred NTLMAuth
}

func ntlmHeader(resp *http.Response) (string, bool) {
	for _, v := range resp.Header.Values("WWW-Authenticate") {
		v = strings.TrimSpace(v)
		if strings.EqualFold(v, "ntlm") {
			return "", true
		}
		if len(v) > 5 && strings.EqualFold(v[:5], "ntlm ") {
			return strings.TrimSpace(v[5:]), true
		}
	}
	return "", false
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
}

func (t *ntlmTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		return t.base.RoundTrip(req)
	}
	again := func(auth string) (*http.Request, error) {
		r := req.Clone(req.Context())
		if req.GetBody != nil {
			b, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			r.Body = b
		}
		r.Header.Set("Authorization", auth)
		return r, nil
	}
	r1, err := again("NTLM " + base64.StdEncoding.EncodeToString(ntlmNegotiateMsg()))
	if err != nil {
		return nil, err
	}
	resp, err := t.base.RoundTrip(r1)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	enc, ok := ntlmHeader(resp)
	if !ok || enc == "" {
		return resp, nil
	}
	msg, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return resp, nil
	}
	chal, flags, info, ok := ntlmChallenge(msg)
	if !ok {
		return resp, nil
	}
	drain(resp) // keep the connection: NTLM authenticates it
	r3, err := again("NTLM " + base64.StdEncoding.EncodeToString(ntlmAuthenticateMsg(t.cred, chal, flags, info, time.Now())))
	if err != nil {
		return nil, err
	}
	return t.base.RoundTrip(r3)
}
