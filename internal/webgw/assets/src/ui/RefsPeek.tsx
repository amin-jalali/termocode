// The references list of the desktop editor (Shift+F12, or "Find All
// References" in the editor's menu): every place the laptop's language
// server found, grouped by file, over the lower part of the editor like
// a peek. A row opens its file at the place and the list stays; Esc or
// the close button ends it.
import { h } from "preact";
import { useEffect, useRef } from "preact/hooks";
import type { LspLoc } from "../../../../webproto/proto";
import { goTo, refs } from "../lib/lsp";
import { t } from "../strings";
import { Cod, FileIcon } from "./icons";

interface Group {
  path: string;
  outside: boolean;
  locs: LspLoc[];
}

function groups(locs: readonly LspLoc[]): Group[] {
  const out: Group[] = [];
  for (const l of locs) {
    const key = l.outside ? "" : l.path ?? "";
    let g = out.find((x) => x.path === key && x.outside === !!l.outside);
    if (!g) {
      g = { path: key, outside: !!l.outside, locs: [] };
      out.push(g);
    }
    g.locs.push(l);
  }
  return out;
}

/** Text is a row with the place marked (UTF-16 columns). */
function Text({ l }: { l: LspLoc }) {
  const s = l.text ?? "";
  const lead = s.length - s.trimStart().length;
  const a = Math.max(lead, Math.min(l.col, s.length));
  const b = l.endLine === l.line ? Math.max(a, Math.min(l.endCol, s.length)) : a;
  return (
    <span class="refs-text" dir="ltr">
      {s.slice(lead, a)}
      {b > a && <mark>{s.slice(a, b)}</mark>}
      {s.slice(b)}
    </span>
  );
}

export function RefsPeek() {
  const r = refs.value;
  const box = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (r?.state === "done") box.current?.querySelector<HTMLElement>(".refs-row")?.focus();
  }, [r?.state]);
  if (!r) return null;
  const close = () => (refs.value = null);
  const list = groups(r.locs);
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Escape") {
      e.preventDefault();
      close();
      return;
    }
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    const rows = [...(box.current?.querySelectorAll<HTMLElement>(".refs-row:not([disabled])") ?? [])];
    const i = rows.indexOf(document.activeElement as HTMLElement);
    const next = rows[Math.min(rows.length - 1, Math.max(0, i + (e.key === "ArrowDown" ? 1 : -1)))];
    if (next) {
      e.preventDefault();
      next.focus();
    }
  };
  return (
    <section class="refs-peek" ref={box} aria-label={t("refs.title", { word: r.word })} onKeyDown={onKey}>
      <header class="refs-head">
        <Cod name="references" />
        <span class="refs-title" dir="auto">
          {t("refs.title", { word: r.word })}
        </span>
        {r.state === "done" && <span class="refs-count">{t("refs.count", { n: r.locs.length })}</span>}
        <button type="button" class="tb tb-icon refs-close" aria-label={t("refs.close")} title={`${t("refs.close")} (Esc)`} onClick={close}>
          <Cod name="close" />
        </button>
      </header>
      <div class="refs-body">
        {r.state === "loading" && (
          <p class="refs-note" role="status">
            {t("refs.loading")}
          </p>
        )}
        {r.state === "error" && (
          <p class="refs-note" role="alert" dir="auto">
            {r.note || t("refs.none")}
          </p>
        )}
        {r.state === "done" && r.locs.length === 0 && (
          <p class="refs-note" role="status">
            {t("refs.none")}
          </p>
        )}
        {list.map((g) => (
          <div class="refs-file" key={(g.outside ? "-" : "+") + g.path}>
            <div class="refs-file-head">
              {g.outside ? <Cod name="link-external" /> : <FileIcon path={g.path} />}
              <span class="refs-path" dir="auto">
                {g.outside ? t("refs.outside") : g.path}
              </span>
              <span class="refs-n">{g.locs.length}</span>
            </div>
            {g.locs.map((l, i) => (
              <button
                type="button"
                key={i}
                class="refs-row"
                disabled={g.outside}
                onClick={() => l.path && goTo(l.path, l.line, l.col)}
              >
                <span class="refs-ln" dir="ltr">
                  {g.outside ? `${l.name ?? ""}:${l.line + 1}` : l.line + 1}
                </span>
                {!g.outside && <Text l={l} />}
              </button>
            ))}
          </div>
        ))}
        {r.cut && <p class="refs-note">{t("refs.cut")}</p>}
      </div>
    </section>
  );
}
