// The Home's pinned views (MOB-71, plan 2.1 and 3.11.7): this phone's
// pins from the laptop, each collapsed to its name; a tap opens it and
// the view runner (view.run, MOB-9) draws its live block below. Offline
// the rows say Stale: what shows is the last answer, not live. Unpin
// keeps the saved view (the All tools menu still lists it).
import { Fragment, h, type ComponentChildren } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { SavedView } from "../../../../webproto/proto";
import { link } from "../session";
import { t } from "../strings";
import "../strings.phone";
import { errText } from "../ui/CardSheet";
import { toast } from "../lib/toasts";
import { loadSaved, pinView, pinnedViews, saved } from "./saved";

/** PinnedViews lists the pins; run draws an open view's block. */
export function PinnedViews({ run }: { run?: (v: SavedView) => ComponentChildren }) {
  const online = link.live.value;
  const [open, setOpen] = useState<Set<string>>(new Set());
  useEffect(() => {
    if (online) loadSaved().catch(() => undefined);
  }, [online]);
  const pins = pinnedViews(saved.value);
  if (pins.length === 0) return null;
  const toggle = (id: string) => {
    const n = new Set(open);
    if (n.has(id)) n.delete(id);
    else n.add(id);
    setOpen(n);
  };
  const unpin = (v: SavedView) =>
    pinView(v.id, false).then(
      () => toast("info", t("saved.unpinned", { name: v.name })),
      (e: unknown) => toast("warn", errText(e)),
    );
  return (
    <section class="pinned" aria-label={t("saved.title")}>
      {pins.map((v) => {
        const isOpen = open.has(v.id);
        return (
          <Fragment key={v.id}>
            <div class="pinned-row">
              <button type="button" class="pinned-head" aria-expanded={isOpen ? "true" : "false"} onClick={() => toggle(v.id)}>
                <span class="pinned-pin" aria-hidden="true">📌</span>
                <span class="pinned-name">{v.name}</span>
                <span class={`pinned-state${online ? " is-live" : " is-stale"}`}>
                  <span aria-hidden="true">{online ? "●" : "◌"}</span> {online ? t("blk.live") : t("blk.stale")}
                </span>
              </button>
              <button type="button" class="pinned-unpin" aria-label={t("saved.unpin", { name: v.name })} disabled={!online} onClick={() => void unpin(v)}>
                <span aria-hidden="true">×</span>
              </button>
            </div>
            {isOpen && run && <div class="pinned-body">{run(v)}</div>}
          </Fragment>
        );
      })}
    </section>
  );
}
