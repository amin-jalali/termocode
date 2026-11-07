// Saved views on the phone (MOBUX-18, mobile plan 3.11.7). "Save as
// view" keeps a block's spec under a name on the laptop (view.save),
// with "📌 Pin to Home"; the Saved views list opens, pins, renames and
// deletes them (delete asks in the row, Cancel first and focused); the
// Home's Pinned views run each pinned spec again, live, on the laptop
// (view.run with live, ev view_update) when it is opened, never from
// saved data. The list is the laptop's, shared by every paired phone: a
// change anywhere comes back as ev views_saved. Every target is 44 px;
// pinned is told by 📌 and words, not colour alone.
import type { SavedView, SavedViewList, ViewSpec } from "../../../../webproto/proto";
import { H, h } from "./jsx";
import { canPin, defaultName, nameOK, pinnedViews, readSavedList, refusal, sameName, MAX_VIEW_NAME } from "./savedstate";
import { sv } from "./savedstrings";

type Sig<T> = { value: T; peek(): T };

let list: Sig<SavedViewList> | null = null;
let started = false;

/** saved is the laptop's saved views (an empty list until the first answer). */
export function saved(): Sig<SavedViewList> {
  if (!list) list = H.signal<SavedViewList>({ items: [], max: 32, max_pinned: 8 }) as unknown as Sig<SavedViewList>;
  return list;
}

/** loadSaved asks the laptop for the list again. */
export function loadSaved(): void {
  H.link.request("view.list").then((r) => (saved().value = readSavedList(r)), () => undefined);
}

/** startSaved hears ev views_saved and loads the list each time the link
 * comes back; once (the Home's init calls it). */
export function startSaved(): void {
  if (started) return;
  started = true;
  H.link.onEvent((kind, data) => {
    if (kind === "views_saved") saved().value = readSavedList(data);
  });
  H.effect(() => {
    if (H.live.value) loadSaved();
  });
}

/** failLine is the calm line for a refused change. */
function failLine(e: unknown, op: "save" | "pin" | "rename" | "delete"): string {
  if (H.isOffline(e)) return sv("err.offline");
  const k = refusal(H.code(e), op);
  return k ? sv(k) : H.errText(e);
}

/** SaveViewSheet is "Save as view": a name, "📌 Pin to Home", Cancel
 * (first) and Save. A name in use replaces that view, and says so. */
export function SaveViewSheet({ spec, title, onDone }: { spec: ViewSpec; title?: string; onDone: (id?: string) => void }) {
  const [name, setName] = H.useState(defaultName(spec, title));
  const [pin, setPin] = H.useState(false);
  const [busy, setBusy] = H.useState(false);
  const [err, setErr] = H.useState("");
  const input = H.useRef<HTMLInputElement | null>(null);
  H.useEffect(() => {
    input.current?.focus();
    input.current?.select();
  }, []);
  const l = saved().value;
  const same = sameName(l, name);
  const pinFits = canPin(l, same?.id);
  const ok = nameOK(name) && !busy && H.live.value;
  const send = () => {
    if (!ok) return;
    setBusy(true);
    setErr("");
    H.link.request("view.save", { spec, name: name.trim(), pin: pin && pinFits }).then(
      (r) => {
        setBusy(false);
        onDone(String((r as { id?: unknown } | null)?.id ?? ""));
      },
      (e: unknown) => {
        setBusy(false);
        setErr(failLine(e, "save"));
      },
    );
  };
  const Sheet = H.Sheet;
  return (
    <Sheet onLater={() => onDone()} label={sv("save.title")}>
      <form
        class="sv-form"
        onSubmit={(e) => {
          e.preventDefault();
          send();
        }}
      >
        <h2 class="sv-title">{sv("save.title")}</h2>
        <label class="field-label" for="sv-name">
          {sv("save.name")}
        </label>
        <input
          id="sv-name"
          ref={input}
          class="search-input name-input"
          dir="auto"
          autocomplete="off"
          maxLength={MAX_VIEW_NAME}
          value={name}
          onInput={(e) => setName((e.currentTarget as HTMLInputElement).value)}
        />
        {same && <p class="sv-note">{sv("save.replaces", { name: same.name })}</p>}
        <label class={`sv-check${pinFits ? "" : " is-off"}`}>
          <input type="checkbox" checked={pin && pinFits} disabled={!pinFits} onChange={(e) => setPin((e.currentTarget as HTMLInputElement).checked)} />
          <span>{sv("save.pin")}</span>
        </label>
        <p class="sv-note">{pinFits ? sv("save.pin_note") : sv("err.pins")}</p>
        <p class="sv-note">{sv("save.where")}</p>
        {err && (
          <p class="sv-err" role="alert">
            <span aria-hidden="true">✗ </span>
            {err}
          </p>
        )}
        <div class="choices">
          <button type="button" class="btn btn-neutral" onClick={() => onDone()}>
            {sv("cancel")}
          </button>
          <button type="submit" class="btn btn-primary" disabled={!ok}>
            {sv("save.do")}
          </button>
        </div>
      </form>
    </Sheet>
  );
}

type RowStep = { s: "idle" } | { s: "menu" } | { s: "rename" } | { s: "delete" };

/** SavedRow is one saved view: tap opens it; ⋯ shows pin, rename and
 * delete in the row. */
function SavedRow({ v, onOpen }: { v: SavedView; onOpen: (v: SavedView) => void }) {
  const [step, setStep] = H.useState<RowStep>({ s: "idle" });
  const [err, setErr] = H.useState("");
  const [name, setName] = H.useState(v.name);
  const safe = H.useRef<HTMLButtonElement | null>(null);
  H.useEffect(() => {
    if (step.s === "delete") safe.current?.focus();
  }, [step.s]);
  const l = saved().value;
  const act = (op: "view.pin" | "view.rename" | "view.delete", args: unknown, kind: "pin" | "rename" | "delete") => {
    setErr("");
    H.link.request(op, args).then(
      (r) => {
        saved().value = readSavedList(r);
        setStep({ s: "idle" });
      },
      (e: unknown) => setErr(failLine(e, kind)),
    );
  };
  const renameOK = nameOK(name) && name.trim() !== v.name && !sameName(l, name, v.id) && H.live.value;
  return (
    <li class="sv-row-wrap">
      <div class="sv-row">
        <button type="button" class="sv-open" onClick={() => onOpen(v)}>
          {v.pinned && (
            <span class="sv-pin" title={sv("row.pinned")}>
              <span aria-hidden="true">📌</span>
              <span class="sr-only">{sv("row.pinned")}</span>
            </span>
          )}
          <span class="sv-main">
            <span class="sv-name trunc" dir="auto">
              {v.name}
            </span>
            <span class="sv-sub trunc mono">{v.spec.view}</span>
          </span>
        </button>
        <button
          type="button"
          class="sv-more"
          aria-label={sv("row.actions", { name: v.name })}
          aria-expanded={step.s !== "idle"}
          onClick={() => setStep(step.s === "idle" ? { s: "menu" } : { s: "idle" })}
        >
          ⋯
        </button>
      </div>
      {step.s === "menu" && (
        <div class="sv-acts" role="group" aria-label={sv("row.actions", { name: v.name })}>
          <button type="button" class="btn btn-neutral" onClick={() => onOpen(v)}>
            {sv("row.open")}
          </button>
          <button
            type="button"
            class="btn btn-neutral"
            disabled={!H.live.value || (!v.pinned && !canPin(l))}
            onClick={() => act("view.pin", { id: v.id, on: !v.pinned }, "pin")}
          >
            {v.pinned ? sv("row.unpin") : sv("row.pin")}
          </button>
          <button
            type="button"
            class="btn btn-neutral"
            onClick={() => {
              setName(v.name);
              setStep({ s: "rename" });
            }}
          >
            {sv("row.rename")}
          </button>
          <button type="button" class="btn btn-neutral" onClick={() => setStep({ s: "delete" })}>
            {sv("row.delete")}
          </button>
        </div>
      )}
      {step.s === "rename" && (
        <form
          class="sv-acts"
          onSubmit={(e) => {
            e.preventDefault();
            if (renameOK) act("view.rename", { id: v.id, name: name.trim() }, "rename");
          }}
        >
          <label class="sr-only" for={`sv-rename-${v.id}`}>
            {sv("rename.title")}
          </label>
          <input
            id={`sv-rename-${v.id}`}
            class="search-input name-input"
            dir="auto"
            autocomplete="off"
            maxLength={MAX_VIEW_NAME}
            value={name}
            onInput={(e) => setName((e.currentTarget as HTMLInputElement).value)}
          />
          {sameName(l, name, v.id) && <p class="sv-note">{sv("err.name")}</p>}
          <div class="choices">
            <button type="button" class="btn btn-neutral" onClick={() => setStep({ s: "idle" })}>
              {sv("cancel")}
            </button>
            <button type="submit" class="btn btn-primary" disabled={!renameOK}>
              {sv("rename.do")}
            </button>
          </div>
        </form>
      )}
      {step.s === "delete" && (
        <div class="sv-acts" role="group" aria-label={sv("delete.ask", { name: v.name })} onKeyDown={(e) => e.key === "Escape" && setStep({ s: "idle" })}>
          <p class="sv-note">{sv("delete.ask", { name: v.name })}</p>
          <div class="choices">
            <button type="button" class="btn btn-neutral" ref={safe} onClick={() => setStep({ s: "idle" })}>
              {sv("cancel")}
            </button>
            <button type="button" class="btn btn-danger" disabled={!H.live.value} onClick={() => act("view.delete", { id: v.id }, "delete")}>
              {sv("delete.do")}
            </button>
          </div>
        </div>
      )}
      {err && (
        <p class="sv-err" role="alert">
          <span aria-hidden="true">✗ </span>
          {err}
        </p>
      )}
    </li>
  );
}

/** SavedViews is the Saved views list (Views menu, ☰): pinned first in
 * their Home order, then by name. onOpen runs the view again, live. */
export function SavedViews({ onOpen }: { onOpen: (v: SavedView) => void }) {
  const l = saved().value;
  if (l.items.length === 0) return <p class="sv-note sv-empty">{sv("list.empty")}</p>;
  return (
    <section class="sv-list" aria-label={sv("list.title")}>
      <h2 class="sv-title">{sv("list.title")}</h2>
      <p class="sv-note">{sv("list.count", { n: l.items.length, max: l.max, p: pinnedViews(l).length, pmax: l.max_pinned })}</p>
      <ul class="sv-rows">
        {l.items.map((v) => (
          <SavedRow key={v.id} v={v} onOpen={onOpen} />
        ))}
      </ul>
    </section>
  );
}

type Run = { s: "closed" } | { s: "loading" } | { s: "shown"; block: Record<string, unknown>; at: number } | { s: "failed"; why: string };

/** PinnedView is one pinned view on Home: collapsed to its name; opening
 * it runs the spec on the laptop, live (view.run), and closing it or
 * leaving Home stops it (view.stop). Offline, the last block stays,
 * marked with when it was seen. */
function PinnedView({ v }: { v: SavedView }) {
  const [run, setRun] = H.useState<Run>({ s: "closed" });
  const blockID = H.useRef("");
  const stop = () => {
    if (blockID.current && H.live.peek()) H.link.request("view.stop", { id: blockID.current }).catch(() => undefined);
    blockID.current = "";
  };
  const start = () => {
    setRun({ s: "loading" });
    H.link.request("view.run", { spec: v.spec, live: true }).then(
      (r) => {
        const b = (r ?? {}) as Record<string, unknown>;
        blockID.current = typeof b.id === "string" ? b.id : "";
        setRun({ s: "shown", block: b, at: Date.now() });
      },
      (e: unknown) => setRun({ s: "failed", why: H.code(e) === "not_found" || H.code(e) === "bad_request" ? sv("pinned.cannot") : H.errText(e) }),
    );
  };
  H.useEffect(() => {
    const off = H.link.onEvent((kind, data) => {
      if (kind !== "view_update" || typeof data !== "object" || data === null) return;
      const d = data as { id?: unknown; block?: unknown };
      if (d.id === blockID.current && blockID.current && typeof d.block === "object" && d.block !== null) {
        setRun({ s: "shown", block: d.block as Record<string, unknown>, at: Date.now() });
      }
    });
    return () => {
      off();
      stop();
    };
  }, []);
  // A spec saved again (here or on another phone) runs again.
  const specKey = JSON.stringify(v.spec);
  H.useEffect(() => {
    if (run.s === "shown" || run.s === "failed") {
      stop();
      start();
    }
  }, [specKey]);
  // Back online: the live view is gone with the old link, so run it again.
  const online = H.live.value;
  H.useEffect(() => {
    if (online && run.s === "shown") start();
  }, [online]);

  const open = run.s !== "closed";
  const Block = H.Block;
  return (
    <li class="sv-pinned">
      <button
        type="button"
        class="sv-pinned-head"
        aria-expanded={open}
        aria-label={open ? sv("pinned.collapse", { name: v.name }) : sv("pinned.expand", { name: v.name })}
        onClick={() => {
          if (open) {
            stop();
            setRun({ s: "closed" });
          } else start();
        }}
      >
        <span aria-hidden="true">📌</span>
        <span class="sv-name trunc" dir="auto">
          {v.name}
        </span>
        {run.s === "shown" && online && (
          <span class="sv-live">
            <span aria-hidden="true">● </span>
            {sv("pinned.live")}
          </span>
        )}
        <span aria-hidden="true" class="sv-caret">
          {open ? "▾" : "▸"}
        </span>
      </button>
      {run.s === "loading" && (
        <p class="sv-note" role="status">
          <span aria-hidden="true">◐ </span>
          {sv("pinned.loading")}
        </p>
      )}
      {run.s === "failed" && (
        <p class="sv-err" role="alert">
          <span aria-hidden="true">✗ </span>
          {run.why}{" "}
          <button type="button" class="btn btn-neutral btn-small" disabled={!online} onClick={start}>
            {sv("pinned.refresh")}
          </button>
        </p>
      )}
      {run.s === "shown" && (
        <div class={`sv-block${online ? "" : " is-stale"}`}>
          {!online && <p class="sv-note">{sv("pinned.stale", { age: new Date(run.at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) })}</p>}
          {Block ? <Block block={{ ...run.block, stale: !online }} /> : <p class="sv-note">{String(run.block.title ?? v.name)}</p>}
          <button type="button" class="btn btn-neutral btn-small sv-refresh" disabled={!online} onClick={start}>
            {sv("pinned.refresh")}
          </button>
        </div>
      )}
    </li>
  );
}

/** PinnedViews is Home's pinned section (MOBUX-4 draws it below the
 * cards): nothing when no view is pinned. */
export function PinnedViews() {
  const pins = pinnedViews(saved().value);
  if (pins.length === 0) return null;
  return (
    <section class="sv-pins" aria-label={sv("pinned.title")}>
      <ul class="sv-rows">
        {pins.map((v) => (
          <PinnedView key={v.id} v={v} />
        ))}
      </ul>
    </section>
  );
}

/** SaveButton is the "Save as view" action under a block that came from
 * a view (it carries its spec). */
export function SaveButton({ spec, title }: { spec: ViewSpec; title?: string }) {
  const [open, setOpen] = H.useState(false);
  return (
    <span class="sv-save">
      <button type="button" class="btn btn-neutral btn-small" disabled={!H.live.value} onClick={() => setOpen(true)}>
        {sv("save")}
      </button>
      {open && <SaveViewSheet spec={spec} title={title} onDone={() => setOpen(false)} />}
    </span>
  );
}
