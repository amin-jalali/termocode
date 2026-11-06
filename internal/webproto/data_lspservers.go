package webproto

// The language server installer from the web (WEBP-2; the laptop's LSP:
// Manage Language Servers…). The page lists the laptop's language
// servers and their state; it never sees a laptop path. Installing,
// reinstalling and uninstalling run on the laptop, only after a laptop
// card that says what runs.

// Op names.
const (
	OpLSPServers   = "lsp.servers"
	OpLSPServerAsk = "lsp.server_ask"
)

// MaxLSPServerName caps a language server's name.
const MaxLSPServerName = 64

// LSPServerAskArgs names a language server and what to do with it.
type LSPServerAskArgs struct {
	Name   string `json:"name"`
	Action string `json:"action" enum:"install|uninstall"`
}

func (a *LSPServerAskArgs) validate(int) error {
	if a.Name == "" || len(a.Name) > MaxLSPServerName || a.Name[0] == '-' {
		return invalid("bad language server name")
	}
	for _, r := range a.Name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return invalid("bad language server name")
		}
	}
	if a.Action != "install" && a.Action != "uninstall" {
		return invalid("bad action")
	}
	return nil
}

// LSPServer is one of the laptop's language servers.
type LSPServer struct {
	Name  string   `json:"name"`
	Title string   `json:"title"` // "gopls (Go)"
	Langs []string `json:"langs"` // the file types it serves
	// State: missing, managed (installed by termocode), system (on the
	// laptop's PATH) or installing (now, in the background).
	State    string `json:"state" enum:"missing|managed|system|installing"`
	Version  string `json:"version,omitempty"`  // a managed copy's version
	Progress string `json:"progress,omitempty"` // the last line of a running install
	// Problem says why it cannot be installed on the laptop, or that the
	// last install failed.
	Problem string `json:"problem,omitempty"`
	Used    bool   `json:"used,omitempty"` // it serves a language of the workspace
}

// LSPServerList answers lsp.servers: the workspace's languages first,
// then the laptop's list order.
type LSPServerList struct {
	Items []LSPServer `json:"items"`
}

var opLSPServerRows = []Policy{
	{Op: OpLSPServers, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpLSPServerAsk, New: newOf[LSPServerAskArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opLSPServerRows...); return true }()

func init() {
	registerResult(OpLSPServers, LSPServerList{})
	registerResult(OpLSPServerAsk, AskRef{})
}
