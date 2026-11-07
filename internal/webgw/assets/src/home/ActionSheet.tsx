// The phone's action sheet (MOB-5, mobile plan 2.4): a long press or the
// ⋯ of a row or block opens it with what can be done with that item.
//   op    runs one termocode op; args come from the item's Ref through
//         lib/actions.ts (one table). A risky op asks once more, with
//         Cancel first and focused.
//   ask   fills the box with a short request and the item as a chip;
//         nothing is sent until Send.
//   ref   adds the item as a chip ("Add to message").
//   open  goes to a view of the page.
// A viewer sees open and ref only. Cancel is first and closes the sheet;
// swiping it down or tapping the dim area answers nothing.
import { h, H } from "./jsx";
import { actionsFor, defaultActions, opArgs, refAttach, type Action, type Ref } from "../lib/actions";
import { parseHash } from "../state/route";
import type { BlockAction, BlockRef } from "../../../../webproto/proto";

export interface SheetReq {
  /** chat is the chat whose composer gets the chips. */
  chat: string;
  title: string;
  ref: Ref;
  /** actions of the block or item; none: defaultActions(ref). */
  actions?: Action[];
}

let show: ((r: SheetReq | null) => void) | null = null;
/** homeChat is the chat the Home shows (ActionSheetHost keeps it). */
let homeChat = "";

/** openActions opens the sheet for one item (ActionSheetHost draws it). */
export function openActions(r: SheetReq): void {
  show?.(r);
}

const ATTACH_KINDS = new Set(["task", "ptask", "issue", "pr", "file", "lines", "diff", "worktree", "run", "race", "workflow", "card", "resource", "block", "project", "view"]);

/** refOfBlock maps a block's reference (MOB-3) to an attach reference: a
 * line becomes a one-line lines ref; an unknown kind goes as a block. */
export function refOfBlock(r: BlockRef, label: string, blockID = ""): Ref {
  if (r.kind === "file" && r.path && r.line) return { kind: "lines", path: r.path, from: r.line, to: r.line, label: `${label}:${r.line}` };
  if (ATTACH_KINDS.has(r.kind)) return { kind: r.kind as Ref["kind"], id: r.id, path: r.path, label };
  return { kind: "block", id: blockID || r.id || r.kind, label };
}

/** actionOfBlock maps a block's action (MOB-3 kinds) onto the sheet's. */
export function actionOfBlock(a: BlockAction): Action | null {
  switch (a.kind) {
    case "op":
      return a.op ? { id: a.id, label: a.label, kind: "op", op: a.op as Action["op"], danger: a.risky } : null;
    case "attach":
      return { id: a.id, label: a.label, kind: "ref" };
    case "laptop":
    case "view":
      return null; // the Views menu (MOB-9) and the laptop's own screens open those
  }
  return null;
}

/** refOfRow maps a Home row id ("<kind>:<ref>", state/home.ts) to a
 * reference; null for rows that name nothing to send (ci, lane). */
export function refOfRow(id: string, title: string): Ref | null {
  const i = id.indexOf(":");
  const kind = i < 0 ? id : id.slice(0, i);
  const ref = i < 0 ? "" : id.slice(i + 1);
  if (!ref) return null;
  const label = title.slice(0, 120);
  switch (kind) {
    case "file":
      return { kind: "file", path: ref, label };
    case "task":
    case "run":
    case "card":
    case "workflow":
    case "race":
    case "project":
      return { kind, id: ref, label };
  }
  return null;
}

/** openRow opens the sheet for a Home row (long press or its ⋯). */
export function openRow(id: string, title: string): boolean {
  const ref = refOfRow(id, title);
  if (!ref || !show) return false;
  show({ chat: homeChat, title, ref });
  return true;
}

export function ActionSheetHost({ chat }: { chat: string }) {
  homeChat = chat;
  const [req, setReq] = H.useState<SheetReq | null>(null);
  H.useEffect(() => {
    show = setReq;
    return () => {
      if (show === setReq) show = null;
    };
  }, []);
  if (!req) return null;
  return <ActionSheet req={req} onDone={() => setReq(null)} />;
}

function ActionSheet({ req, onDone }: { req: SheetReq; onDone: () => void }) {
  const role = H.link.meta.value?.role ?? "viewer";
  const list = actionsFor(role, req.ref, req.actions?.length ? req.actions : defaultActions(req.ref));
  const [confirm, setConfirm] = H.useState<Action | null>(null);
  const [note, setNote] = H.useState<string | null>(null);
  const [busy, setBusy] = H.useState(false);

  const chip = (): boolean => {
    if (H.addAttach(req.chat, refAttach(req.ref))) return true;
    setNote("A message takes 8 items at most. Remove one first.");
    return false;
  };

  const run = (a: Action) => {
    const args = opArgs(a, req.ref);
    if (!a.op || args === null || busy) return;
    setBusy(true);
    H.link.request(a.op, args).then(
      () => onDone(),
      (e: unknown) => {
        setBusy(false);
        setConfirm(null);
        setNote(H.isOffline(e) ? "Offline. Try again when the link is back." : H.errText(e));
      },
    );
  };

  const pick = (a: Action) => {
    setNote(null);
    switch (a.kind) {
      case "op":
        if (a.danger) setConfirm(a);
        else run(a);
        return;
      case "ask":
        if (!chip()) return;
        H.setDraft(req.chat, a.ask ?? "");
        onDone();
        return;
      case "ref":
        if (chip()) onDone();
        return;
      case "open":
        if (a.route) H.go(parseHash(a.route));
        onDone();
    }
  };

  if (confirm) {
    return (
      <H.Sheet onLater={onDone} label={confirm.label}>
        <div class="actsheet">
          <h2 class="actsheet-title" dir="auto">
            {confirm.label}: {req.title}
          </h2>
          <p class="actsheet-hint">This changes something on the laptop.</p>
          <div class="actsheet-list">
            <button type="button" class="actsheet-row actsheet-cancel" onClick={() => setConfirm(null)}>
              Cancel
            </button>
            <button type="button" class="actsheet-row actsheet-danger" disabled={busy || !H.live.value} onClick={() => run(confirm)}>
              <span aria-hidden="true">✗ </span>
              {confirm.label}
            </button>
          </div>
          {note && (
            <p class="actsheet-note" role="alert">
              {note}
            </p>
          )}
        </div>
      </H.Sheet>
    );
  }

  return (
    <H.Sheet onLater={onDone} label={req.title}>
      <div class="actsheet">
        <h2 class="actsheet-title" dir="auto">
          {req.title}
        </h2>
        <div class="actsheet-list" role="menu">
          <button type="button" role="menuitem" class="actsheet-row actsheet-cancel" onClick={onDone}>
            Cancel
          </button>
          {list.map((a) => (
            <button
              type="button"
              role="menuitem"
              key={a.id}
              class={"actsheet-row" + (a.danger ? " actsheet-danger" : "")}
              disabled={a.kind === "op" && (busy || !H.live.value)}
              onClick={() => pick(a)}
            >
              <span class="actsheet-kind" aria-hidden="true">
                {a.kind === "op" ? (a.danger ? "✗" : "▶") : a.kind === "ask" ? "?" : a.kind === "ref" ? "↩" : "→"}
              </span>
              <span dir="auto">{a.label}</span>
            </button>
          ))}
        </div>
        {note && (
          <p class="actsheet-note" role="alert">
            {note}
          </p>
        )}
      </div>
    </H.Sheet>
  );
}
