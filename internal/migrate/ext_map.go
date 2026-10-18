package migrate

// ExtensionResult is the equivalent tools and explanation for a source add-on.
type ExtensionResult struct {
	Tools []string
	Note  string
}

// ExtensionMap maps well-known extension identifiers to registered tools or notes.
var ExtensionMap = map[string]ExtensionResult{
	"golang.go":                             {Tools: []string{"gopls", "dlv"}},
	"ms-python.python":                      {Tools: []string{"pyright", "debugpy"}},
	"ms-python.vscode-pylance":              {Tools: []string{"pyright"}},
	"rust-lang.rust-analyzer":               {Tools: []string{"rust_analyzer"}},
	"llvm-vs-code-extensions.vscode-clangd": {Tools: []string{"clangd"}},
	"sumneko.lua":                           {Tools: []string{"lua_ls"}},
	"tamasfe.even-better-toml":              {Tools: []string{"taplo"}},
	"mads-hartmann.bash-ide-vscode":         {Tools: []string{"bashls"}},
	"dbaeumer.vscode-eslint":                {Tools: []string{"ts_ls"}, Note: "format through the language server"},
	"ms-vscode.js-debug":                    {Tools: []string{"js-debug"}},
	"humao.rest-client":                     {Note: "built-in HTTP client"},
	"ms-azuretools.vscode-docker":           {Note: "Services view"},
	"eamodio.gitlens":                       {Note: "Source Control view"},
	"GitHub.copilot":                        {Note: "AI inline completion; set a provider"},
	"Continue.continue":                     {Note: "AI inline completion; set a provider"},
	"Codeium.codeium":                       {Note: "AI inline completion; set a provider"},
	"vscodevim.vim":                         {Note: "load your vimrc"},
	"EditorConfig.EditorConfig":             {Note: "built in"},
}
