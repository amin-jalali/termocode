// The merge editor (desktop editor tab "@git:merge:<path>"): a
// conflicted file's incoming side (theirs) and current side (ours) on
// top, read only, and the result below, which you edit. Each conflict
// takes Accept Incoming, Accept Current or Accept Both (the laptop's own
// resolutions, given by git.conflict); Complete Merge saves the result
// and marks the file resolved, if it is still the file you read. A file
// that cannot be merged line by line keeps one side through the laptop.
import { Crumbs } from "../FileView";
import { h } from "preact";
import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import type { GitConflict, GitConflictPart } from "../../../../../webproto/proto";
import { mountDesk, setDesk, type DeskHandle } from "../../lib/deskeditor";
import { mergeTab } from "../../lib/gittab";
import { toast } from "../../lib/toasts";
import { closeTab, wordWrap } from "../../lib/workbench";
import type { FrameLines } from "../../monaco/protocol";
import { link, live } from "../../session";
import { t } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { Cod } from "../../ui/icons";
import { View } from "../View";
import { gitErrText, gitFailed } from "./errors";

type Choice = "theirs" | "ours" | "both";

const MARKERS = /^(<{7}|>{7})( |$)/m;

/** lineAt is the 1-based line of offset at in text. */
function lineAt(text: string, at: number): number {
  let n = 1;
  for (let i = text.indexOf("\n"); i >= 0 && i < at; i = text.indexOf("\n", i + 1)) n++;
  return n;
}

/** rows is how many lines a block of text fills ("a\nb\n": 2). */
function rows(s: string): number {
  if (!s) return 0;
  const n = s.split("\n").length;
  return s.endsWith("\n") ? n - 1 : n;
}

/** resultLines colours the conflicts still in the result: the markers,
 * the current side and the incoming side of each. */
function resultLines(text: string, parts: readonly GitConflictPart[]): FrameLines[] {
  const out: FrameLines[] = [];
  for (const p of parts) {
    const at = text.indexOf(p.raw);
    if (at < 0) continue;
    const first = lineAt(text, at);
    const ls = p.raw.split("\n");
    const sep = ls.findIndex((l, i) => i > 0 && /^={7}( |$)/.test(l));
    const base = ls.findIndex((l, i) => i > 0 && i < sep && /^\|{7}( |$)/.test(l));
    const last = first + rows(p.raw) - 1;
    out.push({ from: first, to: first, kind: "marker" });
    const curEnd = (base > 0 ? base : sep) - 1;
    if (curEnd >= 1) out.push({ from: first + 1, to: first + curEnd, kind: "current" });
    if (base > 0) out.push({ from: first + base, to: first + sep, kind: "marker" });
    else out.push({ from: first + sep, to: first + sep, kind: "marker" });
    if (last - 1 >= first + sep + 1) out.push({ from: first + sep + 1, to: last - 1, kind: "incoming" });
    out.push({ from: last, to: last, kind: "marker" });
  }
  return out;
}

/** sideLines colours each conflict's lines in one side's whole file. */
function sideLines(text: string, parts: readonly GitConflictPart[], pick: (p: GitConflictPart) => string, kind: FrameLines["kind"]): FrameLines[] {
  const out: FrameLines[] = [];
  let from = 0;
  for (const p of parts) {
    const s = pick(p);
    if (!s) continue;
    const at = text.indexOf(s, from);
    if (at < 0) continue;
    const first = lineAt(text, at);
    out.push({ from: first, to: first + rows(s) - 1, kind });
    from = at + s.length;
  }
  return out;
}

function readConflict(r: unknown): GitConflict | null {
  const c = (r ?? {}) as Partial<GitConflict>;
  return typeof c.text === "string" && typeof c.version === "string" && Array.isArray(c.blocks) && c.ours && c.theirs ? (c as GitConflict) : null;
}

/** Pane mounts one read-only side. */
function Pane({ label, text, lines }: { label: string; text: string; lines: FrameLines[] }) {
  const box = useRef<HTMLDivElement | null>(null);
  const ed = useRef<DeskHandle | null>(null);
  useEffect(() => {
    if (!box.current) return;
    const hd = mountDesk(box.current, {
      doc: text,
      label,
      readOnly: true,
      wrap: wordWrap.peek(),
      tabSize: 4,
      insertSpaces: false,
      onChange: () => undefined,
      onSave: () => undefined,
      onDone: () => undefined,
      onViewport: () => undefined,
    });
    ed.current = hd;
    return () => {
      hd.destroy();
      ed.current = null;
    };
  }, []);
  useEffect(() => ed.current?.lines(lines), [lines]);
  return <div ref={box} class="editor-host desk-host gm-host" />;
}

/** WholeFile settles a conflict that cannot be merged line by line. */
function WholeFile({ c }: { c: GitConflict }) {
  const [side, setSide] = useState<"ours" | "theirs" | null>(null);
  const word = (s: GitConflict["ours"]) => t(!s.present ? "gm.deleted" : s.binary ? "gm.binary" : "gm.changed");
  const viewer = link.meta.value?.role === "viewer";
  return (
    <div class="gm-whole">
      <p class="gm-whole-why">{t("gm.whole_body")}</p>
      <p>
        {t("gm.incoming")}: {word(c.theirs)} · {t("gm.current")}: {word(c.ours)}
      </p>
      {!viewer && (
        <div class="gm-whole-acts">
          <button type="button" class="gv-btn gv-btn-sec" data-act="keep-theirs" onClick={() => setSide("theirs")}>
            {t("gm.keep_incoming")}
          </button>
          <button type="button" class="gv-btn gv-btn-sec" data-act="keep-ours" onClick={() => setSide("ours")}>
            {t("gm.keep_current")}
          </button>
        </div>
      )}
      {side && (
        <AskLaptop
          label={t(side === "ours" ? "gm.keep_current" : "gm.keep_incoming")}
          text={t("gm.keep_body", { path: c.path })}
          send={() => link.request("git.resolve_side", { path: c.path, side })}
          onDone={() => setSide(null)}
          onConfirmed={() => closeTab(mergeTab(c.path))}
        />
      )}
    </div>
  );
}

export function MergeEditor({ path, slot = "g0" }: { path: string; slot?: string }) {
  const [c, setC] = useState<GitConflict | null>(null);
  const [err, setErr] = useState("");
  const [again, setAgain] = useState(0);
  const [tick, setTick] = useState(0);
  const [cur, setCur] = useState(0);
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const box = useRef<HTMLDivElement | null>(null);
  const ed = useRef<DeskHandle | null>(null);
  const cancel = useRef<HTMLButtonElement | null>(null);
  const viewer = link.meta.value?.role === "viewer";
  const on = live.value;
  const name = path.slice(path.lastIndexOf("/") + 1);

  useEffect(() => {
    if (!on) return;
    let stop = false;
    link.request("git.conflict", { path }).then(
      (r) => {
        if (stop) return;
        setC(readConflict(r));
        setErr("");
      },
      (e: unknown) => !stop && setErr(gitErrText(e)),
    );
    return () => {
      stop = true;
    };
  }, [path, again, on]);

  const text = c && ed.current ? ed.current.text() : c?.text ?? "";
  const parts = c?.blocks ?? [];
  const open = parts.map((p) => text.includes(p.raw));
  const left = open.filter(Boolean).length;
  const markers = MARKERS.test(text);
  const whole = c !== null && (!c.ours.present || !c.theirs.present || c.ours.binary === true || c.theirs.binary === true);

  // The result editor: the file as saved, its markers in it.
  useEffect(() => {
    if (!c || whole || !box.current) return;
    const hd = mountDesk(box.current, {
      doc: c.text,
      label: t("gm.result_of", { name }),
      path,
      slot,
      readOnly: viewer,
      wrap: wordWrap.peek(),
      tabSize: 4,
      insertSpaces: false,
      onChange: () => setTick((x) => x + 1),
      onSave: () => undefined,
      onDone: () => undefined,
      onViewport: () => undefined,
    });
    ed.current = hd;
    setDesk(slot, hd);
    setTick((x) => x + 1);
    return () => {
      setDesk(slot, null);
      hd.destroy();
      ed.current = null;
    };
  }, [c]);
  useEffect(() => ed.current?.lines(resultLines(text, parts)), [tick, c]);
  useEffect(() => ed.current?.opts({ wrap: wordWrap.value }), [wordWrap.value]);
  useEffect(() => {
    if (asking) cancel.current?.focus();
  }, [asking]);

  // The conflict the buttons act on: the chosen one if still open, else
  // the first open one.
  const at = open[cur] ? cur : open.indexOf(true);
  const reveal = (i: number) => {
    const p = parts[i], hd = ed.current;
    if (!p || !hd) return;
    const o = hd.text().indexOf(p.raw);
    if (o >= 0) hd.reveal(lineAt(hd.text(), o));
  };
  const go = (dir: 1 | -1) => {
    if (left === 0) return;
    let i = at;
    for (let k = 0; k < parts.length; k++) {
      i = (i + dir + parts.length) % parts.length;
      if (open[i]) break;
    }
    setCur(i);
    reveal(i);
  };
  const accept = (choice: Choice) => {
    const p = parts[at], hd = ed.current;
    if (!p || !hd) return;
    const all = hd.text();
    const o = all.indexOf(p.raw);
    if (o < 0) return;
    hd.replace(all.slice(0, o) + p[choice] + all.slice(o + p.raw.length));
    setTick((x) => x + 1);
    const next = open.findIndex((v, i) => v && i !== at);
    if (next >= 0) {
      setCur(next);
      requestAnimationFrame(() => reveal(next));
    }
  };
  const complete = () => {
    const hd = ed.current;
    if (!c || !hd) return;
    setBusy(true);
    link
      .request("git.resolve", { path, text: hd.text(), version: c.version })
      .then(() => {
        toast("ok", t("gm.resolved", { name }));
        closeTab(mergeTab(path));
      }, gitFailed)
      .finally(() => {
        setBusy(false);
        setAsking(false);
      });
  };

  const incLines = useMemo(() => sideLines(c?.theirs.text ?? "", parts, (p) => p.theirs, "incoming"), [c]);
  const curLines = useMemo(() => sideLines(c?.ours.text ?? "", parts, (p) => p.ours, "current"), [c]);
  const theirsLabel = parts[0]?.theirsLabel ?? "";
  const oursLabel = parts[0]?.oursLabel ?? "";
  const canAct = !viewer && on && left > 0;
  return (
    <View tab="files" name="merge" title={name} sub={path.includes("/") ? <Crumbs dir={path.slice(0, path.lastIndexOf("/"))} file={path} /> : undefined} bodyClass="gm-body">
      {err ? (
        <div class="gm-err">
          <p class="empty refused">{err}</p>
          <button type="button" class="gv-btn gv-btn-sec" onClick={() => setAgain((x) => x + 1)}>
            {t("gm.reload")}
          </button>
        </div>
      ) : !c ? (
        <p class="empty">{t("loading")}</p>
      ) : whole ? (
        <WholeFile c={c} />
      ) : (
        <div class="gm">
          <div class="gm-top">
            <section class="gm-pane gm-incoming" aria-label={t("gm.incoming")}>
              <header class="gm-head">
                <span class="gm-title">{t("gm.incoming")}</span>
                <span class="gm-label" dir="ltr">
                  {theirsLabel}
                </span>
                <span class="gm-gap" />
                <button type="button" class="gm-act" data-act="theirs" disabled={!canAct} onClick={() => accept("theirs")}>
                  {t("gm.accept_incoming")}
                </button>
              </header>
              <Pane label={t("gm.incoming")} text={c.theirs.text ?? ""} lines={incLines} />
            </section>
            <section class="gm-pane gm-current" aria-label={t("gm.current")}>
              <header class="gm-head">
                <span class="gm-title">{t("gm.current")}</span>
                <span class="gm-label" dir="ltr">
                  {oursLabel}
                </span>
                <span class="gm-gap" />
                <button type="button" class="gm-act" data-act="ours" disabled={!canAct} onClick={() => accept("ours")}>
                  {t("gm.accept_current")}
                </button>
              </header>
              <Pane label={t("gm.current")} text={c.ours.text ?? ""} lines={curLines} />
            </section>
          </div>
          <section class="gm-pane gm-result" aria-label={t("gm.result")}>
            <header class="gm-head">
              <span class="gm-title">{t("gm.result")}</span>
              <span class="gm-label" dir="ltr">
                {path}
              </span>
              <span class="gm-gap" />
              <span class={`gm-left${left === 0 ? " is-done" : ""}`} role="status">
                {left === 0 ? t("gm.none_left") : t("gm.left", { n: left, at: at + 1 })}
              </span>
              <button type="button" class="tb tb-icon" data-act="prev" aria-label={t("gm.prev")} title={t("gm.prev")} disabled={left === 0} onClick={() => go(-1)}>
                <Cod name="arrow-up" />
              </button>
              <button type="button" class="tb tb-icon" data-act="next" aria-label={t("gm.next")} title={t("gm.next")} disabled={left === 0} onClick={() => go(1)}>
                <Cod name="arrow-down" />
              </button>
              <button type="button" class="gm-act" data-act="both" disabled={!canAct} onClick={() => accept("both")}>
                {t("gm.accept_both")}
              </button>
            </header>
            <div ref={box} class="editor-host desk-host gm-host" />
            <footer class="gm-foot">
              {markers && left === 0 && <span class="gm-bad">{t("gm.markers_left")}</span>}
              <span class="gm-gap" />
              {asking ? (
                <span class="gm-ask" role="group" aria-label={t("gm.complete")}>
                  <span class="gm-ask-text">{t("gm.complete_ask", { name })}</span>
                  <button type="button" class="gv-btn gv-btn-sec" ref={cancel} onClick={() => setAsking(false)}>
                    {t("cancel")}
                  </button>
                  <button type="button" class="gv-btn" data-act="save" disabled={busy || !on} onClick={complete}>
                    {t("gm.complete")}
                  </button>
                </span>
              ) : (
                !viewer && (
                  <button type="button" class="gv-btn" data-act="complete" disabled={markers || !on} onClick={() => setAsking(true)}>
                    {t("gm.complete")}
                  </button>
                )
              )}
            </footer>
          </section>
        </div>
      )}
    </View>
  );
}
