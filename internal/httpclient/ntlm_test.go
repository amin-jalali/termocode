package httpclient

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// NTOWFv2 from MS-NLMP 4.2.4.1.1 (User / Domain / Password).
func TestNTLMv2HashVector(t *testing.T) {
	got := hex.EncodeToString(ntowfv2("User", "Domain", "Password"))
	if got != "0c868a403bfd7a93a3001ef22ef02e3f" {
		t.Fatalf("NTOWFv2 = %s", got)
	}
}

func TestParseNTLMAuth(t *testing.T) {
	a, err := ParseAuth(`ntlm CORP\ann s3cret`)
	if err != nil || a.Scheme != AuthNTLM || a.User != `CORP\ann` || a.Pass != "s3cret" {
		t.Fatalf("%+v %v", a, err)
	}
	rr, un := (&Resolver{}).Resolve(Request{Method: "GET", URL: "http://x/", Directives: map[string]string{"auth": `ntlm CORP\ann pw`}})
	if len(un) != 0 || rr.NTLM == nil || rr.NTLM.Domain != "CORP" || rr.NTLM.User != "ann" || rr.HasHeader("Authorization") {
		t.Fatalf("resolved %+v %v", rr.NTLM, un)
	}
}

// The three-message handshake: negotiate, challenge, authenticate with
// the user and domain the server reads back.
func TestNTLMHandshake(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "NTLM ") {
			w.Header().Set("WWW-Authenticate", "NTLM")
			w.WriteHeader(401)
			return
		}
		msg, err := base64.StdEncoding.DecodeString(h[5:])
		if err != nil || len(msg) < 12 || string(msg[:8]) != "NTLMSSP\x00" {
			w.WriteHeader(400)
			return
		}
		switch binary.LittleEndian.Uint32(msg[8:]) {
		case 1:
			ch := make([]byte, 48)
			copy(ch, "NTLMSSP\x00")
			binary.LittleEndian.PutUint32(ch[8:], 2)
			binary.LittleEndian.PutUint32(ch[20:], 0x00088201)
			copy(ch[24:], []byte{1, 2, 3, 4, 5, 6, 7, 8})
			ti := []byte{0, 0, 0, 0} // MsvAvEOL
			binary.LittleEndian.PutUint16(ch[40:], uint16(len(ti)))
			binary.LittleEndian.PutUint16(ch[42:], uint16(len(ti)))
			binary.LittleEndian.PutUint32(ch[44:], 48)
			ch = append(ch, ti...)
			w.Header().Set("WWW-Authenticate", "NTLM "+base64.StdEncoding.EncodeToString(ch))
			w.WriteHeader(401)
		case 3:
			user := ntlmField(msg, 36)
			dom := ntlmField(msg, 28)
			nt := ntlmField(msg, 20)
			if utf16le("ann") != string(user) || utf16le("CORP") != string(dom) || len(nt) < 48 {
				w.WriteHeader(403)
				return
			}
			_, _ = w.Write([]byte("ok"))
		}
	}))
	defer srv.Close()
	rr, _ := (&Resolver{}).Resolve(Request{Method: "GET", URL: srv.URL + "/", Directives: map[string]string{"auth": `ntlm CORP\ann pw`}})
	resp := New(Options{}).Send(context.Background(), rr, Hooks{})
	if resp.Status != 200 || string(resp.Body) != "ok" {
		t.Fatalf("status %d body %q err %s", resp.Status, resp.Body, resp.Err)
	}
}

func ntlmField(msg []byte, at int) []byte {
	n := int(binary.LittleEndian.Uint16(msg[at:]))
	off := int(binary.LittleEndian.Uint32(msg[at+4:]))
	if off+n > len(msg) {
		return nil
	}
	return msg[off : off+n]
}

func utf16le(s string) string { return string(ntlmUnicode(s)) }
