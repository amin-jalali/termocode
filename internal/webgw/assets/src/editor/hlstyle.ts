// The syntax colour classes inside the editor's shadow root: the same
// classes and tokens as styles/hl.css for the read view (hlstyle_test.go
// keeps the two the same).
export const HL_STYLES: Record<string, Record<string, string>> = {
  ".hl-k": { color: "var(--ai)" },
  ".hl-s": { color: "var(--green)" },
  ".hl-e": { color: "var(--amber)" },
  ".hl-c": { color: "var(--muted)", fontStyle: "italic" },
  ".hl-n": { color: "var(--amber)" },
  ".hl-f": { color: "var(--blue)" },
  ".hl-t": { color: "var(--git-conf)" },
  ".hl-g": { color: "var(--red)" },
  ".hl-o": { color: "var(--text-2)" },
  ".hl-h": { color: "var(--blue)", fontWeight: "700" },
  ".hl-b": { fontWeight: "700" },
  ".hl-i": { fontStyle: "italic" },
  ".hl-x": { textDecoration: "line-through" },
  ".hl-u": { color: "var(--blue)", textDecoration: "underline" },
  ".hl-r": { color: "var(--red)", textDecoration: "underline wavy" },
  ".hl-da": { color: "var(--git-add)" },
  ".hl-dd": { color: "var(--git-del)" },
  ".hl-dm": { color: "var(--git-mod)" },
};
