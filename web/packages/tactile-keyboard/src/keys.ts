// DOM KeyboardEvent names for the keyboard's keys, so a host can treat a
// Tactile key like a hardware key (key + code + modifiers).

const NAMED: Record<string, [key: string, code: string]> = {
  enter: ["Enter", "Enter"],
  backspace: ["Backspace", "Backspace"],
  tab: ["Tab", "Tab"],
  esc: ["Escape", "Escape"],
  space: [" ", "Space"],
  left: ["ArrowLeft", "ArrowLeft"],
  right: ["ArrowRight", "ArrowRight"],
  up: ["ArrowUp", "ArrowUp"],
  down: ["ArrowDown", "ArrowDown"],
  home: ["Home", "Home"],
  end: ["End", "End"],
  insert: ["Insert", "Insert"],
  delete: ["Delete", "Delete"],
  pageUp: ["PageUp", "PageUp"],
  pageDown: ["PageDown", "PageDown"],
};

const CHAR_CODE: Record<string, string> = {
  "`": "Backquote",
  "-": "Minus",
  "=": "Equal",
  "[": "BracketLeft",
  "]": "BracketRight",
  "\\": "Backslash",
  ";": "Semicolon",
  "'": "Quote",
  ",": "Comma",
  ".": "Period",
  "/": "Slash",
};

/** namedKey maps a key id (enter, f5, pageUp…) to its DOM key and code. */
export function namedKey(id: string): [key: string, code: string] {
  const n = NAMED[id];
  if (n) return n;
  const f = /^[fF](\d+)$/.exec(id);
  if (f) return [`F${f[1]}`, `F${f[1]}`];
  return [id, id];
}

/** charCode is the DOM code of a symbol or letter key by its main glyph. */
export function charCode(main: string): string {
  if (/^[A-Za-z]$/.test(main)) return `Key${main.toUpperCase()}`;
  if (/^\d$/.test(main)) return `Digit${main}`;
  return CHAR_CODE[main] ?? main;
}
