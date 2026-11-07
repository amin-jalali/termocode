package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// API-15: HTTP: Import Collection… reads a v2.1 collection JSON, a HAR
// or an Insomnia export and writes a new .http file next to it (never
// over an existing one); HTTP: Export as HAR… writes the open .http
// file as a HAR 1.2 log (secrets redacted, never over a file).

const (
	promptKindHTTPImportCollection promptKindEnum = 4620
	promptKindHTTPExportHAR        promptKindEnum = 4621
)

// httpCollectionMaxBytes caps the file an import reads.
const httpCollectionMaxBytes = 32 << 20

// httpImportCollectionFile imports src into a new .http file in its
// folder and returns that file and the request count.
func httpImportCollectionFile(src string) (string, int, error) {
	data, err := readFileLimit(src, httpCollectionMaxBytes)
	if err != nil {
		return "", 0, err
	}
	im, err := httpclient.ImportCollection(data)
	if err != nil {
		return "", 0, err
	}
	out, err := httpWriteNew(filepath.Dir(src), httpclient.ImportedFileName(src), []byte(im.HTTP()))
	return out, len(im.Requests), err
}

// httpWriteNew writes data as name in dir, or name-2, name-3 … when the
// name is taken; it never overwrites.
func httpWriteNew(dir, name string, data []byte) (string, error) {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		n := name
		if i > 1 {
			n = fmt.Sprintf("%s-%d%s", base, i, ext)
		}
		p := filepath.Join(dir, n)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(data)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(p)
			return "", werr
		}
		return p, nil
	}
	return "", fmt.Errorf("too many files named %s", name)
}

// httpExportHARFile writes the requests of the .http file src as a HAR
// to dst (which must not exist yet) and returns the request count.
func httpExportHARFile(src, dst string) (int, error) {
	data, err := readFileLimit(src, httpCollectionMaxBytes)
	if err != nil {
		return 0, err
	}
	f := httpclient.Parse(string(data))
	if len(f.Requests) == 0 {
		return 0, errors.New("the file has no requests")
	}
	har, err := httpclient.ExportHAR(f.Requests, time.Now())
	if err != nil {
		return 0, err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return 0, fmt.Errorf("%s already exists; pick another name", filepath.Base(dst))
	}
	if err != nil {
		return 0, err
	}
	_, werr := out.Write(har)
	if cerr := out.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(dst)
		return 0, werr
	}
	return len(f.Requests), nil
}

// httpExportHARPrompt asks where to write the open .http file as HAR.
func (m *Model) httpExportHARPrompt() tea.Cmd {
	f := m.httpCurrentFile()
	if f == "" {
		return m.pushToastDetail(toast.Info, "Open a .http file first", "HTTP: Export as HAR… writes its requests as a HAR file")
	}
	m.httpS().importInto = f
	return m.openHTTPPrompt(promptKindHTTPExportHAR, "HTTP: Export as HAR", "HAR file:", httpclient.HARFileName(f))
}

// httpCollectionPrompt handles the two prompts. ok=false: not ours.
func (m *Model) httpCollectionPrompt(value string) (tea.Cmd, bool) {
	value = strings.TrimSpace(value)
	abs := func(p string) string {
		if p != "" && !filepath.IsAbs(p) {
			return filepath.Join(aiWorkspaceRoot(), p)
		}
		return p
	}
	switch m.promptKind {
	case promptKindHTTPImportCollection:
		if value == "" {
			return nil, true
		}
		out, n, err := httpImportCollectionFile(abs(value))
		if err != nil {
			return m.pushToastDetail(toast.Warn, "Import Collection: "+err.Error(), "a collection JSON (v2.1), a HAR or an Insomnia export"), true
		}
		m.explorer.Reload()
		return tea.Batch(m.httpOpenInEditor(out), m.pushToastDetail(toast.Info, fmt.Sprintf("Imported %d requests into %s", n, filepath.Base(out)), "secret header values became {{placeholders}}")), true
	case promptKindHTTPExportHAR:
		src := m.httpS().importInto
		if value == "" || src == "" {
			return nil, true
		}
		dst := abs(value)
		n, err := httpExportHARFile(src, dst)
		if err != nil {
			return m.pushToastDetail(toast.Warn, "Export as HAR: "+err.Error(), ""), true
		}
		m.explorer.Reload()
		return m.pushToastDetail(toast.Info, fmt.Sprintf("Exported %d requests to %s", n, filepath.Base(dst)), "from the saved file; secret values are redacted"), true
	}
	return nil, false
}
