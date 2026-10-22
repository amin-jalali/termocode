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

	// JetBrains plugin ids
	"org.jetbrains.plugins.go": {Tools: []string{"gopls", "dlv"}},
	"PythonCore":               {Tools: []string{"pyright", "debugpy"}},
	"Pythonid":                 {Tools: []string{"pyright", "debugpy"}},
	"org.rust.lang":            {Tools: []string{"rust_analyzer"}},
	"org.toml.lang":            {Tools: []string{"taplo"}},
	"com.jetbrains.sh":         {Tools: []string{"bashls"}},
	"com.tang":                 {Tools: []string{"lua_ls"}},
	"com.jetbrains.restClient": {Note: "built-in HTTP client"},
	"Docker":                   {Note: "Services view"},
	"com.github.copilot":       {Note: "AI inline completion; set a provider"},
	"com.intellij.ml.llm":      {Note: "AI inline completion; set a provider"},
	"IdeaVIM":                  {Note: "load your vimrc"},

	// Sublime Text packages (Package Control names)
	"LSP-gopls":         {Tools: []string{"gopls"}},
	"LSP-pyright":       {Tools: []string{"pyright"}},
	"LSP-rust-analyzer": {Tools: []string{"rust_analyzer"}},
	"LSP-clangd":        {Tools: []string{"clangd"}},
	"LSP-lua":           {Tools: []string{"lua_ls"}},
	"LSP-taplo":         {Tools: []string{"taplo"}},
	"LSP-bash":          {Tools: []string{"bashls"}},
	"LSP-typescript":    {Tools: []string{"ts_ls"}},
	"LSP":               {Note: "language servers are built in; the matching LSP-* packages give the tools"},
	"GitGutter":         {Note: "Source Control view"},
	"GitSavvy":          {Note: "Source Control view"},
	"Terminus":          {Note: "built-in terminal"},
	"EditorConfig":      {Note: "built in"},
	"Markdown Preview":  {Note: "built-in Markdown preview"},
	"SublimeLinter":     {Note: "problems come from language servers"},
	"Vintageous":        {Note: "load your vimrc"},
	"NeoVintageous":     {Note: "load your vimrc"},
	"GitHub Copilot":    {Note: "AI inline completion; set a provider"},
	"Codeium":           {Note: "AI inline completion; set a provider"},
}
