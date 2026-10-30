// The PROBLEMS panel of the desktop workbench: the laptop's diagnostics
// (the diagnostics topic) grouped by file, worst file first, as the
// laptop's own Problems panel lists them. A row opens its file at the
// place; a file's head folds its rows.
import { h } from "preact";
import { useState } from "preact/hooks";
import type { Diag } from "../../../../webproto/proto";
import { diags, goTo } from "../lib/lsp";
import { t, type StringKey } from "../strings";
import { Cod, FileIcon } from "../ui/icons";

const SEV_ICON: Readonly<Record<number, string>> = { 1: "error", 2: "warning", 3: "info", 4: "lightbulb" };
const SEV_NAME: Readonly<Record<number, StringKey>> = { 1: "prob.sev1", 2: "prob.sev2", 3: "prob.sev3", 4: "prob.sev4" };

/** SevIcon is a diagnostic's severity as the workbench draws it. */
export function SevIcon({ sev }: { sev: number }) {
  return (
    <span class={`sev sev-${sev}`} role="img" aria-label={t(SEV_NAME[sev] ?? "prob.sev1")} title={t(SEV_NAME[sev] ?? "prob.sev1")}>
      <Cod name={SEV_ICON[sev] ?? "error"} />
    </span>
  );
}

function split(path: string): { name: string; dir: string } {
  const i = path.lastIndexOf("/");
  return i < 0 ? { name: path, dir: "" } : { name: path.slice(i + 1), dir: path.slice(0, i) };
}

function Row({ path, d }: { path: string; d: Diag }) {
  const first = d.message.split("\n")[0];
  return (
    <button type="button" class="prob-row" title={d.message} onClick={() => goTo(path, d.line, d.col)}>
      <SevIcon sev={d.sev} />
      <span class="prob-msg" dir="auto">
        {first}
      </span>
      {(d.source || d.code) && (
        <span class="prob-src" dir="ltr">
          {d.source}
          {d.code ? `(${d.code})` : ""}
        </span>
      )}
      <span class="prob-at" dir="ltr">
        [{t("sb.pos", { n: d.line + 1, c: d.col + 1 })}]
      </span>
    </button>
  );
}

export function ProblemsView() {
  const s = diags();
  const [folded, setFolded] = useState<ReadonlySet<string>>(new Set());
  if (!s || s.files.length === 0) {
    return (
      <div class="problems problems-empty" role="status">
        {t("prob.none")}
      </div>
    );
  }
  const fold = (path: string) => {
    const n = new Set(folded);
    if (n.has(path)) n.delete(path);
    else n.add(path);
    setFolded(n);
  };
  return (
    <div class="problems" role="tree" aria-label={t("wb.problems")}>
      {s.files.map((f) => {
        const { name, dir } = split(f.path);
        const open = !folded.has(f.path);
        return (
          <div class="prob-file" key={f.path} role="treeitem" aria-expanded={open ? "true" : "false"}>
            <button type="button" class="prob-head" onClick={() => fold(f.path)}>
              <Cod name={open ? "chevron-down" : "chevron-right"} />
              <FileIcon path={f.path} />
              <span class="prob-name" dir="auto">
                {name}
              </span>
              {dir && (
                <span class="prob-dir" dir="auto">
                  {dir}
                </span>
              )}
              <span class="prob-count">{f.items.length}</span>
            </button>
            {open && (
              <div role="group">
                {f.items.map((d, i) => (
                  <Row key={i} path={f.path} d={d} />
                ))}
              </div>
            )}
          </div>
        );
      })}
      {(s.more ?? 0) > 0 && <p class="prob-more">{t("prob.more", { n: s.more ?? 0 })}</p>}
    </div>
  );
}
