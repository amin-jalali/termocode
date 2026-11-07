package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// HTTP: Open API Docs shows the open .http file as a read-only
// Markdown page; secrets stay hidden.
func TestHTTPAPIDocsOpensPage(t *testing.T) {
	m, root, _ := covSvcHTTP(t)
	if _, ok := m.dispatchHTTPPalette("http-api-docs"); !ok {
		t.Fatal("not handled without a file")
	}
	if !strings.Contains(covSvcToast(m), "Open an .http or OpenAPI file") {
		t.Fatalf("toast = %q", covSvcToast(m))
	}
	file := filepath.Join(root, "api.http")
	m.httpOpenInEditor(file)
	m.editor.SetMeta(file, false, "http")
	m.dispatchHTTPPalette("http-api-docs")
	var name, text, ft string
	if err := m.nvim.ExecLuaResult(`return vim.api.nvim_buf_get_name(0)`, &name); err != nil {
		t.Fatal(err)
	}
	_ = m.nvim.ExecLuaResult(`return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), '\n')`, &text)
	_ = m.nvim.ExecLuaResult(`return vim.bo.filetype`, &ft)
	if !strings.HasSuffix(name, "API docs: api.http") || ft != "markdown" {
		t.Fatalf("buffer %q ft %q", name, ft)
	}
	if !strings.Contains(text, "## users") || !strings.Contains(text, "## missing") {
		t.Fatalf("page:\n%s", text)
	}
}

// API-23: HTTP: Open API Docs also works on an open OpenAPI file.
func TestHTTPAPIDocsFromOpenAPIFile(t *testing.T) {
	m, root, _ := covSvcHTTP(t)
	file := filepath.Join(root, "pets.yaml")
	src := "openapi: 3.0.0\ninfo: {title: Pets}\npaths:\n  /pets:\n    get:\n      operationId: listPets\n      responses: {\"200\": {description: ok}}\n"
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	m.httpOpenInEditor(file)
	m.editor.SetMeta(file, false, "yaml")
	m.dispatchHTTPPalette("http-api-docs")
	var name, text string
	if err := m.nvim.ExecLuaResult(`return vim.api.nvim_buf_get_name(0)`, &name); err != nil {
		t.Fatal(err)
	}
	_ = m.nvim.ExecLuaResult(`return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), '\n')`, &text)
	if !strings.HasSuffix(name, "API docs: pets.yaml") || !strings.Contains(text, "## listPets") {
		t.Fatalf("buffer %q:\n%s", name, text)
	}
}
