package app

// snippetsLua defines a small per-filetype snippet table and rebinds Tab so
// that when the word immediately before the cursor matches a known trigger
// the trigger is replaced with the snippet body and `vim.snippet.expand`
// drives any `${1:name}` / `$0` placeholders.
//
// Loaded after the existing Tab keymap in lsp_lua.go, so this version wins.
// The new Tab order of preference is:
//
//   1. Snippet expansion if the word before the cursor matches.
//   2. Accept popup completion if pumvisible().
//   3. Jump to next placeholder if a snippet session is active.
//   4. Plain <Tab>.
//
// Snippet bodies use LSP / VSCode syntax — `${N:placeholder}` for tab-stop
// positions, `$0` for the final cursor position. nvim ≥ 0.10's
// `vim.snippet.expand()` interprets that natively, so no custom parser.
const snippetsLua = `
-- Per-filetype snippet table. Keep these short and focused on the truly
-- common patterns that hurt to type repeatedly. Anything domain-specific
-- belongs in user config (which we don't have a UI for yet).
local _snippets = {
  go = {
    iferr   = 'if err != nil {\n\treturn ${1:err}\n}\n$0',
    iferrf  = 'if err != nil {\n\treturn fmt.Errorf("${1:context}: %w", err)\n}\n$0',
    funcm   = 'func main() {\n\t$0\n}',
    fori    = 'for i := 0; i < ${1:n}; i++ {\n\t$0\n}',
    forr    = 'for ${1:i}, ${2:v} := range ${3:slice} {\n\t$0\n}',
    pkg     = 'package ${1:main}\n\n$0',
    test    = 'func Test${1:Name}(t *testing.T) {\n\t$0\n}',
    bench   = 'func Benchmark${1:Name}(b *testing.B) {\n\tfor i := 0; i < b.N; i++ {\n\t\t$0\n\t}\n}',
    lg      = 'log.Printf("${1:%v}", ${2:value})$0',
    doc     = '// ${1:Name} ${0}',
  },
  python = {
    defm    = 'def __init__(self${1:, args}):\n\t$0',
    deff    = 'def ${1:name}(${2:args}):\n\t"""${3:docstring}"""\n\t$0',
    cls     = 'class ${1:Name}:\n\tdef __init__(self${2:, args}):\n\t\t$0',
    main    = 'if __name__ == "__main__":\n\t$0',
    forr    = 'for ${1:i} in ${2:iterable}:\n\t$0',
    pr      = 'print(${1:value})$0',
    try     = 'try:\n\t${1:pass}\nexcept ${2:Exception} as ${3:e}:\n\t$0',
  },
  javascript = {
    cl      = 'console.log(${1:value})$0',
    fn      = 'function ${1:name}(${2:args}) {\n\t$0\n}',
    afn     = 'const ${1:name} = (${2:args}) => {\n\t$0\n}',
    forr    = 'for (const ${1:item} of ${2:items}) {\n\t$0\n}',
    fori    = 'for (let i = 0; i < ${1:n}; i++) {\n\t$0\n}',
    iml     = 'import ${1:name} from \'${2:module}\'$0',
    tryc    = 'try {\n\t${1}\n} catch (${2:err}) {\n\t$0\n}',
  },
  typescript = {
    cl      = 'console.log(${1:value})$0',
    fn      = 'function ${1:name}(${2:args}): ${3:void} {\n\t$0\n}',
    afn     = 'const ${1:name} = (${2:args}): ${3:void} => {\n\t$0\n}',
    iface   = 'interface ${1:Name} {\n\t$0\n}',
    type    = 'type ${1:Name} = ${0}',
    forr    = 'for (const ${1:item} of ${2:items}) {\n\t$0\n}',
    iml     = 'import { ${1:name} } from \'${2:module}\'$0',
  },
  rust = {
    fnm     = 'fn main() {\n\t$0\n}',
    fnn     = 'fn ${1:name}(${2:args}) -> ${3:()} {\n\t$0\n}',
    forr    = 'for ${1:item} in ${2:iter} {\n\t$0\n}',
    matchc  = 'match ${1:expr} {\n\t${2:Pattern} => ${3:value},\n\t_ => $0,\n}',
    impl    = 'impl ${1:Type} {\n\t$0\n}',
    test    = '#[test]\nfn ${1:name}() {\n\t$0\n}',
    pl      = 'println!("${1:%v}", ${2:value})$0',
  },
  lua = {
    fn      = 'function ${1:name}(${2:args})\n\t$0\nend',
    forr    = 'for ${1:i}, ${2:v} in ipairs(${3:t}) do\n\t$0\nend',
    forp    = 'for ${1:k}, ${2:v} in pairs(${3:t}) do\n\t$0\nend',
    iff     = 'if ${1:cond} then\n\t$0\nend',
  },
  markdown = {
    code    = '` + "`" + `` + "`" + `` + "`" + `${1:lang}\n${2:content}\n` + "`" + `` + "`" + `` + "`" + `\n$0',
    link    = '[${1:text}](${2:url})$0',
    img     = '![${1:alt}](${2:url})$0',
  },
}

-- Look up the trigger word immediately before the cursor (word characters
-- only; we want 'iferr' to match but '.iferr' should not). Returns the
-- trigger and its body, or nil if no match.
local function _find_snippet()
  local line = vim.api.nvim_get_current_line()
  local col = vim.api.nvim_win_get_cursor(0)[2]
  if col == 0 then return nil end
  -- Walk back from the cursor over [%w_] characters.
  local i = col
  while i > 0 and line:sub(i, i):match('[%w_]') do
    i = i - 1
  end
  local trigger = line:sub(i + 1, col)
  if trigger == '' then return nil end
  local ft = vim.bo.filetype
  -- Map related filetypes to their canonical entry. JSX/TSX share JS/TS.
  local alias = {
    javascriptreact = 'javascript',
    typescriptreact = 'typescript',
    py = 'python',
  }
  ft = alias[ft] or ft
  local table_ = _snippets[ft]
  if not table_ then return nil end
  local body = table_[trigger]
  if not body then return nil end
  return trigger, body, i + 1, col
end

-- Replace the existing <Tab> keymap from lsp_lua.go with a smarter version
-- that handles snippet expansion FIRST, then completion popup, then snippet
-- placeholder navigation, then a plain <Tab>. We override (silent = true,
-- expr = false) so we can use feedkeys / api directly inside the callback.
-- _expand_plain strips the snippet placeholder syntax from body and
-- returns it as plain lines plus a (row, col) offset of the $0 cursor
-- marker (or end of body if absent). Used on nvim less than 0.10 where
-- vim.snippet.expand isn't available.
local function _expand_plain(body)
  -- ${N:default} → default
  body = body:gsub('%${(%d+):([^}]*)}', '%2')
  -- $N → ""
  body = body:gsub('%$%d+', '')
  -- Find $0 (final cursor position) and strip it.
  local zero_idx = body:find('%$0')
  if zero_idx then
    body = body:sub(1, zero_idx - 1) .. body:sub(zero_idx + 2)
  end
  local lines = vim.split(body, '\n', { plain = true })
  -- Compute (row, col) of cursor by counting newlines up to zero_idx.
  local row, col = #lines - 1, #lines[#lines]
  if zero_idx then
    local before = body:sub(1, zero_idx - 1)
    local nl_count = 0
    for _ in before:gmatch('\n') do nl_count = nl_count + 1 end
    row = nl_count
    local last_nl = before:match('.*\n()')
    if last_nl then
      col = zero_idx - last_nl
    else
      col = zero_idx - 1
    end
  end
  return lines, row, col
end

vim.keymap.set('i', '<Tab>', function()
  -- 1. Try snippet expansion first.
  local trigger, body, start_col, end_col = _find_snippet()
  if trigger and body then
    local row = vim.api.nvim_win_get_cursor(0)[1] - 1
    vim.api.nvim_buf_set_text(0, row, start_col - 1, row, end_col, { '' })
    if vim.snippet and vim.snippet.expand then
      -- nvim 0.10+: native expansion with placeholder navigation.
      vim.snippet.expand(body)
      return
    end
    -- nvim < 0.10 fallback: strip the placeholder syntax and insert
    -- the cleaned body as plain text. No tab-stop navigation, but
    -- the user gets the boilerplate without typing it.
    local lines, off_row, off_col = _expand_plain(body)
    vim.api.nvim_buf_set_text(0, row, start_col - 1, row, start_col - 1, lines)
    -- Position cursor at $0 (or end of body).
    local cur_row = row + off_row
    local cur_col = off_col
    if off_row == 0 then
      cur_col = start_col - 1 + off_col
    end
    pcall(vim.api.nvim_win_set_cursor, 0, { cur_row + 1, cur_col })
    return
  end
  -- 2. Accept popup completion if visible.
  if vim.fn.pumvisible() == 1 then
    vim.api.nvim_feedkeys(vim.api.nvim_replace_termcodes('<C-y>', true, false, true), 'n', false)
    return
  end
  -- 3. Jump to next placeholder if a snippet session is in progress.
  if vim.snippet and vim.snippet.active and vim.snippet.active({ direction = 1 }) then
    vim.snippet.jump(1)
    return
  end
  -- 4. Plain Tab.
  vim.api.nvim_feedkeys(vim.api.nvim_replace_termcodes('<Tab>', true, false, true), 'n', false)
end, { silent = true })

-- Shift+Tab also jumps backwards through placeholders when a session is
-- active; otherwise falls back to the popup-prev / plain S-Tab path.
vim.keymap.set('i', '<S-Tab>', function()
  if vim.fn.pumvisible() == 1 then
    vim.api.nvim_feedkeys(vim.api.nvim_replace_termcodes('<C-p>', true, false, true), 'n', false)
    return
  end
  if vim.snippet and vim.snippet.active and vim.snippet.active({ direction = -1 }) then
    vim.snippet.jump(-1)
    return
  end
  vim.api.nvim_feedkeys(vim.api.nvim_replace_termcodes('<S-Tab>', true, false, true), 'n', false)
end, { silent = true })
`
