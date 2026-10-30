package app

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// fakeWebLSPLua starts an in-process language server on buffer b. It
// answers hover, completion, definition and references from fixed data
// and writes the position of every request it gets into _G.web_lsp_seen
// (method, line, character). enc is its position encoding; silent: it
// never answers the four requests.
const fakeWebLSPLua = `
local b, root, other, enc, silent = ...
_G.web_lsp_seen = {}
local function pos(l, c) return { line = l, character = c } end
local function loc(f, l, c0, c1) return { uri = vim.uri_from_fname(f), range = { start = pos(l, c0), ['end'] = pos(l, c1) } } end
local function server(dispatchers)
  local closing = false
  return {
    request = function(method, params, callback)
      local res = nil
      if method == 'initialize' then
        res = { capabilities = { positionEncoding = enc, textDocumentSync = 1, hoverProvider = true,
          completionProvider = { triggerCharacters = { '.' } }, definitionProvider = true, referencesProvider = true } }
      elseif method:sub(1, 13) == 'textDocument/' then
        table.insert(_G.web_lsp_seen, method .. ' ' .. params.position.line .. ':' .. params.position.character)
        if silent then return true, 2 end
        local p = params.position
        if method == 'textDocument/hover' then
          res = { contents = { kind = 'markdown', value = '` + "```" + `go\nfunc bad() int\n` + "```" + `\n\nDoes **bad** things. <script>x()</script>' } }
        elseif method == 'textDocument/completion' then
          res = { isIncomplete = true, items = {
            { label = 'Println', kind = 3, detail = 'func(a ...any)', insertTextFormat = 2, sortText = '0001',
              textEdit = { range = { start = pos(p.line, p.character - 2), ['end'] = pos(p.line, p.character) }, newText = 'Println(${1:a})' },
              additionalTextEdits = { { range = { start = pos(0, 0), ['end'] = pos(0, 0) }, newText = 'import "fmt"\n' } } },
            { label = 'Printf', kind = 99, insertText = 'Printf' },
          } }
        elseif method == 'textDocument/definition' then
          res = { loc(other, 2, 5, 8) }
        elseif method == 'textDocument/references' then
          res = { loc(other, 2, 5, 8), loc(vim.api.nvim_buf_get_name(b), 1, p.character, p.character + 3),
            loc('/elsewhere/lib.go', 4, 0, 3), loc(root .. '/.env', 0, 0, 3) }
        end
      end
      vim.schedule(function() callback(nil, res) end)
      return true, 1
    end,
    notify = function() return true end,
    is_closing = function() return closing end,
    terminate = function() closing = true end,
  }
end
local id = vim.lsp.start({ name = 'fake-' .. enc, cmd = server, root_dir = root }, { bufnr = b })
vim.wait(2000, function() return #vim.lsp.get_clients({ bufnr = b }) > 0 end)
return id`

// webLspSeen returns (and forgets) the requests the fake server got.
func webLspSeen(r *webFilesRig) string {
	var s string
	r.lua(`local out = table.concat(_G.web_lsp_seen, '\n'); _G.web_lsp_seen = {}; return out`, &s)
	return s
}

// callLsp runs an lsp op and hands nvim's notifications to the Model
// (as Update does) until it is answered.
func (r *webFilesRig) callLsp(op string, args *webproto.LspPosArgs) map[string]any {
	r.t.Helper()
	r.id++
	c := r.c
	c.inflight.Add(1)
	call := &webCall{ctx: c.ctx, conn: c, id: r.id, op: op, args: args}
	cmd, found := r.m.webRunCall(r.h, call)
	if !found {
		r.t.Fatalf("no handler for %s", op)
	}
	done := make(chan tea.Msg, 1)
	go func() {
		if cmd == nil {
			done <- nil
			return
		}
		done <- cmd()
	}()
	r.pumpUntil(op+" answer", call.answered.Load)
	select {
	case msg := <-done:
		if f, ok := msg.(applyMsg); ok {
			f(r.m)
		}
	case <-time.After(20 * time.Second):
		r.t.Fatal("the wait did not end")
	}
	for _, f := range r.takeOn(c) {
		if id, ok := f["id"].(float64); ok && uint64(id) == r.id {
			return f
		}
		r.keep(c, f)
	}
	r.t.Fatalf("%s gave no answer", op)
	return nil
}

// webLspRig opens a.go (a row with a Persian word and an emoji before
// "bad") and other.go, and starts the fake server on a.go.
func webLspRig(t *testing.T, enc string, silent bool) (*webFilesRig, webproto.BufOpenRes, string) {
	r := newWebFilesRig(t)
	r.withNvim()
	line := `	x := "سلام 😀"; fmt.Pr`
	r.write("a.go", "package a\n"+line+"\n")
	other := r.write("other.go", "package a\n\nfunc bad() int { return 1 }\n")
	b := webBufOpen(t, r, r.c, "a.go")
	var id any
	r.lua(fakeWebLSPLua, &id, b.Buf, r.root, other, enc, silent)
	if id == nil {
		t.Fatal("the fake server did not start")
	}
	return r, b, line
}

// Spec (desktop hover): lsp.hover asks the laptop's language server at
// the page's UTF-16 column (the server gets it in its own encoding) and
// answers the markdown as an AST: the code block keeps its language,
// raw HTML never becomes a node.
func TestLspHoverAsksTheLaptopsServer(t *testing.T) {
	for _, enc := range []string{"utf-16", "utf-8"} {
		t.Run(enc, func(t *testing.T) {
			r, b, line := webLspRig(t, enc, false)
			at := strings.Index(line, "fmt")
			var res webproto.LspHover
			webResData(t, r.callLsp(webproto.OpLspHover, &webproto.LspPosArgs{Buf: b.Buf, Tick: b.Tick, Line: 1, Col: u16(line[:at])}), &res)
			want := u16(line[:at])
			if enc == "utf-8" {
				want = at
			}
			if seen := webLspSeen(r); seen != "textDocument/hover 1:"+strconv.Itoa(want) {
				t.Errorf("server got %q, want column %d", seen, want)
			}
			if res.None || res.MD == nil || len(res.MD.C) < 2 {
				t.Fatalf("hover %+v", res)
			}
			if c := res.MD.C[0]; c.K != webproto.MdCodeBlock || c.Lang != "go" || !strings.Contains(c.Text, "func bad() int") {
				t.Errorf("code block %+v", c)
			}
			if s := webMdTextOf(*res.MD); strings.Contains(s, "<script>") {
				t.Errorf("raw HTML went out: %q", s)
			}
		})
	}
}

// webMdTextOf joins every text of an AST.
func webMdTextOf(n webproto.MdNode) string {
	s := n.Text
	for _, c := range n.C {
		s += webMdTextOf(c)
	}
	return s
}

// Spec (desktop completion): lsp.complete answers the server's items:
// snippet syntax kept, the replaced range and the extra import edit in
// UTF-16 columns, kinds outside the protocol's list dropped.
func TestLspCompleteItems(t *testing.T) {
	r, b, line := webLspRig(t, "utf-16", false)
	end := u16(line)
	var res webproto.LspCompletion
	webResData(t, r.callLsp(webproto.OpLspComplete, &webproto.LspPosArgs{Buf: b.Buf, Tick: b.Tick, Line: 1, Col: end}), &res)
	if !res.Incomplete || len(res.Items) != 2 {
		t.Fatalf("completion %+v", res)
	}
	p := res.Items[0]
	if p.Label != "Println" || p.Kind != 3 || !p.Snippet || p.Insert != "Println(${1:a})" || p.From != end-2 || p.To != end || p.Sort != "0001" {
		t.Errorf("Println %+v (end %d)", p, end)
	}
	if len(p.Edits) != 1 || p.Edits[0] != (webproto.LspEdit{Text: "import \"fmt\"\n"}) {
		t.Errorf("edits %+v", p.Edits)
	}
	if q := res.Items[1]; q.Kind != 0 || q.Insert != "Printf" || q.From != -1 || q.To != -1 {
		t.Errorf("Printf %+v", q)
	}
}

// Spec (desktop go to definition / find references): places come back
// workspace-relative with their row's text; a place outside the
// workspace has only its file's name; a secret file has no text.
// References are sorted by path and position.
func TestLspDefinitionAndReferences(t *testing.T) {
	r, b, line := webLspRig(t, "utf-16", false)
	at := u16(line[:strings.Index(line, "fmt")])
	var defs webproto.LspLocations
	webResData(t, r.callLsp(webproto.OpLspDefinition, &webproto.LspPosArgs{Buf: b.Buf, Tick: b.Tick, Line: 1, Col: at}), &defs)
	if len(defs.Locs) != 1 {
		t.Fatalf("definition %+v", defs)
	}
	if d := defs.Locs[0]; d.Path != "other.go" || d.Line != 2 || d.Col != 5 || d.EndCol != 8 || d.Text != "func bad() int { return 1 }" {
		t.Errorf("definition %+v", d)
	}
	r.write(".env", "TOKEN=x\n") // its text never goes out
	var refs webproto.LspLocations
	webResData(t, r.callLsp(webproto.OpLspReferences, &webproto.LspPosArgs{Buf: b.Buf, Tick: b.Tick, Line: 1, Col: at}), &refs)
	var got []string
	for _, l := range refs.Locs {
		got = append(got, l.Path+"|"+l.Name+"|"+strconv.Itoa(l.Line)+":"+strconv.Itoa(l.Col)+"-"+strconv.Itoa(l.EndCol)+"|"+l.Text)
	}
	want := []string{
		"|lib.go|4:0-0|",
		".env||0:0-3|",
		"a.go||1:" + strconv.Itoa(at) + "-" + strconv.Itoa(at+3) + "|" + line,
		"other.go||2:5-8|func bad() int { return 1 }",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("references:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !refs.Locs[0].Outside {
		t.Errorf("outside place %+v", refs.Locs[0])
	}
}

// Spec: the ops read the buffer at the page's tick: a buffer that moved
// on is a conflict that carries the new tick; a buffer the page did not
// open is not_found; a file with no server says so; a server that does
// not answer ends the call as busy, and its late answer is dropped.
func TestLspGuards(t *testing.T) {
	r, b, _ := webLspRig(t, "utf-16", true)
	r.lua(`vim.api.nvim_buf_set_lines(...,0,1,false,{'package b'})`, nil, b.Buf)
	f := r.callLsp(webproto.OpLspHover, &webproto.LspPosArgs{Buf: b.Buf, Tick: b.Tick, Line: 0, Col: 0})
	if webErrCode(f) != "conflict" {
		t.Fatalf("stale tick: %v", f)
	}
	var tick int
	r.lua(`return vim.api.nvim_buf_get_changedtick(...)`, &tick, b.Buf)
	if d, _ := f["data"].(map[string]any); d == nil || int(d["tick"].(float64)) != tick {
		t.Errorf("conflict data %v, tick %d", f["data"], tick)
	}
	if f := r.callLsp(webproto.OpLspHover, &webproto.LspPosArgs{Buf: 999, Tick: 1}); webErrCode(f) != "not_found" {
		t.Errorf("unopened buffer: %v", f)
	}
	old := webLspWait
	webLspWait = 50 * time.Millisecond
	t.Cleanup(func() { webLspWait = old })
	f = r.callLsp(webproto.OpLspDefinition, &webproto.LspPosArgs{Buf: b.Buf, Tick: tick, Line: 0, Col: 0})
	if webErrCode(f) != "busy" || len(r.m.web.lsp.waiting) != 0 {
		t.Errorf("silent server: %v (waiting %d)", f, len(r.m.web.lsp.waiting))
	}
	r.m.webLspDone(r.m.web.lsp.next, `{"locs":[]}`) // a late answer: nothing happens
	r.write("n.txt", "plain\n")
	n := webBufOpen(t, r, r.c, "n.txt")
	if f := r.callLsp(webproto.OpLspHover, &webproto.LspPosArgs{Buf: n.Buf, Tick: n.Tick}); webErrCode(f) != "not_found" || !strings.Contains(f["msg"].(string), "No language server") {
		t.Errorf("no server: %v", f)
	}
}

// Spec: with the real gopls (skipped when it is not installed), hover
// and go to definition come from it through nvim's own client.
func TestLspWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	r := newWebFilesRig(t)
	r.withNvim()
	r.write("go.mod", "module example.com/a\n\ngo 1.22\n")
	src := "package main\n\nimport \"fmt\"\n\nfunc greet() string { return \"hi\" }\n\nfunc main() { fmt.Println(greet()) }\n"
	r.write("main.go", src)
	b := webBufOpen(t, r, r.c, "main.go")
	var ok bool
	r.lua(`local b, root, cache = ...
vim.lsp.start({ name = 'gopls', cmd = { 'gopls' }, root_dir = root, cmd_env = { XDG_CACHE_HOME = cache } }, { bufnr = b })
return vim.wait(15000, function()
  for _, c in ipairs(vim.lsp.get_clients({ bufnr = b })) do
    if c.initialized then return true end
  end
  return false
end, 50)`, &ok, b.Buf, r.root, t.TempDir())
	if !ok {
		t.Fatal("gopls did not start")
	}
	t.Cleanup(func() {
		var done bool
		r.lua(`for _, c in ipairs(vim.lsp.get_clients()) do c:stop(true) end
return vim.wait(5000, function() return #vim.lsp.get_clients() == 0 end, 50)`, &done)
	})
	line := 6
	row := strings.Split(src, "\n")[line]
	at := func(s string) int { return u16(row[:strings.Index(row, s)]) + 1 }
	// The first answers wait for gopls to load the package: ask again.
	var hover webproto.LspHover
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); {
		f := r.callLsp(webproto.OpLspHover, &webproto.LspPosArgs{Buf: b.Buf, Tick: b.Tick, Line: line, Col: at("Println")})
		if webErrCode(f) == "" {
			webResData(t, f, &hover)
			if !hover.None {
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if hover.MD == nil || !strings.Contains(webMdTextOf(*hover.MD), "Println") {
		t.Fatalf("hover %+v", hover)
	}
	var defs webproto.LspLocations
	webResData(t, r.callLsp(webproto.OpLspDefinition, &webproto.LspPosArgs{Buf: b.Buf, Tick: b.Tick, Line: line, Col: at("greet")}), &defs)
	if len(defs.Locs) != 1 || defs.Locs[0].Path != "main.go" || defs.Locs[0].Line != 4 || defs.Locs[0].Col != 5 {
		t.Errorf("definition %+v", defs)
	}
}
