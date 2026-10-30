// Markdown from the hub, drawn from its AST (web design 2.2): each node
// kind becomes a Preact element and each string a text node, so nothing
// in a message can become markup. Links go through safeHref and a confirm
// sheet; code blocks stay left to right and isolated with a copy button.
import { Fragment, h, type ComponentChildren } from "preact";
import { useState } from "preact/hooks";
import type { MdNode } from "../../../../webproto/proto";
import { askLink } from "../lib/links";
import { toast } from "../lib/toasts";
import { safeHref } from "../state/chat";
import { t } from "../strings";

/** MAX_DEPTH stops runaway nesting; deeper nodes show their text only. */
const MAX_DEPTH = 40;
/** CODE_ROWS is how many rows of a code block show before "Show more". */
export const CODE_ROWS = 200;

/** textOf is all the text under a node, for nodes drawn as plain text. */
export function textOf(n: MdNode): string {
  if (!n || typeof n !== "object") return "";
  if (typeof n.text === "string" && !n.c) return n.text;
  return (n.c ?? []).map(textOf).join("");
}

export function CodeBlock({ text, lang }: { text: string; lang?: string }) {
  const [all, setAll] = useState(false);
  const lines = text.replace(/\n$/, "").split("\n");
  const cut = !all && lines.length > CODE_ROWS;
  const copy = () => {
    const done = () => toast("ok", t("copied"));
    const fail = () => toast("warn", t("copy.failed"));
    try {
      navigator.clipboard.writeText(text).then(done, fail);
    } catch {
      fail();
    }
  };
  return (
    <div class="codeblock" dir="ltr">
      <div class="cb-head">
        <span class="cb-lang">{lang}</span>
        <button type="button" class="cb-copy" onClick={copy}>
          {t("copy")}
        </button>
      </div>
      <pre class="cb-pre">
        <code>{cut ? lines.slice(0, CODE_ROWS).join("\n") : lines.join("\n")}</code>
      </pre>
      {cut && (
        <button type="button" class="cb-more" onClick={() => setAll(true)}>
          {t("chat.more_code")}
        </button>
      )}
    </div>
  );
}

function kids(n: MdNode, d: number): ComponentChildren[] {
  return (n.c ?? []).map((c, i) => node(c, d + 1, i));
}

function node(n: MdNode, d: number, key: number): ComponentChildren {
  if (!n || typeof n !== "object") return null;
  if (d > MAX_DEPTH) return textOf(n);
  switch (n.k) {
    case "doc":
      return <Fragment key={key}>{kids(n, d)}</Fragment>;
    case "p":
      return (
        <p key={key} dir="auto">
          {kids(n, d)}
        </p>
      );
    case "h": {
      const lv = Math.min(6, Math.max(1, Math.floor(n.level ?? 1)));
      return (
        <div key={key} class={`md-h md-h${lv}`} role="heading" aria-level={Math.min(6, lv + 2)} dir="auto">
          {kids(n, d)}
        </div>
      );
    }
    case "em":
      return <em key={key}>{kids(n, d)}</em>;
    case "strong":
      return <strong key={key}>{kids(n, d)}</strong>;
    case "code":
      return (
        <code key={key} class="md-code" dir="ltr">
          {n.text ?? ""}
        </code>
      );
    case "codeblock":
      return <CodeBlock key={key} text={n.text ?? ""} lang={n.lang} />;
    case "ul":
      return <ul key={key}>{kids(n, d)}</ul>;
    case "ol":
      return (
        <ol key={key} start={n.start && n.start !== 1 ? n.start : undefined}>
          {kids(n, d)}
        </ol>
      );
    case "li":
      return (
        <li key={key} dir="auto">
          {kids(n, d)}
        </li>
      );
    case "blockquote":
      return (
        <blockquote key={key} dir="auto">
          {kids(n, d)}
        </blockquote>
      );
    case "link": {
      const href = safeHref(n.href);
      if (!href) return <Fragment key={key}>{kids(n, d)}</Fragment>;
      return (
        <a
          key={key}
          class="md-link"
          href={href}
          target="_blank"
          rel="noopener noreferrer"
          onClick={(e: MouseEvent) => {
            e.preventDefault();
            askLink(href);
          }}
        >
          {kids(n, d)}
        </a>
      );
    }
    case "hr":
      return <hr key={key} />;
    case "br":
      return <br key={key} />;
    case "table":
      return (
        <div key={key} class="md-table">
          <table>
            <tbody>
              {(n.rows ?? []).map((row, ri) => (
                <tr key={ri}>{(Array.isArray(row) ? row : []).map((cell, ci) => node(cell, d + 1, ci))}</tr>
              ))}
            </tbody>
          </table>
        </div>
      );
    case "th":
      return (
        <th key={key} dir="auto">
          {kids(n, d)}
        </th>
      );
    case "td":
      return (
        <td key={key} dir="auto">
          {kids(n, d)}
        </td>
      );
    case "text":
      return n.text ?? "";
    default:
      return textOf(n);
  }
}

/** Md draws one markdown AST. */
export function Md({ doc }: { doc: MdNode }) {
  return <div class="md">{node(doc, 0, 0)}</div>;
}
