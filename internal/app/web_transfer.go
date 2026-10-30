package app

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// File upload and download over the WebSocket (WEB-49, web design v2).
// The bytes go base64 in chunks of webproto.TransferChunk, one request
// each, so the op table's rates and caps hold for every chunk; a file is
// at most webproto.MaxTransfer. Every path is resolved inside the
// workspace. Download refuses a file that holds secrets (.env, keys,
// credentials: the same rule as the file view) and fails when the file
// changed between chunks. An upload is staged in a private temporary
// folder (0700, the file 0600) outside the workspace; saving moves it in
// only under a free name; replacing an existing file waits for a laptop
// card. A container workspace keeps transfers on the laptop.

const (
	webMaxUploads      = 2  // staged uploads per connection
	webMaxUploadsTotal = 4  // staged uploads on the hub
	webUploadIDBytes   = 8  // hex: 16 characters
)

func init() {
	webOps[webproto.OpFileDownload] = (*Model).webFileDownload
	webOps[webproto.OpFileUploadBegin] = (*Model).webUploadBegin
	webOps[webproto.OpFileUploadChunk] = (*Model).webUploadChunk
	webOps[webproto.OpFileUploadSave] = (*Model).webUploadSave
	webOps[webproto.OpFileUploadCancel] = (*Model).webUploadCancel
	webAskOps[webproto.OpFileUploadReplace] = (*Model).webUploadReplace
	webAfterUpdates = append(webAfterUpdates, (*Model).webUploadsPrune)
}

// webUpload is one staged upload.
type webUpload struct {
	conn  string // the connection that started it
	rel   string // the workspace path it goes to
	abs   string // its absolute path (resolved at begin)
	tmp   string // the staged file
	size  int64
	got   int64
	ready bool // all bytes are here
}

// webUploads are the staged uploads (Update and the hub's stop).
type webUploads struct {
	mu  sync.Mutex
	dir string
	by  map[string]*webUpload
}

// stage makes the private folder and a new staged file for u.
func (s *webUploads) stage(u *webUpload) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.by) >= webMaxUploadsTotal {
		return "", errors.New("too many uploads at once")
	}
	n := 0
	for _, x := range s.by {
		if x.conn == u.conn {
			n++
		}
	}
	if n >= webMaxUploads {
		return "", errors.New("too many uploads at once")
	}
	if s.dir == "" {
		d, err := os.MkdirTemp("", "termocode-web-upload-")
		if err != nil {
			return "", err
		}
		s.dir = d
	}
	var b [webUploadIDBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b[:])
	u.tmp = filepath.Join(s.dir, id)
	f, err := os.OpenFile(u.tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	_ = f.Close()
	if s.by == nil {
		s.by = map[string]*webUpload{}
	}
	s.by[id] = u
	return id, nil
}

// get returns the upload id of conn.
func (s *webUploads) get(id, conn string) *webUpload {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u := s.by[id]; u != nil && u.conn == conn {
		return u
	}
	return nil
}

// drop forgets an upload and removes its staged file.
func (s *webUploads) drop(id string) {
	s.mu.Lock()
	u := s.by[id]
	delete(s.by, id)
	s.mu.Unlock()
	if u != nil {
		_ = os.Remove(u.tmp)
	}
}

// dropAll removes every staged file and the folder (the hub stops).
func (s *webUploads) dropAll() {
	s.mu.Lock()
	dir := s.dir
	s.by, s.dir = nil, ""
	s.mu.Unlock()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
}

// webUploadsPrune drops the uploads of connections that are gone.
func (m *Model) webUploadsPrune(tea.Msg) tea.Cmd {
	h := m.web
	h.ups.mu.Lock()
	var gone []string
	for id, u := range h.ups.by {
		if !h.webConnKnown(u.conn) {
			gone = append(gone, id)
		}
	}
	h.ups.mu.Unlock()
	for _, id := range gone {
		h.ups.drop(id)
	}
	return nil
}

// webSecretFile is a file whose bytes never leave the laptop.
func webSecretFile(p string) bool { return ai.IsSecretFileName(p) || ai.SecretFile(p) }

// webFileDownload answers one chunk of a file.
func (m *Model) webFileDownload(call *webCall) tea.Cmd {
	a := call.args.(*webproto.DownloadArgs)
	rel := webCleanRel(a.Path)
	if !m.webFileOpsLocal(call) {
		return nil
	}
	abs, err := m.webResolve(rel)
	if err != nil {
		webFailErr(call, err, "")
		return nil
	}
	real, _ := filepath.EvalSymlinks(abs)
	if webSecretFile(abs) || webSecretFile(real) {
		call.fail(webproto.CodeForbidden, "This file may hold secrets. It stays on the laptop.")
		return nil
	}
	if a.From == 0 {
		m.webAuditOp(call, rel, "download")
	}
	call.later()
	return func() tea.Msg {
		f, err := os.Open(abs)
		if err != nil {
			call.fail(webproto.CodeNotFound, "That file cannot be read.")
			return nil
		}
		defer f.Close()
		fi, err := f.Stat()
		switch {
		case err != nil || !fi.Mode().IsRegular():
			call.fail(webproto.CodeBadRequest, "Only a file can be downloaded.")
			return nil
		case fi.Size() > webproto.MaxTransfer:
			call.fail(webproto.CodeLimit, "This file is over 256 MB. Copy it on the laptop.")
			return nil
		case a.From > fi.Size():
			call.fail(webproto.CodeConflict, "The file changed. Download it again.")
			return nil
		}
		buf := make([]byte, min(int64(webproto.TransferChunk), fi.Size()-a.From))
		n, err := f.ReadAt(buf, a.From)
		if err != nil && !errors.Is(err, io.EOF) {
			call.fail(webproto.CodeInternal, "The file could not be read.")
			return nil
		}
		out := webproto.FileChunk{Path: rel, Size: fi.Size(), From: a.From, Data: base64.StdEncoding.EncodeToString(buf[:n]),
			Mtime: fi.ModTime().UnixMilli()}
		if end := a.From + int64(n); end < fi.Size() {
			out.Next = end
		}
		call.reply(out)
		return nil
	}
}

// webUploadBegin stages a new upload to a path whose folder resolves
// inside the workspace.
func (m *Model) webUploadBegin(call *webCall) tea.Cmd {
	a := call.args.(*webproto.UploadBeginArgs)
	rel := webCleanRel(a.Path)
	if !m.webFileOpsLocal(call) {
		return nil
	}
	dir, name := path.Split(rel)
	parent, err := m.webResolve(dir)
	if err != nil {
		webFailErr(call, err, "")
		return nil
	}
	if fi, err := os.Stat(parent); err != nil || !fi.IsDir() {
		call.fail(webproto.CodeNotFound, "That folder does not exist.")
		return nil
	}
	abs := filepath.Join(parent, name)
	exists := false
	if fi, err := os.Lstat(abs); err == nil {
		if !fi.Mode().IsRegular() {
			call.fail(webproto.CodeConflict, "Something that is not a file has this name.")
			return nil
		}
		exists = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		call.fail(webproto.CodeInternal, "That name cannot be used.")
		return nil
	}
	u := &webUpload{conn: call.conn.id, rel: rel, abs: abs, size: a.Size, ready: a.Size == 0}
	id, err := m.web.ups.stage(u)
	if err != nil {
		call.fail(webproto.CodeBusy, "Too many uploads at once. Finish one first.")
		return nil
	}
	m.webAuditOp(call, rel+" "+strconv.FormatInt(a.Size, 10)+" bytes", "begin")
	call.reply(webproto.UploadStarted{ID: id, Chunk: webproto.TransferChunk, Exists: exists})
	return nil
}

// webUploadChunk appends the next chunk; it must start where the staged
// bytes end and stay within the size given at begin.
func (m *Model) webUploadChunk(call *webCall) tea.Cmd {
	a := call.args.(*webproto.UploadChunkArgs)
	u := m.web.ups.get(a.ID, call.conn.id)
	if u == nil {
		call.fail(webproto.CodeNotFound, "That upload is gone. Start again.")
		return nil
	}
	b := a.Bytes()
	if a.Off != u.got || u.got+int64(len(b)) > u.size {
		call.fail(webproto.CodeConflict, "The upload is out of step. Start again.")
		m.web.ups.drop(a.ID)
		return nil
	}
	f, err := os.OpenFile(u.tmp, os.O_WRONLY, 0o600)
	if err == nil {
		_, err = f.WriteAt(b, a.Off)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		m.web.ups.drop(a.ID)
		call.fail(webproto.CodeInternal, "The upload could not be stored. "+fileOpFailText(err))
		return nil
	}
	u.got += int64(len(b))
	u.ready = u.got == u.size
	call.reply(webproto.UploadProgress{Got: u.got})
	return nil
}

// webUploadReady returns a complete upload of call's connection.
func (m *Model) webUploadReady(call *webCall, id string) *webUpload {
	u := m.web.ups.get(id, call.conn.id)
	switch {
	case u == nil:
		call.fail(webproto.CodeNotFound, "That upload is gone. Start again.")
		return nil
	case !u.ready:
		call.fail(webproto.CodeConflict, "The upload is not complete.")
		return nil
	}
	return u
}

// webUploadTarget resolves the upload's folder again (Update): it must
// still be the same folder inside the workspace. It returns the folder
// and its real path for webUploadPut.
func (m *Model) webUploadTarget(u *webUpload) (parent, real string, err error) {
	dir, _ := path.Split(u.rel)
	parent, err = m.webResolve(dir)
	if err != nil || filepath.Join(parent, filepath.Base(u.abs)) != u.abs {
		return "", "", errors.New("the folder changed")
	}
	real, err = filepath.EvalSymlinks(parent)
	return parent, real, err
}

// webUploadPut puts the staged bytes at u.abs, off the UI thread:
// through a temporary file in the target folder (the same disk), then a
// link that fails on an existing name (save) or a rename over it
// (replace, confirmed on the laptop). The folder must still be real at
// the end.
func webUploadPut(u *webUpload, parent, real string, replace bool) error {
	src, err := os.Open(u.tmp)
	if err != nil {
		return err
	}
	defer src.Close()
	tmp, err := os.CreateTemp(parent, ".termocode-upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = io.Copy(tmp, src)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	if err != nil {
		return err
	}
	if now, err := filepath.EvalSymlinks(parent); err != nil || now != real {
		return errors.New("the folder changed")
	}
	if !replace {
		return os.Link(tmp.Name(), u.abs) // fails when the name is taken
	}
	if fi, err := os.Lstat(u.abs); err == nil && !fi.Mode().IsRegular() {
		return errors.New("something that is not a file has this name now")
	}
	return os.Rename(tmp.Name(), u.abs)
}

// webUploadSave moves a complete upload in under a name that is free.
func (m *Model) webUploadSave(call *webCall) tea.Cmd {
	id := call.args.(*webproto.UploadArgs).ID
	u := m.webUploadReady(call, id)
	if u == nil {
		return nil
	}
	if _, err := os.Lstat(u.abs); err == nil {
		call.fail(webproto.CodeConflict, "A file has this name. Replace it?")
		return nil
	}
	parent, real, err := m.webUploadTarget(u)
	if err != nil {
		call.fail(webproto.CodeConflict, "The folder changed. Start again.")
		return nil
	}
	call.later()
	return func() tea.Msg {
		err := webUploadPut(u, parent, real, false)
		return applyMsg(func(m *Model) tea.Cmd {
			if err != nil {
				m.webAuditOp(call, u.rel, "failed")
				if errors.Is(err, fs.ErrExist) {
					call.fail(webproto.CodeConflict, "A file has this name. Replace it?")
					return nil
				}
				call.fail(webproto.CodeInternal, "The file could not be saved. "+fileOpFailText(err))
				return nil
			}
			m.web.ups.drop(id)
			m.webAuditOp(call, u.rel+" "+strconv.FormatInt(u.size, 10)+" bytes", "saved")
			m.explorer.Reload()
			call.reply(webproto.TreeChanged{Path: u.rel})
			return tea.Batch(m.webTreeRefresh(), fetchGitCmd())
		})
	}
}

// webUploadReplace asks the laptop before an upload replaces a file.
func (m *Model) webUploadReplace(call *webCall) *webAsk {
	id := call.args.(*webproto.UploadArgs).ID
	u := m.webUploadReady(call, id)
	if u == nil {
		return nil
	}
	old := "a file"
	if fi, err := os.Stat(u.abs); err == nil {
		old = "the file (" + webSizeText(fi.Size()) + ")"
	}
	return &webAsk{
		summary: u.rel + " " + strconv.FormatInt(u.size, 10) + " bytes",
		title:   "Replace file?",
		body:    "Replace " + old + " " + u.rel + " with the upload (" + webSizeText(u.size) + ")? The old content is lost.",
		action:  "Replace",
		run: func(m *Model) tea.Cmd {
			parent, real, err := m.webUploadTarget(u)
			if err != nil {
				m.web.ups.drop(id)
				return m.pushToastDetail(toast.Errr, "Upload not saved", "The folder changed.")
			}
			return func() tea.Msg {
				err := webUploadPut(u, parent, real, true)
				return applyMsg(func(m *Model) tea.Cmd {
					if m.web != nil {
						m.web.ups.drop(id)
					}
					if err != nil {
						return m.pushToastDetail(toast.Errr, "Upload not saved", fileOpFailText(err))
					}
					if m.nvim != nil {
						_ = m.nvim.Command("silent! checktime")
					}
					m.explorer.Reload()
					return tea.Batch(m.pushToastDetail(toast.Info, "Replaced "+filepath.Base(u.abs), webSizeText(u.size)+" from the web"),
						m.webTreeRefresh(), fetchGitCmd())
				})
			}
		},
	}
}

// webUploadCancel drops a staged upload.
func (m *Model) webUploadCancel(call *webCall) tea.Cmd {
	id := call.args.(*webproto.UploadArgs).ID
	if m.web.ups.get(id, call.conn.id) != nil {
		m.web.ups.drop(id)
	}
	call.reply(nil)
	return nil
}

// webSizeText is a byte count for a card ("1.2 MB").
func webSizeText(n int64) string {
	switch {
	case n < 1<<10:
		return strconv.FormatInt(n, 10) + " B"
	case n < 1<<20:
		return strconv.FormatFloat(float64(n)/(1<<10), 'f', 1, 64) + " KB"
	}
	return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MB"
}
