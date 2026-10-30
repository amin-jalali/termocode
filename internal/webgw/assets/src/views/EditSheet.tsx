// The edit sheet (WEB-22, web UX 3.4): the selected rows and 20 around
// them in a text box. Cancel first, then Send: one buf.set_text with the
// tick of the rows the sheet opened with. When the laptop changed those
// rows meanwhile, the sheet keeps the user's text, says so, can show what
// changed and offers Send again (on the laptop's new text).
import { h } from "preact";
import { useState } from "preact/hooks";
import type { BufSnap } from "../../../../webproto/proto";
import { cancelEdit, draft, send, setText } from "../lib/edit";
import { toast } from "../lib/toasts";
import { live } from "../session";
import { t, tn } from "../strings";
import { Button } from "../ui/Button";
import { DiffRow } from "../ui/rows";
import { Sheet } from "../ui/Sheet";

/** diffRows is a plain two-part diff: the old rows that went, the new that came. */
export function diffRows(old: readonly string[], now: readonly string[]): { del: string[]; add: string[]; at: number } {
  let a = 0;
  while (a < old.length && a < now.length && old[a] === now[a]) a++;
  let oe = old.length, ne = now.length;
  while (oe > a && ne > a && old[oe - 1] === now[ne - 1]) {
    oe--;
    ne--;
  }
  return { del: old.slice(a, oe), add: now.slice(a, ne), at: a };
}

export function EditSheet({ buf }: { buf: BufSnap }) {
  const d = draft.value;
  const [diff, setDiff] = useState(false);
  if (!d || d.buf !== buf.buf) return null;
  const name = d.path.slice(d.path.lastIndexOf("/") + 1);
  const go = (again: boolean) => {
    if (!live.peek()) return;
    send(buf, again).then((r) => {
      if (!r.ok) return;
      toast("ok", r.a === r.b ? tn("edit.sent", 1, { n: r.a + 1 }) : tn("edit.sent", 2, { a: r.a + 1, b: r.b + 1 }));
    });
  };
  const now = buf.lines.slice(d.a, Math.min(d.end, buf.lines.length));
  const dr = d.conflict ? diffRows(d.base, now) : null;
  return (
    <Sheet onLater={cancelEdit} label={t("edit.title", { name, a: d.a + 1, b: Math.max(d.a + 1, d.end) })}>
      <section class="edit" role="dialog" aria-modal="true" aria-labelledby="edit-t">
        <h2 class="edit-title" id="edit-t" dir="auto">
          {t("edit.title", { name, a: d.a + 1, b: Math.max(d.a + 1, d.end) })}
        </h2>
        {d.conflict && (
          <div class="file-note edit-conflict" role="alert">
            <span>{t("edit.conflict")}</span>
            <button type="button" class="link edit-diff-btn" aria-expanded={diff ? "true" : "false"} onClick={() => setDiff(!diff)}>
              {t("edit.show_diff")}
            </button>
          </div>
        )}
        {dr && diff && (
          <div class="edit-diff">
            {dr.del.map((x, i) => (
              <DiffRow key={"d" + i} kind="del" a={d.a + dr.at + i + 1} text={x} />
            ))}
            {dr.add.map((x, i) => (
              <DiffRow key={"a" + i} kind="add" b={d.a + dr.at + i + 1} text={x} />
            ))}
          </div>
        )}
        {d.note && (
          <p class="composer-note" role="alert" dir="auto">
            {d.note}
          </p>
        )}
        <textarea
          class="edit-box"
          dir="ltr"
          spellcheck={false}
          autocapitalize="off"
          autocomplete="off"
          value={d.text}
          aria-labelledby="edit-t"
          onInput={(e) => setText((e.currentTarget as HTMLTextAreaElement).value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
              e.preventDefault();
              go(d.conflict === true);
            }
          }}
        />
        <div class="edit-actions">
          <Button kind="neutral" onClick={cancelEdit}>
            {t("cancel")}
          </Button>
          <span class="edit-send">
            <Button kind="primary" needsLink disabled={d.sending} onClick={() => go(d.conflict === true)}>
              {d.conflict ? t("edit.send_again") : t("send")}
            </Button>
          </span>
        </div>
      </section>
    </Sheet>
  );
}
