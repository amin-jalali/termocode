package app

import (
	"strings"
	"testing"
)

// CTX-13: the LSP setup starts a server with file name patterns only on
// matching files, passes its settings, and lints Dockerfiles with
// hadolint when it is installed.
func TestLSPSetupFilesSettingsAndHadolint(t *testing.T) {
	for _, want := range []string{
		"cfg.files",
		"vim.json.decode(srv.cfg.settings)",
		"'hadolint'",
		"vim.diagnostic.set(",
		"TermocodeHadolint",
	} {
		if !strings.Contains(lspSetupLua, want) {
			t.Errorf("lspSetupLua has no %s", want)
		}
	}
}
