package lspinstall

import (
	"encoding/json"
	"strings"
	"testing"
)

// CTX-13: Dockerfile, compose and Kubernetes YAML servers.
func TestContainerLanguageServers(t *testing.T) {
	for _, c := range []struct{ name, ft, bin string }{
		{"dockerls", "dockerfile", "docker-langserver"},
		{"docker_compose_language_service", "yaml", "docker-compose-langserver"},
		{"yamlls", "yaml", "yaml-language-server"},
	} {
		tl, ok := Lookup(c.name)
		if !ok {
			t.Errorf("missing %s", c.name)
			continue
		}
		if tl.Category != CategoryLSP || tl.InstallKind != KindNpm || tl.Cmd[0] != c.bin || tl.Cmd[len(tl.Cmd)-1] != "--stdio" {
			t.Errorf("%s = %+v", c.name, tl)
		}
		found := false
		for _, ft := range tl.Filetypes {
			found = found || ft == c.ft
		}
		if !found {
			t.Errorf("%s: no filetype %s", c.name, c.ft)
		}
	}
	// The compose server only starts on compose files, not on every YAML.
	cs, _ := Lookup("docker_compose_language_service")
	if len(cs.Files) == 0 {
		t.Error("compose server has no file name patterns")
	}
	// yamlls knows the Kubernetes schema.
	y, _ := Lookup("yamlls")
	if !json.Valid([]byte(y.Settings)) || !strings.Contains(y.Settings, `"kubernetes"`) {
		t.Errorf("yamlls settings = %q", y.Settings)
	}
}

func TestLuaServersTableFilesAndSettings(t *testing.T) {
	s := LuaServersTable()
	for _, want := range []string{
		`['dockerls'] = { cmd = { 'docker-langserver', '--stdio' }, filetypes = { 'dockerfile' }`,
		`files = { '^compose%.ya?ml$'`,
		`['yamlls'] = { cmd = { 'yaml-language-server', '--stdio' }`,
		`settings = '{"yaml":`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	// Servers without patterns or settings keep the old line.
	if strings.Contains(s, `['gopls'] = { cmd = { 'gopls' }, filetypes = { 'go', 'gomod', 'gowork', 'gotmpl' }, root = { 'go.mod', 'go.work', '.git' }, order = 1 },`) == false {
		t.Errorf("gopls line changed:\n%s", s)
	}
}
