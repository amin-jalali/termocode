package lspinstall

import (
	"strconv"
	"strings"
)

// luaQuote returns s as a single-quoted Lua string literal.
func luaQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`)
	return "'" + r.Replace(s) + "'"
}

func luaList(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = luaQuote(x)
	}
	return "{ " + strings.Join(q, ", ") + " }"
}

// LuaServersTable renders the LSP part of the registry as the Lua table
// lsp_lua.go's FileType autocmd reads:
//
//	{ gopls = { cmd = {...}, filetypes = {...}, root = {...}, order = 1 }, ... }
//
// `order` keeps the registry's preference order (Lua `pairs` is unordered).
func LuaServersTable() string {
	var b strings.Builder
	b.WriteString("{\n")
	order := 0
	for _, t := range ByCategory(CategoryLSP) {
		if len(t.Cmd) == 0 {
			continue
		}
		order++
		b.WriteString("  [")
		b.WriteString(luaQuote(t.Name))
		b.WriteString("] = { cmd = ")
		b.WriteString(luaList(t.Cmd))
		b.WriteString(", filetypes = ")
		b.WriteString(luaList(t.Filetypes))
		b.WriteString(", root = ")
		b.WriteString(luaList(t.Root))
		b.WriteString(", order = ")
		b.WriteString(strconv.Itoa(order))
		b.WriteString(" },\n")
	}
	b.WriteString("}")
	return b.String()
}

// LuaQuote is exported for callers that inject paths into Lua chunks.
func LuaQuote(s string) string { return luaQuote(s) }
