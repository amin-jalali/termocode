package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// HTTP: Start / Stop Mock Server (API-22). The open .http file (its
// `# @mock-*` directives) or OpenAPI file is served on a random
// 127.0.0.1 port until Stop, a new Start, or quit. Local only, so no
// card.

// httpSourceFile is the active buffer when it is an .http file or an
// OpenAPI / Swagger file. openapi tells which; raw is the buffer text.
func (m *Model) httpSourceFile() (file, raw string, openapi bool) {
	if f := m.httpCurrentFile(); f != "" {
		raw, _ = m.httpReadRaw(f)
		return f, raw, false
	}
	p := m.editor.Path()
	switch strings.ToLower(filepath.Ext(p)) {
	case ".yaml", ".yml", ".json":
	default:
		return "", "", false
	}
	raw, ok := m.httpReadRaw(p)
	if !ok || !httpclient.LooksOpenAPI(p, []byte(raw)) {
		return "", "", false
	}
	return p, raw, true
}

func (m *Model) httpStartMock() tea.Cmd {
	file, raw, openapi := m.httpSourceFile()
	if file == "" {
		return m.pushToast(toast.Info, "Open an .http or OpenAPI file first")
	}
	var mock *httpclient.Mock
	var err error
	if openapi {
		mock, err = httpclient.NewMockOpenAPI([]byte(raw))
	} else {
		mock, err = httpclient.NewMock(httpclient.Parse(raw))
	}
	name := filepath.Base(file)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Mock server: "+err.Error(), name)
	}
	m.httpStopMock()
	ctx, cancel := context.WithCancel(context.Background())
	addr, err := mock.Start(ctx)
	if err != nil {
		cancel()
		return m.pushToastDetail(toast.Errr, "Mock server: could not listen", err.Error())
	}
	st := m.httpS()
	st.mockAddr, st.mockFile, st.mockCancel, st.mockSrv = addr, file, cancel, mock
	routes := mock.Routes()
	var lines []string
	for i, r := range routes {
		if i == 8 {
			lines = append(lines, fmt.Sprintf("… %d more", len(routes)-i))
			break
		}
		lines = append(lines, fmt.Sprintf("%s %s → %d", r.Method, r.Path, r.Status))
	}
	return m.pushToastDetail(toast.Info, fmt.Sprintf("Mock server for %s on http://%s (%d routes)", name, addr, len(routes)),
		strings.Join(lines, "\n")+"\nHTTP: Stop Mock Server ends it.")
}

func (m *Model) httpStopMockCmd() tea.Cmd {
	if m.http == nil || m.http.mockAddr == "" {
		return m.pushToast(toast.Info, "No mock server is running")
	}
	addr, name := m.http.mockAddr, filepath.Base(m.http.mockFile)
	m.httpStopMock()
	return m.pushToast(toast.Info, "Mock server for "+name+" on "+addr+" stopped")
}

// httpStopMock ends the mock server, if any, and waits until it closed.
func (m *Model) httpStopMock() {
	st := m.http
	if st == nil || st.mockCancel == nil {
		return
	}
	st.mockCancel()
	if st.mockSrv != nil {
		st.mockSrv.Wait()
	}
	st.mockAddr, st.mockFile, st.mockCancel, st.mockSrv = "", "", nil, nil
}
