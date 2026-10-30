// The commit graph (desktop editor tab "@git:graph"): every branch's
// commits in a table with a drawn graph, as a git graph view has it —
// Graph, Description (branch and tag labels first), Date, Author,
// Commit. A click opens the commit's details under its row (its message
// and changed files; a file opens its change side by side). The context
// menu copies, checks out a branch or starts an interactive rebase.
// The hub lays out the lanes (git.graph); this only draws them.
import { Fragment, h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { GitGraphRow, GitRef } from "../../../../../webproto/proto";
import { copyText } from "../../lib/clip";
import { diffTab } from "../../lib/difftab";
import { rebaseTab } from "../../lib/gittab";
import { showFile } from "../../lib/workbench";
import { link, live, store } from "../../session";
import { readDiff, readGit, splitPath, type DiffPage } from "../../state/git";
import { t, tn } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { gitErrText as errText } from "./errors";
import { onMenu, type MenuItems } from "../../ui/ContextMenu";
import { Cod } from "../../ui/icons";
import { View } from "../View";
import { colorClass, edgePath, lanes, laneX, readGraph, ROW } from "./graph";

const dateFmt = new Intl.DateTimeFormat(undefined, { day: "numeric", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" });

/** GraphCell draws one row's lines and node. */
function GraphCell({ r, width }: { r: GitGraphRow; width: number }) {
  const mid = ROW / 2;
  return (
    <svg class="gg-svg" width={width} height={ROW} viewBox={`0 0 ${width} ${ROW}`} aria-hidden="true" focusable="false">
      {r.up.map((e, i) => (
        <path key={"u" + i} class={`gg-line ${colorClass(e.c)}`} d={edgePath(e, 0, mid)} />
      ))}
      {r.down.map((e, i) => (
        <path key={"d" + i} class={`gg-line ${colorClass(e.c)}`} d={edgePath(e, mid, ROW)} />
      ))}
      <circle class={`gg-node ${colorClass(r.color)}${r.head ? " is-head" : ""}`} cx={laneX(r.col)} cy={mid} r={r.head ? 4.5 : 4} />
    </svg>
  );
}

/** PassLines carry the lanes below an open row on through its details. */
function PassLines({ r, width }: { r: GitGraphRow; width: number }) {
  return (
    <svg class="gg-svg gg-pass-svg" width={width} height="100%" aria-hidden="true" focusable="false">
      {r.down.map((e, i) => (
        <line key={i} class={`gg-line ${colorClass(e.c)}`} x1={laneX(e.t)} x2={laneX(e.t)} y1="0" y2="100%" />
      ))}
    </svg>
  );
}

/** RefChip is one branch or tag label. */
function RefChip({ x, color, head }: { x: GitRef; color: number; head: boolean }) {
  const icon = x.kind === "tag" ? "tag" : x.kind === "remote" ? "cloud" : "git-branch";
  return (
    <span class={`gg-ref gg-ref-${x.kind} ${colorClass(color)}${head ? " is-head" : ""}`} title={x.name}>
      <Cod name={icon} />
      <span class="gg-ref-name" dir="ltr">
        {x.name}
      </span>
    </span>
  );
}

/** Details is the commit under its row: message, parents, files. */
function Details({ r }: { r: GitGraphRow }) {
  const [page, setPage] = useState<DiffPage | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    let stop = false;
    link.request("git.show", { hash: r.hash }).then(
      (x) => !stop && setPage(readDiff(x)),
      (e: unknown) => !stop && setErr(errText(e)),
    );
    return () => {
      stop = true;
    };
  }, [r.hash]);
  // Each file row starts a file; its added and removed rows follow.
  const files: { path: string; add: number; del: number }[] = [];
  for (const x of page?.rows ?? []) {
    if (x.k === "file") files.push({ path: x.text, add: 0, del: 0 });
    else if (files.length && x.k === "add") files[files.length - 1].add++;
    else if (files.length && x.k === "del") files[files.length - 1].del++;
  }
  const c = page?.commit;
  return (
    <div class="gg-details" role="region" aria-label={t("gg.details", { hash: r.hash })}>
      <div class="gg-info">
        <p class="gg-info-line">
          <span class="gg-k">{t("git.commit_do")}</span>
          <span class="gg-mono" dir="ltr">
            {r.hash}
          </span>
        </p>
        {r.parents.length > 0 && (
          <p class="gg-info-line">
            <span class="gg-k">{t("gg.parents")}</span>
            <span class="gg-mono" dir="ltr">
              {r.parents.join(", ")}
            </span>
          </p>
        )}
        <p class="gg-info-line">
          <span class="gg-k">{t("gg.col_author")}</span>
          <span dir="auto">{r.author}</span>
        </p>
        <p class="gg-info-line">
          <span class="gg-k">{t("gg.col_date")}</span>
          <span>{dateFmt.format(r.when)}</span>
        </p>
        <p class="gg-msg" dir="auto">
          {c ? c.subject + (c.body ? "\n\n" + c.body : "") : r.subject}
        </p>
      </div>
      <div class="gg-files" aria-label={t("gg.files_label")}>
        {err ? (
          <p class="empty refused">{err}</p>
        ) : !page ? (
          <p class="gg-wait">{t("loading")}</p>
        ) : (
          <>
            <p class="gg-files-head">{tn("gg.files", files.length)}</p>
            {files.map((f) => {
              const { name, dir } = splitPath(f.path);
              return (
                <button type="button" class="gg-file" key={f.path} title={f.path} onClick={() => showFile(diffTab("commit", f.path, r.hash), true)}>
                  <span class="gg-file-name" dir="auto">
                    {name}
                  </span>
                  {dir && (
                    <span class="gg-file-dir" dir="auto">
                      {dir}
                    </span>
                  )}
                  <span class="gg-file-n" dir="ltr">
                    {f.add > 0 && <span class="gg-add">+{f.add}</span>}
                    {f.del > 0 && <span class="gg-del">−{f.del}</span>}
                  </span>
                </button>
              );
            })}
            {(page.next > 0 || page.truncated) && <p class="gg-wait">{t("gg.files_more")}</p>}
          </>
        )}
      </div>
    </div>
  );
}

export function GraphView() {
  const git = store.topic("git").value;
  const g = readGit(git?.data);
  const [head, setHead] = useState(false);
  const [rows, setRows] = useState<GitGraphRow[]>([]);
  const [more, setMore] = useState(false);
  const [busy, setBusy] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [err, setErr] = useState("");
  const [open, setOpen] = useState<string | null>(null);
  const [checkout, setCheckout] = useState<string | null>(null);
  const body = useRef<HTMLTableSectionElement | null>(null);
  const viewer = link.meta.value?.role === "viewer";
  // A new commit or branch shows at once: the graph is read again when
  // HEAD or the branch moves (the git topic says so), never on a timer.
  const headHash = g.log[0]?.hash ?? "";
  const on = live.value;

  const load = (skip: number) => {
    setBusy(true);
    return link
      .request("git.graph", head ? { skip, head: true } : { skip })
      .then(
        (r) => {
          const p = readGraph(r);
          setRows((old) => (skip === 0 ? p.rows : [...old.slice(0, skip), ...p.rows]));
          setMore(p.more);
          setErr("");
          setLoaded(true);
        },
        (e: unknown) => setErr(errText(e)),
      )
      .finally(() => setBusy(false));
  };
  useEffect(() => {
    if (on && g.repo) void load(0);
  }, [head, headHash, g.branch.name, g.branch.ahead, on, g.repo]);

  const width = lanes(rows) * 16;
  const focusRow = (i: number) => {
    const list = body.current?.querySelectorAll<HTMLElement>("tr.gg-row");
    list?.[i]?.focus();
  };
  const rowMenu = (r: GitGraphRow): MenuItems => {
    const items: (MenuItems[number])[] = [
      { label: t("gg.copy_hash"), run: () => copyText(r.hash) },
      { label: t("gg.copy_subject"), run: () => copyText(r.subject) },
    ];
    if (!viewer) {
      items.push(null, { label: t("gg.rebase"), disabled: !!g.op, run: () => showFile(rebaseTab(r.hash), true) });
      for (const x of r.refs ?? []) {
        if (x.kind === "local" && x.name !== g.branch.name) items.push({ label: t("gg.checkout", { name: x.name }), disabled: !!g.op, run: () => setCheckout(x.name) });
      }
    }
    return items;
  };

  return (
    <View
      tab="files"
      name="graph"
      title={t("gg.tab")}
      actions={
        <>
          <div class="gg-scope" role="radiogroup" aria-label={t("gg.scope")}>
            <button type="button" role="radio" class={`tb gg-scope-b${!head ? " is-on" : ""}`} aria-checked={!head ? "true" : "false"} onClick={() => setHead(false)}>
              {t("gg.all")}
            </button>
            <button type="button" role="radio" class={`tb gg-scope-b${head ? " is-on" : ""}`} aria-checked={head ? "true" : "false"} onClick={() => setHead(true)}>
              {t("gg.head")}
            </button>
          </div>
          <button type="button" class="tb tb-icon" data-act="refresh" aria-label={t("ctr.refresh")} title={t("ctr.refresh")} disabled={busy} onClick={() => void load(0)}>
            <Cod name="refresh" />
          </button>
        </>
      }
      bodyClass="gg-body"
    >
      {err && rows.length === 0 ? (
        <p class="empty refused">{err}</p>
      ) : !g.repo && git ? (
        <p class="empty">{t("git.not_repo")}</p>
      ) : loaded && rows.length === 0 ? (
        <p class="empty">{t("gg.empty")}</p>
      ) : (
        <table class="gg-table" aria-label={t("gg.tab")} aria-busy={busy ? "true" : undefined}>
          <colgroup>
            <col class="gg-col-graph" style={{ width: Math.max(width + 8, 64) + "px" }} />
            <col />
            <col class="gg-col-date" />
            <col class="gg-col-author" />
            <col class="gg-col-hash" />
          </colgroup>
          <thead>
            <tr>
              <th scope="col">{t("gg.col_graph")}</th>
              <th scope="col">{t("gg.col_desc")}</th>
              <th scope="col">{t("gg.col_date")}</th>
              <th scope="col">{t("gg.col_author")}</th>
              <th scope="col">{t("git.commit_do")}</th>
            </tr>
          </thead>
          <tbody ref={body}>
            {rows.map((r, i) => {
              const isOpen = open === r.hash;
              return (
                <Fragment key={r.hash}>
                  <tr
                    class={`gg-row${isOpen ? " is-open" : ""}${r.head ? " is-head" : ""}`}
                    tabIndex={0}
                    data-hash={r.hash}
                    aria-expanded={isOpen ? "true" : "false"}
                    onClick={() => setOpen(isOpen ? null : r.hash)}
                    onContextMenu={onMenu(r.hash, () => rowMenu(r))}
                    onKeyDown={(e) => {
                      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                        e.preventDefault();
                        focusRow(e.key === "ArrowDown" ? Math.min(rows.length - 1, i + 1) : Math.max(0, i - 1));
                      } else if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        setOpen(isOpen ? null : r.hash);
                      }
                    }}
                  >
                    <td class="gg-graph">
                      <GraphCell r={r} width={width} />
                    </td>
                    <td class="gg-desc">
                      {(r.refs ?? []).map((x) => (
                        <RefChip key={x.kind + x.name} x={x} color={r.color} head={r.head === true && x.kind === "local" && x.name === g.branch.name} />
                      ))}
                      <span class="gg-subject" dir="auto">
                        {r.subject}
                      </span>
                    </td>
                    <td class="gg-date">{dateFmt.format(r.when)}</td>
                    <td class="gg-author" dir="auto">
                      {r.author}
                    </td>
                    <td class="gg-hash" dir="ltr">
                      {r.hash}
                    </td>
                  </tr>
                  {isOpen && (
                    <tr class="gg-open">
                      <td class="gg-graph gg-pass">
                        <PassLines r={r} width={width} />
                      </td>
                      <td colSpan={4}>
                        <Details r={r} />
                      </td>
                    </tr>
                  )}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      )}
      {more && (
        <div class="gg-more">
          <button type="button" class="btn btn-neutral" disabled={busy || !on} onClick={() => void load(rows.length)}>
            {t("gg.more")}
          </button>
        </div>
      )}
      {checkout && (
        <AskLaptop
          label={t("gg.checkout", { name: checkout })}
          text={t("gg.checkout_text", { name: checkout })}
          send={() => link.request("git.branches", {}).then(() => link.request("git.checkout", { branch: checkout }))}
          onDone={() => setCheckout(null)}
        />
      )}
    </View>
  );
}
