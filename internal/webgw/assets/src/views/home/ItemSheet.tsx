// The action sheet of a lane, a run or a task's output on the phone's
// Home (MOB-17, plan 2.4). The rows come from lib/itemactions.ts in
// their order: open, ref and ask first, then ops, then the risky ones
// (danger style, armed after 600 ms). An op with a confirm asks inside
// the sheet first, Cancel first and focused; one with a laptop card says
// the laptop asks. Note and Link take one short field first. Dragging
// the sheet down, the dim area or Esc mean Later: nothing is sent.
// ref and ask go to the composer through the props (MOB-5 owns it).
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { go } from "../../lib/router";
import { toast } from "../../lib/toasts";
import { linkArgs, type ItemAction, type ItemRef } from "../../lib/itemactions";
import { link } from "../../session";
import { useArming } from "../../ui/arming";
import { Button } from "../../ui/Button";
import { Sheet } from "../../ui/Sheet";
import { t, type StringKey } from "../../strings";
import { gitFailed as failed } from "../git/errors";

interface Props {
  title: StringKey;
  item: ItemRef;
  actions: ItemAction[];
  /** vars fill the labels and questions ({name}, {label}). */
  vars: Record<string, string>;
  onLater: () => void;
  /** onRef adds the row's chip to the composer. */
  onRef: (ref: ItemRef) => void;
  /** onAsk fills the box with text and attaches the row. */
  onAsk: (text: string, ref: ItemRef) => void;
}

function RiskyButton({ a, vars, onPress }: { a: ItemAction; vars: Record<string, string>; onPress: () => void }) {
  const el = useRef<HTMLDivElement | null>(null);
  const arm = useArming(el);
  return (
    <div ref={el} class={`item-act is-risky${arm.armed ? "" : " is-arming"}`} onPointerDown={arm.pressBegan} onKeyDown={arm.pressBegan}>
      <Button kind="danger" wide needsLink onClick={() => arm.pressOK() && onPress()}>
        {t(a.label, vars)}
      </Button>
    </div>
  );
}

function Confirm({ text, verb, onYes, onNo }: { text: string; verb: string; onYes: () => void; onNo: () => void }) {
  const safe = useRef<HTMLButtonElement | null>(null);
  useEffect(() => safe.current?.focus(), []);
  return (
    <div class="row-ask" role="group" aria-label={text}>
      <p class="row-ask-text">{text}</p>
      <div class="choices">
        <button type="button" class="btn btn-neutral" ref={safe} onClick={onNo}>
          {t("cancel")}
        </button>
        <Button kind="danger" needsLink onClick={onYes}>
          {verb}
        </Button>
      </div>
    </div>
  );
}

function NoteField({ id, onDone }: { id: string; onDone: () => void }) {
  const [v, setV] = useState("");
  const send = () => {
    const text = v.replace(/\s+/g, " ").trim().slice(0, 500);
    if (!text) return;
    link.request("worktrees.note", { id, text }).then(() => {
      toast("ok", t("item.noted"));
      onDone();
    }, failed);
  };
  return (
    <div class="item-field">
      <input class="input" type="text" maxLength={500} value={v} placeholder={t("item.note_ph")} aria-label={t("item.note")} onInput={(e) => setV((e.currentTarget as HTMLInputElement).value)} />
      <div class="choices">
        <button type="button" class="btn btn-neutral" onClick={onDone}>
          {t("cancel")}
        </button>
        <Button kind="primary" needsLink disabled={!v.trim()} onClick={send}>
          {t("item.note_save")}
        </Button>
      </div>
    </div>
  );
}

function LinkField({ id, onDone }: { id: string; onDone: () => void }) {
  const [v, setV] = useState("");
  const [kind, setKind] = useState<"issue" | "pr">("issue");
  const args = linkArgs(id, kind, v);
  const send = () => {
    if (!args) return;
    link.request("worktrees.link", args).then((r) => {
      const note = (r as { note?: unknown } | null)?.note;
      toast("ok", typeof note === "string" && note ? note : t("item.linked"));
      onDone();
    }, failed);
  };
  return (
    <div class="item-field">
      <div class="choices" role="radiogroup" aria-label={t("item.link")}>
        <Button small pressed={kind === "issue"} onClick={() => setKind("issue")}>
          {t("item.link_issue")}
        </Button>
        <Button small pressed={kind === "pr"} onClick={() => setKind("pr")}>
          {t("item.link_pr")}
        </Button>
      </div>
      <input class="input" type="text" inputMode="numeric" maxLength={12} value={v} placeholder={t("item.link_ph")} aria-label={t("item.link_ph")} onInput={(e) => setV((e.currentTarget as HTMLInputElement).value)} />
      {v.trim() !== "" && !args && <p class="sheet-note">{t("item.link_bad")}</p>}
      <div class="choices">
        <button type="button" class="btn btn-neutral" onClick={onDone}>
          {t("cancel")}
        </button>
        <Button kind="primary" needsLink disabled={!args} onClick={send}>
          {t("item.link_save")}
        </Button>
      </div>
    </div>
  );
}

export function ItemSheet({ title, item, actions, vars, onLater, onRef, onAsk }: Props) {
  const [open, setOpen] = useState<ItemAction | null>(null);
  const send = (a: ItemAction) => {
    setOpen(null);
    if (!a.op) return;
    link.request(a.op, a.args ?? {}).then((r) => {
      const note = (r as { note?: unknown } | null)?.note;
      if (a.card) toast("info", t("item.card_sent"));
      else if (typeof note === "string" && note) toast("ok", note);
      onLater();
    }, failed);
  };
  const press = (a: ItemAction) => {
    switch (a.kind) {
      case "open":
        if (a.route) go({ tab: a.route.tab, rest: a.route.rest, query: {} });
        onLater();
        return;
      case "ref":
        onRef(item);
        onLater();
        return;
      case "ask":
        if (a.ask) onAsk(t(a.ask, vars), item);
        onLater();
        return;
      case "op":
        if (a.input || a.confirm) setOpen(a);
        else send(a);
    }
  };
  const cur = open;
  return (
    <Sheet onLater={onLater} label={t(title, vars)}>
      <h2 class="sheet-title" dir="auto">
        {t(title, vars)}
      </h2>
      {cur?.input === "note" ? (
        <NoteField id={item.id} onDone={onLater} />
      ) : cur?.input === "link" ? (
        <LinkField id={item.id} onDone={onLater} />
      ) : cur?.confirm ? (
        <Confirm text={t(cur.confirm, vars)} verb={t(cur.label, vars)} onYes={() => send(cur)} onNo={() => setOpen(null)} />
      ) : (
        <div class="item-acts" role="group" aria-label={t(title, vars)}>
          {actions.map((a) =>
            a.risky && !a.confirm ? (
              <RiskyButton key={a.id} a={a} vars={vars} onPress={() => press(a)} />
            ) : (
              <div key={a.id} class={`item-act${a.risky ? " is-risky" : ""}`} data-act={a.id}>
                <Button wide kind={a.risky ? "danger" : "neutral"} needsLink={a.kind === "op"} onClick={() => press(a)}>
                  {t(a.label, vars)}
                </Button>
              </div>
            ),
          )}
          <button type="button" class="btn btn-neutral btn-wide" onClick={onLater}>
            {t("cancel")}
          </button>
        </div>
      )}
    </Sheet>
  );
}

/** laneVars, runVars and logVars fill the sheet's strings for one row. */
export const laneVars = (title: string) => ({ name: title });
export const runVars = (title: string) => ({ name: title });
export const logVars = (label: string) => ({ name: label, label });

/** SHEET_TITLE is the sheet's title per row kind. */
export const SHEET_TITLE: Record<ItemRef["kind"], StringKey> = { worktree: "item.sheet_lane", run: "item.sheet_run", task: "item.sheet_log" };
