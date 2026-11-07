// Package lspproxy runs a local language server for files that live in
// a container (plan RC, phase 5). The editor names files by their
// container path (/app/main.go); the server sees the same files in a
// local folder (a bind mount or the read-only mirror). The proxy sits
// on the server's stdio and rewrites every file:// URI, rootUri,
// rootPath and workspace folder between the two prefixes, both ways.
//
//	termocode lsp-proxy --map /app=/home/u/proj -- gopls
//
// Nothing runs in the container and nothing is written there.
package lspproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// MaxMessage is the largest message the proxy accepts (64 MB): a
// bigger Content-Length is a broken stream, not a real message.
const MaxMessage = 64 << 20

// Map is one path pair: Remote is the folder as the editor names it
// (inside the container), Local the same folder on this machine.
type Map struct {
	Remote string
	Local  string
}

// ParseMap reads "REMOTE=LOCAL". Both must be absolute.
func ParseMap(s string) (Map, error) {
	r, l, ok := strings.Cut(s, "=")
	if !ok || r == "" || l == "" {
		return Map{}, fmt.Errorf("--map wants REMOTE=LOCAL, got %q", s)
	}
	if !path.IsAbs(r) || !path.IsAbs(l) {
		return Map{}, fmt.Errorf("--map paths must be absolute: %q", s)
	}
	return Map{Remote: path.Clean(r), Local: path.Clean(l)}, nil
}

// Rewriter swaps the path prefix From for To in the URIs and paths of a
// message.
type Rewriter struct {
	From, To string
}

// ToServer rewrites editor → server (container paths become local).
func (m Map) ToServer() Rewriter { return Rewriter{From: m.Remote, To: m.Local} }

// ToClient rewrites server → editor (local paths become container ones).
func (m Map) ToClient() Rewriter { return Rewriter{From: m.Local, To: m.Remote} }

// pathKeys are the keys whose value is a plain path, not a URI.
var pathKeys = map[string]bool{"rootPath": true}

// Rewrite returns body with every matching URI and path rewritten. A
// body that is not JSON, or has nothing to change, comes back as is.
func (rw Rewriter) Rewrite(body []byte) []byte {
	if rw.From == "" || rw.From == rw.To || !bytes.Contains(body, []byte(rw.probe())) {
		return body
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return body
	}
	changed := false
	v = rw.walk(v, "", &changed)
	if !changed {
		return body
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return body
	}
	return bytes.TrimRight(out.Bytes(), "\n")
}

// probe is a cheap substring every message with a match must contain.
func (rw Rewriter) probe() string {
	if i := strings.IndexByte(rw.From[1:], '/'); i >= 0 {
		return rw.From[:i+1]
	}
	return rw.From
}

func (rw Rewriter) walk(v any, key string, changed *bool) any {
	switch t := v.(type) {
	case map[string]any:
		// Keys can be URIs too (WorkspaceEdit.changes); rename them after
		// the loop so a new key is never visited again.
		renamed := map[string]string{}
		for k, val := range t {
			t[k] = rw.walk(val, k, changed)
			if nk, ok := rw.URI(k); ok {
				renamed[k] = nk
			}
		}
		for k, nk := range renamed {
			t[nk] = t[k]
			delete(t, k)
			*changed = true
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = rw.walk(val, key, changed)
		}
		return t
	case string:
		if pathKeys[key] {
			if s, ok := rw.Path(t); ok {
				*changed = true
				return s
			}
			return t
		}
		if s, ok := rw.URI(t); ok {
			*changed = true
			return s
		}
	}
	return v
}

// Path rewrites a plain path under From (ok false: not under From).
func (rw Rewriter) Path(p string) (string, bool) {
	if p == rw.From {
		return rw.To, true
	}
	if rest, ok := strings.CutPrefix(p, strings.TrimSuffix(rw.From, "/")+"/"); ok {
		return path.Join(rw.To, rest), true
	}
	return p, false
}

// URI rewrites a file:// URI under From (ok false: anything else).
// The path may be percent-encoded; the result always is.
func (rw Rewriter) URI(s string) (string, bool) {
	rest, ok := strings.CutPrefix(s, "file://")
	if !ok {
		return s, false
	}
	frag := ""
	if i := strings.IndexAny(rest, "?#"); i >= 0 {
		rest, frag = rest[:i], rest[i:]
	}
	p, err := url.PathUnescape(rest)
	if err != nil {
		return s, false
	}
	np, ok := rw.Path(p)
	if !ok {
		return s, false
	}
	return FileURI(np) + frag, true
}

// FileURI is the file:// URI of an absolute path, each segment escaped.
func FileURI(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "file://" + strings.Join(segs, "/")
}

// ReadMessage reads one framed message (headers, blank line, body).
func ReadMessage(r *bufio.Reader) ([]byte, error) {
	tp := textproto.NewReader(r)
	hdr, err := tp.ReadMIMEHeader()
	if err != nil {
		if errors.Is(err, io.EOF) && len(hdr) == 0 {
			return nil, io.EOF
		}
		return nil, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(hdr.Get("Content-Length")))
	if err != nil || n < 0 {
		return nil, errors.New("lsp-proxy: message without a valid Content-Length")
	}
	if n > MaxMessage {
		return nil, fmt.Errorf("lsp-proxy: message of %d bytes is too large", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// WriteMessage frames body and writes it.
func WriteMessage(w io.Writer, body []byte) error {
	if _, err := io.WriteString(w, "Content-Length: "+strconv.Itoa(len(body))+"\r\n\r\n"); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}

// Pump copies messages from src to dst through rw until src ends.
func Pump(dst io.Writer, src io.Reader, rw Rewriter) error {
	br := bufio.NewReaderSize(src, 64<<10)
	for {
		body, err := ReadMessage(br)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if err := WriteMessage(dst, rw.Rewrite(body)); err != nil {
			return err
		}
	}
}

// Run starts argv in m.Local and proxies stdio between in/out and the
// server. It returns when the server exits.
func Run(ctx context.Context, m Map, argv []string, in io.Reader, out, errw io.Writer) error {
	if len(argv) == 0 {
		return errors.New("lsp-proxy: no server command after --")
	}
	cmd := spawn.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = m.Local
	cmd.Stderr = errw
	sin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	sout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	crash.Go("lspproxy.Run", func() {
		_ = Pump(sin, in, m.ToServer())
		_ = sin.Close()
	})
	perr := Pump(out, sout, m.ToClient())
	werr := cmd.Wait()
	// The editor side ends with the server; do not wait on a stdin
	// that may never close.
	if werr != nil {
		return werr
	}
	return perr
}
