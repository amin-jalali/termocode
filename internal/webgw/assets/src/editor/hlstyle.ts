// The syntax colour classes inside the editor's shadow root: the same
// classes and tokens as styles/hl.css for the read view (hlstyle_test.go
// keeps the two the same).
export const HL_STYLES: Record<string, Record<string, string>> = {
  ".hl-k": { color: "var(--syn-kw)" },
  ".hl-kc": { color: "var(--syn-ctl)" },
  ".hl-s": { color: "var(--syn-str)" },
  ".hl-e": { color: "var(--amber)" },
  ".hl-rx": { color: "var(--syn-rx)" },
  ".hl-c": { color: "var(--syn-cmt)", fontStyle: "italic" },
  ".hl-n": { color: "var(--syn-num)" },
  ".hl-cn": { color: "var(--syn-cst)" },
  ".hl-f": { color: "var(--syn-fn)" },
  ".hl-t": { color: "var(--syn-ty)" },
  ".hl-v": { color: "var(--syn-var)" },
  ".hl-g": { color: "var(--syn-kw)" },
  ".hl-o": { color: "var(--syn-op)" },
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
