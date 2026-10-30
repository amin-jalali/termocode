// Hover text for the desktop editor's frame. The hub sends a hover as a
// markdown AST (webproto.MdNode: a closed list of kinds, no raw HTML, no
// link targets but http and https). The editor's hover widget takes
// markdown, so the frame writes the AST back as markdown in which every
// piece of text is escaped: what the widget sees has only the structure
// the AST has, and the widget runs with HTML and trusted commands off.
// Links become their text: nothing in the frame navigates. Pure.
import type { MdNode } from "../../../../webproto/proto";

/** esc escapes every character markdown gives a meaning to. */
export function esc(s: string): string {
  return s.replace(/[\\`*_{}[\]()#+\-.!|<>~&$:]/g, (c) => "\\" + c).replace(/\n/g, " ");
}

/** fence is a run of backticks longer than any run inside s. */
function fence(s: string, min: number): string {
  let longest = 0;
  for (const m of s.match(/`+/g) ?? []) longest = Math.max(longest, m.length);
  return "`".repeat(Math.max(min, longest + 1));
}

const LANG = /^[A-Za-z0-9_+#.-]{1,32}$/;
const BLOCKS = new Set(["doc", "p", "h", "codeblock", "ul", "ol", "li", "blockquote", "hr", "table"]);

function inline(cs: readonly MdNode[] | undefined): string {
  return (cs ?? []).map(md).join("");
}

function blocks(cs: readonly MdNode[] | undefined): string {
  const out: string[] = [];
  let run = "";
  for (const c of cs ?? []) {
    if (BLOCKS.has(c.k)) {
      if (run) out.push(run);
      run = "";
      out.push(md(c));
    } else run += md(c);
  }
  if (run) out.push(run);
  return out.join("\n\n");
}

function indent(s: string, by: string): string {
  return s.split("\n").map((l, i) => (i === 0 || l === "" ? l : by + l)).join("\n");
}

function cell(n: MdNode): string {
  return inline(n.c).replace(/\n/g, " ");
}

/** md writes one node (and its children) as markdown. */
export function md(n: MdNode): string {
  switch (n.k) {
    case "doc":
    case "li":
      return blocks(n.c);
    case "p":
      return inline(n.c);
    case "h":
      return "#".repeat(Math.min(6, Math.max(1, n.level ?? 1))) + " " + inline(n.c);
    case "em":
      return "*" + inline(n.c) + "*";
    case "strong":
      return "**" + inline(n.c) + "**";
    case "code": {
      const t = (n.text ?? "").replace(/\n/g, " ");
      const f = fence(t, 1);
      const pad = t.startsWith("`") || t.endsWith("`") ? " " : "";
      return f + pad + t + pad + f;
    }
    case "codeblock": {
      const t = (n.text ?? "").replace(/\n$/, "");
      const f = fence(t, 3);
      const lang = n.lang && LANG.test(n.lang) ? n.lang : "";
      return f + lang + "\n" + t + "\n" + f;
    }
    case "ul":
      return (n.c ?? []).map((li) => "- " + indent(md(li), "  ")).join("\n");
    case "ol": {
      const start = n.start ?? 1;
      return (n.c ?? []).map((li, i) => `${start + i}. ` + indent(md(li), "    ")).join("\n");
    }
    case "blockquote":
      return blocks(n.c).split("\n").map((l) => "> " + l).join("\n");
    case "link":
      return inline(n.c);
    case "hr":
      return "---";
    case "br":
      return "  \n";
    case "table": {
      const rows = n.rows ?? [];
      if (rows.length === 0) return "";
      const w = Math.max(...rows.map((r) => r.length));
      const line = (r: readonly MdNode[]) => "| " + Array.from({ length: w }, (_, i) => (r[i] ? cell(r[i]) : "")).join(" | ") + " |";
      return [line(rows[0]), "|" + " --- |".repeat(w), ...rows.slice(1).map(line)].join("\n");
    }
    case "th":
    case "td":
      return cell(n);
    default:
      return esc(n.text ?? "");
  }
}
