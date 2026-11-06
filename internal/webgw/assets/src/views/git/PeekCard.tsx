// The gutter peek card (lib/gitpeek.ts): the change in colour, as the
// laptop's card shows it. Close is first and has the focus (Enter and
// Esc only close); Stage (Unstage for a staged change) and Next Change
// act at once; Revert… asks here first, Cancel first, then the laptop.
import { h } from "preact";
import { useEffect, useRef } from "preact/hooks";
import type { GitPeek } from "../../../../../webproto/proto";
import { activeDesk } from "../../lib/deskeditor";
import { gitAsk } from "../../lib/gitnav";
import { peekChange, peekNext, peekOpen } from "../../lib/gitpeek";
import { toast } from "../../lib/toasts";
import { link, live } from "../../session";
import { t } from "../../strings";
import { DiffRow, type DiffKind } from "../../ui/rows";
import { Sheet } from "../../ui/Sheet";
import { gitFailed } from "./errors";

/** rowKind reads one hunk row's sign. */
function rowKind(l: string): DiffKind {
  return l.startsWith("+") ? "add" : l.startsWith("-") ? "del" : "ctx";
}

export function PeekHost() {
  const p = peekOpen.value;
  if (!p) return null;
  return <PeekCard key={p.path + ":" + p.line + ":" + p.header + (p.staged ? "s" : "")} p={p} />;
}

function PeekCard({ p }: { p: GitPeek }) {
  const close = useRef<HTMLButtonElement | null>(null);
  // Close takes the focus after the click in the editor's frame and the
  // command list (which gives the focus back as it closes) are done.
  useEffect(() => {
    close.current?.focus();
    const later = setTimeout(() => close.current?.focus(), 30);
    return () => clearTimeout(later);
  }, []);
  const done = () => (peekOpen.value = null);
  const name = p.path.split("/").pop() ?? p.path;
  const canAct = link.meta.value?.role !== "viewer";

  const stage = () => {
    if (!live.value) return;
    link.request(p.staged ? "git.unstage_hunk" : "git.stage_hunk", { path: p.path, hunk: p.header }).then(() => {
      toast("ok", t(p.staged ? "gp.unstaged_ok" : "gp.staged_ok", { name }));
      done();
    }, gitFailed);
  };
  const revert = () => {
    done();
    gitAsk.value = { op: "git.discard_hunk", args: { path: p.path, hunk: p.header }, label: t("gp.revert_label"), text: t("gp.revert_q", { path: p.path, line: p.line }) };
  };
  const next = () => {
    const n = peekNext(p.starts ?? [], p.line);
    activeDesk.peek()?.reveal(n);
    peekChange(p.path, n);
  };

  return (
    <Sheet onLater={done} label={name}>
      <div class="peek-card">
        <p class="peek-title">{t("gp.title", { n: p.index, count: p.count, name })}</p>
        <p class="peek-head" dir="ltr">
          {p.path} {p.header.trim()}
        </p>
        {p.staged && <p class="peek-note">{t("gp.staged")}</p>}
        <div class="diff-rows peek-rows">
          {(p.lines ?? []).map((l, i) => (
            <DiffRow key={i} kind={rowKind(l)} text={l.slice(1)} />
          ))}
        </div>
        <div class="choices">
          <button type="button" class="btn btn-neutral" ref={close} onClick={done}>
            {t("close")}
          </button>
          {canAct && (
            <button type="button" class={`btn ${p.staged ? "btn-neutral" : "btn-primary"}`} onClick={stage}>
              {t(p.staged ? "git.unstage" : "git.stage")}
            </button>
          )}
          {canAct && !p.staged && (
            <button type="button" class="btn btn-danger" onClick={revert}>
              {t("gp.revert")}
            </button>
          )}
          {p.count > 1 && (
            <button type="button" class="btn btn-neutral" onClick={next}>
              {t("dv.next")}
            </button>
          )}
        </div>
      </div>
    </Sheet>
  );
}
