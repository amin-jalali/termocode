// Typing smarts for code on a touch keyboard: auto-closing pairs,
// indentation on newline and small keyword snippets. Pure functions; the
// host applies the results to its own editor.

const PAIRS: Record<string, string> = { "(": ")", "[": "]", "{": "}", '"': '"', "'": "'", "`": "`" };

export interface PairInsert {
  /** Text to insert (empty when skip). */
  text: string;
  /** Insert: move the caret left by this much (inside the pair). Skip:
   * move it right by this much instead of inserting. */
  caretBackBy: number;
  skip: boolean;
}

/** autoPair: what to insert for ch given the character after the caret,
 * or null to insert ch as it is. */
export function autoPair(ch: string, charAfterCaret: string): PairInsert | null {
  if (ch in PAIRS) {
    if ((ch === '"' || ch === "'" || ch === "`") && charAfterCaret === ch) return { text: "", caretBackBy: 1, skip: true };
    return { text: ch + PAIRS[ch], caretBackBy: 1, skip: false };
  }
  if ((ch === ")" || ch === "]" || ch === "}") && charAfterCaret === ch) return { text: "", caretBackBy: 1, skip: true };
  return null;
}

/** newlineIndent: the whitespace after a newline: the line's indent, one
 * level (2 spaces) deeper after { : ( [. */
export function newlineIndent(textBeforeCaret: string): string {
  const line = textBeforeCaret.slice(textBeforeCaret.lastIndexOf("\n") + 1);
  const indent = /^[ \t]*/.exec(line)?.[0] ?? "";
  const t = line.trimEnd();
  const deeper = t.endsWith("{") || t.endsWith(":") || t.endsWith("(") || t.endsWith("[");
  return deeper ? indent + "  " : indent;
}

export interface SnippetExpansion {
  text: string;
  caretOffset: number;
}

const MARK = "\u0000";

/** expandSnippet: the skeleton for a leading keyword in a language, with
 * the caret's place, or null. */
export function expandSnippet(word: string, languageId: string | null | undefined): SnippetExpansion | null {
  const lang = languageId ?? "";
  switch (word) {
    case "if":
      return mk(`if (${MARK}) {\n  \n}`);
    case "for":
      return lang === "python" ? mk(`for ${MARK} in :\n    `) : mk(`for (${MARK}) {\n  \n}`);
    case "while":
      return lang === "python" ? mk(`while ${MARK}:\n    `) : mk(`while (${MARK}) {\n  \n}`);
    case "func":
    case "fn":
      return lang === "go" ? mk(`func ${MARK}() {\n  \n}`) : mk(`function ${MARK}() {\n  \n}`);
    case "def":
      return lang === "python" ? mk(`def ${MARK}():\n    `) : null;
    case "class":
      return lang === "python" ? mk(`class ${MARK}:\n    `) : mk(`class ${MARK} {\n  \n}`);
    default:
      return null;
  }
}

function mk(t: string): SnippetExpansion {
  return { text: t.replace(MARK, ""), caretOffset: t.indexOf(MARK) };
}
