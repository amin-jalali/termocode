// The AI's diff tabs (desktop, WEBP-3). An AI suggestion ("@ai:<n>:<path>",
// from Fix, Edit or Doc with AI) shows the file as it was on the left and
// the suggestion on the right, where it can still be typed in; each
// change has an arrow in the gutter that takes it back. Accept writes
// what is left in one edit; Reject drops it, the file unchanged. A file
// an agent changed in its own worktree ("@agent:<id>:<path>") shows the
// file before and after the agent, read only.
import { Crumbs } from "./FileView";
import { Fragment, h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { AIWorktreeText } from "../../../../webproto/proto";
import { acceptProposal, hubSays, proposals, rejectProposal, type Proposal } from "../lib/ai";
import { desks, mountDesk, setDesk, type DeskHandle } from "../lib/deskeditor";
import type { DiffTab } from "../lib/difftab";
import { wordWrap } from "../lib/workbench";
import { link, live } from "../session";
import { t } from "../strings";
import { Chip } from "../ui/Chip";
import { Cod } from "../ui/icons";
import { diffCollapse, diffInline } from "./DiffView";
import { View } from "./View";

/** Side mounts the diff editor: left read only, right typed in unless
 * readOnly; revert puts an arrow on each change. */
function Sides({ path, left, right, slot, readOnly, revert, onEditor }: { path: string; left: string; right: string; slot: string; readOnly: boolean; revert: boolean; onEditor?: (h: DeskHandle | null) => void }) {
  const box = useRef<HTMLDivElement | null>(null);
  const ed = useRef<DeskHandle | null>(null);
  useEffect(() => {
    if (!box.current) return;
    const h = mountDesk(box.current, {
      doc: right,
      label: path,
      path,
      slot,
      readOnly,
      wrap: wordWrap.peek(),
      tabSize: 4,
      insertSpaces: false,
      diff: { original: left, inline: diffInline.peek(), collapse: diffCollapse.peek(), revert },
      onChange: () => undefined,
      onSave: () => undefined,
      onDone: () => undefined,
      onViewport: () => undefined,
    });
    ed.current = h;
    setDesk(slot, h);
    onEditor?.(h);
    return () => {
      onEditor?.(null);
      setDesk(slot, null);
      h.destroy();
      ed.current = null;
    };
  }, []);
  useEffect(() => ed.current?.diffView({ inline: diffInline.value, collapse: diffCollapse.value }), [diffInline.value, diffCollapse.value]);
  useEffect(() => ed.current?.opts({ wrap: wordWrap.value }), [wordWrap.value]);
  return <div ref={box} class="editor-host desk-host" />;
}

function name(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1);
}

/** Moves are the diff's Previous / Next Change buttons. */
function Moves({ slot }: { slot: string }) {
  const go = (dir: 1 | -1) => desks.peek().get(slot)?.diffGo(dir);
  return (
    <>
      <button type="button" class="tb tb-icon" data-act="prev" aria-label={t("dv.prev")} title={`${t("dv.prev")} (Shift+F7)`} onClick={() => go(-1)}>
        <Cod name="arrow-up" />
      </button>
      <button type="button" class="tb tb-icon" data-act="next" aria-label={t("dv.next")} title={`${t("dv.next")} (F7)`} onClick={() => go(1)}>
        <Cod name="arrow-down" />
      </button>
    </>
  );
}

function Proposed({ p, slot }: { p: Proposal; slot: string }) {
  const ed = useRef<DeskHandle | null>(null);
  const [busy, setBusy] = useState(false);
  const controller = link.meta.value?.role === "controller";
  const accept = () => {
    const h = ed.current;
    if (!h || busy) return;
    setBusy(true);
    void acceptProposal(p, h.text()).finally(() => setBusy(false));
  };
  return (
    <View
      tab="files"
      name="ai-diff"
      title={name(p.path)}
      sub={p.path.includes("/") ? <Crumbs dir={p.path.slice(0, p.path.lastIndexOf("/"))} file={p.path} /> : undefined}
      bodyClass="is-editing"
      actions={
        <>
          {p.title && <Chip>{p.title}</Chip>}
          <Moves slot={slot} />
          <button type="button" class="ai-btn is-primary" data-act="accept" disabled={!controller || busy || !live.value} title={t("ai.accept_tip")} onClick={accept}>
            <Cod name="check" />
            {t("card.accept")}
          </button>
          <button type="button" class="ai-btn" data-act="reject" title={t("ai.reject_tip")} onClick={() => rejectProposal(p)}>
            <Cod name="close" />
            {t("card.reject")}
          </button>
        </>
      }
    >
      <p class="file-note ai-note" role="status">
        {t("ai.body_review")}
      </p>
      <div class="diff-host">
        <Sides path={p.path} left={p.before} right={p.after} slot={slot} readOnly={!controller} revert={controller} onEditor={(h) => (ed.current = h)} />
      </div>
    </View>
  );
}

function AgentFile({ d, slot }: { d: DiffTab; slot: string }) {
  const [res, setRes] = useState<AIWorktreeText | null>(null);
  const [err, setErr] = useState("");
  const on = live.value;
  useEffect(() => {
    if (!on) return;
    let stop = false;
    link.request("ai.worktree_text", { id: d.hash, path: d.path }).then(
      (r) => !stop && setRes(r as AIWorktreeText),
      (e: unknown) => !stop && setErr(hubSays(e)),
    );
    return () => {
      stop = true;
    };
  }, [d.hash, d.path, on]);
  return (
    <View
      tab="files"
      name="ai-diff"
      title={name(d.path)}
      sub={d.path.includes("/") ? <Crumbs dir={d.path.slice(0, d.path.lastIndexOf("/"))} file={d.path} /> : undefined}
      bodyClass="is-editing"
      actions={
        <>
          <Chip>{t("viewonly")}</Chip>
          <Moves slot={slot} />
        </>
      }
    >
      {res?.truncated && (
        <p class="file-note" role="status">
          {t("dv.cut")}
        </p>
      )}
      <div class="diff-host">
        {err ? (
          <p class="empty refused">{err}</p>
        ) : res?.secret ? (
          <p class="empty refused">{t("ai.secret")}</p>
        ) : res?.binary ? (
          <p class="empty">{t("dv.binary")}</p>
        ) : res ? (
          <Sides path={d.path} left={res.before} right={res.after} slot={slot} readOnly revert={false} />
        ) : (
          <div class="editor-host desk-host" />
        )}
      </div>
    </View>
  );
}

/** AIDiff is the tab of an AI suggestion or of an agent's file. */
export function AIDiff({ d, slot }: { d: DiffTab; slot: string }) {
  if (d.kind === "agent") return <AgentFile d={d} slot={slot} />;
  const p = proposals.value.get(Number(d.hash));
  if (!p) return <p class="empty">{t("ai.gone")}</p>;
  return <Proposed key={p.n} p={p} slot={slot} />;
}
