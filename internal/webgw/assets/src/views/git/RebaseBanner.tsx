// A stopped rebase in source control (WEBP-5): where it stopped (the
// commit, the step, the conflicts) and Continue, Skip or Abort, each
// asked on the laptop (they rewrite the branch), run by the laptop's own
// rebase code.
import { h } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { GitRebaseState } from "../../../../../webproto/proto";
import { link, live } from "../../session";
import { t, type StringKey } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { Cod } from "../../ui/icons";

type Do = "continue" | "skip" | "abort";
const LABEL: Record<Do, StringKey> = { continue: "gr.continue", skip: "gr.skip", abort: "gr.abort" };
const ASK: Record<Do, StringKey> = { continue: "gr.continue_body", skip: "gr.skip_body", abort: "gr.abort_body" };

export function RebaseBanner({ seq }: { seq: unknown }) {
  const [st, setSt] = useState<GitRebaseState | null>(null);
  const [asking, setAsking] = useState<Do | null>(null);
  const viewer = link.meta.value?.role === "viewer";
  const on = live.value;
  useEffect(() => {
    if (!on) return;
    let stop = false;
    link.request("git.rebase_state", {}).then(
      (r) => !stop && setSt(r as GitRebaseState),
      () => !stop && setSt(null),
    );
    return () => {
      stop = true;
    };
  }, [seq, on]);
  const where =
    st?.inProgress && st.hash
      ? t("gr.stopped_at", { hash: st.hash, subject: st.subject ?? "", step: st.step ?? 0, total: st.total ?? 0 })
      : t("gr.in_progress");
  const conflicts = st?.conflicts ?? [];
  return (
    <div class="git-op gr-banner" role="status">
      <p class="gr-banner-line">
        <Cod name="list-ordered" /> {where}
      </p>
      {conflicts.length > 0 && <p class="gr-banner-note">{t("gr.fix_first", { n: conflicts.length })}</p>}
      {st?.action === "edit" && conflicts.length === 0 && <p class="gr-banner-note">{t("gr.edit_now")}</p>}
      {!viewer && (
        <div class="gr-banner-acts">
          {(["continue", "skip", "abort"] as const).map((d) => (
            <button key={d} type="button" class={`btn ${d === "continue" ? "btn-primary" : "btn-neutral"}`} data-act={d} disabled={!on} onClick={() => setAsking(d)}>
              {t(LABEL[d])}
            </button>
          ))}
        </div>
      )}
      {asking && (
        <AskLaptop label={t(LABEL[asking])} text={t(ASK[asking])} send={() => link.request("git.rebase_ctl", { do: asking })} onDone={() => setAsking(null)} />
      )}
    </div>
  );
}
