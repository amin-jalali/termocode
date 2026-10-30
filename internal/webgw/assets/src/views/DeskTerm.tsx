// The desktop's terminal panel: the laptop's shells as an integrated
// terminal. You type straight into it: every key (Ctrl, Alt, the arrows,
// Tab) goes to the shell as a terminal sends it (lib/termkeys.ts), the
// workbench's own shortcuts excepted. The shell keeps the laptop's width;
// its rows are joined where the laptop wrapped them and wrap again at the
// panel's width (the view follows the panel, the shell does not). A
// cursor sits after the last row's text, colours come from the laptop's
// screen, older rows page in from the top. Several shells: a list on the
// right; Split shows two side by side; Kill asks the laptop. Text is
// selected and copied as on any page; a paste of several lines asks
// first. The phone keeps its own view with the key bar (Term.tsx).
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import type { TermItem, TermNewRes } from "../../../../webproto/proto";
import { rang } from "../lib/attention";
import { isMac } from "../lib/platform";
import { route } from "../lib/router";
import { loadingOlder, loadOlder, older, stitch } from "../lib/term";
import { colorsOn, paintLine, termColors, wantColors, type TermSeg } from "../lib/termcolors";
import { keyBytes, pasteLines, pasteText, workbenchKey } from "../lib/termkeys";
import { toast } from "../lib/toasts";
import { OfflineError, ReqError } from "../net/link";
import { link, live, store } from "../session";
import { isGone } from "../state/files";
import { readTerm, readTerms, reflow } from "../state/term";
import { t } from "../strings";
import { AskLaptop } from "../ui/AskLaptop";
import { Button } from "../ui/Button";
import { errText } from "../ui/CardSheet";
import { Cod } from "../ui/icons";
import { Sheet } from "../ui/Sheet";
import { SkeletonRows } from "../ui/Skeleton";
import { RowMenu, type MenuItem } from "../ui/RowMenu";
import { Refused } from "./Placeholder";
import { About, asRows, Screen, wrapRows } from "./Term";
import { MAX_PASTE, PasteSheet, pasting } from "./TermInput";

/** NEAR_BOTTOM_PX: closer than this to the end, new output keeps the view at the end. */
const NEAR_BOTTOM_PX = 24;
/** READ_ROWS is how many rows "Read the last rows" reads. */
const READ_ROWS = 20;
/** RING_MS is how long the bell dot shows after a shell rang. */
const RING_MS = 3000;
/** MAX_PANES: at most this many shells side by side. */
const MAX_PANES = 4;

/** panes are the shells shown side by side (a split group). */
export const panes = signal<number[]>([]);
/** focusBuf is the shell the keys go to. */
export const focusBuf = signal<number | null>(null);

/** show makes buf the shown and focused shell (its group stays when it is in one). */
function show(buf: number): void {
  if (!panes.peek().includes(buf)) panes.value = [buf];
  focusBuf.value = buf;
}

function failed(e: unknown): void {
  if (e instanceof ReqError && !(e instanceof OfflineError)) toast("warn", errText(e));
}

/** newDesk starts a shell on the laptop; split puts it beside the focused one. */
export function newDesk(split: boolean): void {
  link.request("term.new").then((data) => {
    const buf = (data as TermNewRes).buf;
    if (split) {
      const cur = panes.peek();
      panes.value = cur.includes(buf) ? cur : [...cur, buf].slice(-MAX_PANES);
      focusBuf.value = buf;
    } else show(buf);
  }, failed);
}

function send(buf: number, text: string): void {
  if (!live.peek() || !text) return;
  link.request("term.send", { buf, text }).catch((e: unknown) => {
    if (!(e instanceof OfflineError)) toast("warn", errText(e));
  });
}

function copy(text: string): void {
  const fail = () => toast("warn", t("copy.failed"));
  try {
    navigator.clipboard.writeText(text).then(() => toast("ok", t("copied")), fail);
  } catch {
    fail();
  }
}

/** selectedIn is the page's selected text when it lies in el ("" none). */
function selectedIn(el: HTMLElement | null): string {
  const s = getSelection();
  if (!el || !s || s.isCollapsed || !s.anchorNode || !el.contains(s.anchorNode)) return "";
  return s.toString();
}

function pageBackground(): string {
  const v = getComputedStyle(document.documentElement).getPropertyValue("--wb-panel-bg").trim().toLowerCase();
  return /^#[0-9a-f]{6}$/.test(v) ? v : "#1e1e1e";
}

/** Row is one terminal line; the cursor sits after the text of its row. */
function Row({ text, segs, cursor }: { text: string; segs: TermSeg[] | null; cursor: boolean }) {
  return (
    <div class="trow" dir="ltr">
      <span class="trow-text">
        {segs ? segs.map((g, k) => (g.look ? <span key={k} class="tc" style={g.look}>{g.text}</span> : g.text)) : text}
        {cursor && <span class="dterm-cursor" aria-hidden="true" />}
      </span>
    </div>
  );
}

function PasteAsk({ text, onPaste, onClose }: { text: string; onPaste: () => void; onClose: () => void }) {
  return (
    <Sheet onLater={onClose} label={t("key.paste")}>
      <section class="ask dterm-paste" role="dialog" aria-modal="true" aria-labelledby="dterm-paste-t">
        <p class="ask-text" id="dterm-paste-t">
          {t("dterm.paste_ask", { n: pasteLines(text) })}
        </p>
        <pre class="dterm-paste-text" dir="ltr">
          {text.length > 2000 ? text.slice(0, 2000) + "…" : text}
        </pre>
        <div class="choices">
          <button type="button" class="btn btn-neutral" autoFocus onClick={onClose}>
            {t("cancel")}
          </button>
          <button type="button" class="btn btn-primary" onClick={onPaste}>
            {t("key.paste")}
          </button>
        </div>
      </section>
    </Sheet>
  );
}

/** menuItems is what a right click on a row offers. */
function menuItems(m: { row: string; sel: string }, all: string, canPaste: boolean): MenuItem[] {
  const items: MenuItem[] = [];
  if (m.sel) items.push({ label: t("dterm.copy_sel"), pick: () => copy(m.sel) });
  items.push({ label: t("term.copy_row"), pick: () => copy(m.row) }, { label: t("term.copy_all"), pick: () => copy(all) });
  if (canPaste) items.push({ label: t("term.paste"), pick: () => (pasting.value = true) });
  return items;
}

/** Pane is one shell: its rows, the cursor, and the hidden input that
 * takes the keys. */
function Pane({ buf, item, n, focused, controller }: { buf: number; item: TermItem | undefined; n: number; focused: boolean; controller: boolean }) {
  const topic = "term:" + buf;
  const ts = store.topic(topic).value;
  const snap = readTerm(ts?.data);
  const root = useRef<HTMLDivElement | null>(null);
  const scroll = useRef<HTMLDivElement | null>(null);
  const input = useRef<HTMLTextAreaElement | null>(null);
  const stick = useRef(true);
  const keep = useRef<number | null>(null);
  const prev = useRef<{ first: number; rows: readonly string[] } | null>(null);
  const [pinned, setPinned] = useState(true);
  const [ask, setAsk] = useState<string | null>(null);
  const [said, setSaid] = useState("");
  const [menu, setMenu] = useState<{ row: string; sel: string } | null>(null);
  const gone = isGone(ts?.data);
  const ended = gone || item?.exited === true;
  const canType = controller && live.value && !ended;

  useEffect(() => {
    link.subscribe(topic);
    return () => link.unsubscribe(topic);
  }, [buf]);
  useEffect(() => {
    if (focused) input.current?.focus({ preventScroll: true });
  }, [focused, !!snap]);
  // Rows the window drops from its top stay joined to the paged ones.
  useEffect(() => {
    if (!snap) return;
    const p = prev.current;
    if (p && snap.first !== p.first) stitch(buf, p.first, p.rows, snap.first);
    prev.current = { first: snap.first, rows: snap.rows };
  }, [snap?.first, snap?.rows]);
  useEffect(() => {
    const el = scroll.current;
    if (!el) return;
    const onScroll = () => {
      const at = el.scrollHeight - el.scrollTop - el.clientHeight < NEAR_BOTTOM_PX;
      stick.current = at;
      setPinned(at);
    };
    el.addEventListener("scroll", onScroll, { passive: true });
    return () => el.removeEventListener("scroll", onScroll);
  }, [!!snap]);
  useLayoutEffect(() => {
    const el = scroll.current;
    if (!el) return;
    if (keep.current !== null) {
      el.scrollTop += el.scrollHeight - keep.current;
      keep.current = null;
    } else if (stick.current) el.scrollTop = el.scrollHeight;
  });
  // Colours of the focused shell's rows, once output settles.
  const on = colorsOn.value;
  useEffect(() => {
    if (!snap || !on || !focused || snap.screen) return;
    wantColors(buf, snap.tick, snap.first, () => {
      const s = readTerm(store.topic(topic).peek()?.data);
      return s && { first: s.first, rows: s.rows, tick: s.tick };
    });
  }, [snap?.tick, on, focused]);

  const toEnd = () => {
    stick.current = true;
    setPinned(true);
    const el = scroll.current;
    if (el) el.scrollTop = el.scrollHeight;
  };
  const type = (text: string) => {
    if (!canType) return;
    toEnd();
    send(buf, text);
  };
  const paste = (text: string) => {
    if (!canType || !text) return;
    if (new TextEncoder().encode(text).length > MAX_PASTE) {
      toast("warn", t("err.limit"));
      return;
    }
    if (pasteLines(text) > 1) setAsk(text);
    else type(pasteText(text));
  };

  const onKeyDown = (e: KeyboardEvent) => {
    // Keys of a sheet or a button inside the pane are theirs.
    if (e.target !== input.current && e.target !== root.current) return;
    if (e.isComposing || e.keyCode === 229) return;
    const mac = isMac();
    if (workbenchKey(e, mac)) return; // the workbench's own shortcut
    const typing = e.target === input.current;
    const k = e.key;
    if (e.ctrlKey && e.shiftKey && !e.altKey && (e.code === "Backquote" || e.code === "Digit5")) {
      e.preventDefault();
      e.stopPropagation();
      if (controller) newDesk(e.code === "Digit5");
      return;
    }
    const sel = selectedIn(root.current);
    if ((e.ctrlKey && e.shiftKey && e.code === "KeyC") || (!mac && e.ctrlKey && !e.shiftKey && !e.altKey && e.code === "KeyC" && sel)) {
      e.preventDefault();
      e.stopPropagation();
      if (sel) copy(sel);
      return;
    }
    if ((mac ? e.metaKey : e.ctrlKey) && !e.altKey && e.code === "KeyV") {
      // The browser's own paste: it lands in the input (onPaste).
      e.stopPropagation();
      input.current?.focus({ preventScroll: true });
      return;
    }
    if (e.shiftKey && !e.ctrlKey && !e.altKey && !e.metaKey && (k === "PageUp" || k === "PageDown")) {
      e.preventDefault();
      const el = scroll.current;
      if (el) el.scrollTop += (k === "PageUp" ? -1 : 1) * Math.max(40, el.clientHeight - 20);
      return;
    }
    const bytes = keyBytes(e, mac);
    if (bytes === null) {
      // A character typed while the rows had the focus (after a
      // selection): it goes to the shell, the input takes the next ones.
      if (!typing && k.length === 1 && !e.ctrlKey && !e.metaKey && canType) {
        e.preventDefault();
        type(k);
        input.current?.focus({ preventScroll: true });
      }
      return;
    }
    e.preventDefault();
    e.stopPropagation();
    type(bytes);
  };

  // What the input took by typing: Enter already went from its key
  // (onKeyDown), and a paste goes through onPaste, so a line break here
  // is the same Enter again and is dropped.
  const takeInput = (el: HTMLTextAreaElement) => {
    const v = el.value.replace(/[\r\n]/g, "");
    el.value = "";
    if (v) type(v);
  };

  if (!snap) {
    const r = link.refused.value.get(topic);
    return (
      <div class="dterm-pane" data-buf={buf}>
        {r ? <Refused topic={topic}>{r.msg}</Refused> : <SkeletonRows rows={6} />}
      </div>
    );
  }
  const o = older.value;
  const joined = o && o.buf === buf && o.first + o.rows.length === snap.first;
  const first = joined ? o.first : snap.first;
  const all = joined ? [...o.rows, ...snap.rows] : snap.rows;
  const lines = reflow(all, first, snap.cols);
  const colors = on && focused ? termColors.value : null;
  const pageBg = colors ? pageBackground() : "";
  const more = joined ? o.more : snap.more;
  let cur = lines.length - 1;
  while (cur > 0 && lines[cur].text.trim() === "") cur--;
  const label = item?.task || item?.title || String(n);
  return (
    <div
      class={`dterm-pane${focused ? " is-focus" : ""}${ts?.stale ? " is-stale" : ""}`}
      data-buf={buf}
      ref={root}
      tabIndex={-1}
      onKeyDown={onKeyDown}
      onPaste={(e: ClipboardEvent) => {
        if (e.target !== input.current && e.target !== root.current) return;
        e.preventDefault();
        paste(e.clipboardData?.getData("text/plain") ?? "");
      }}
      onMouseDown={() => {
        if (!focused) focusBuf.value = buf;
      }}
      onMouseUp={() => {
        if (!selectedIn(root.current)) input.current?.focus({ preventScroll: true });
      }}
      onContextMenu={(e: MouseEvent) => {
        const row = (e.target as Element).closest(".trow");
        if (!row) return;
        e.preventDefault();
        setMenu({ row: row.querySelector(".trow-text")?.textContent ?? "", sel: selectedIn(root.current) });
      }}
    >
      <div class="dterm-scroll" ref={scroll}>
        {more && (
          <button
            type="button"
            class="term-older"
            disabled={loadingOlder.value}
            onClick={() => {
              keep.current = scroll.current?.scrollHeight ?? null;
              stick.current = false;
              loadOlder(buf, first);
            }}
          >
            {t("term.older")}
          </button>
        )}
        <button type="button" class="sr-focus term-read" onClick={() => setSaid(lines.slice(-READ_ROWS).map((l) => l.text).join("\n"))}>
          {t("term.read_last")}
        </button>
        <div class="sr-only term-said" role="status">
          {said}
        </div>
        {snap.screen && !asRows.value.has(buf) ? (
          <Screen buf={buf} rows={snap.rows} cols={snap.cols} height={snap.height} />
        ) : (
          <div class={`term-rows dterm-rows${wrapRows.value ? "" : " is-nowrap"}`} role="log" aria-live="off" aria-label={label}>
            {lines.map((l, i) => (
              <Row
                key={l.row}
                text={l.text}
                cursor={i === cur && canType}
                segs={colors ? paintLine(colors, buf, l.row, all.slice(l.row - first, (lines[i + 1]?.row ?? first + all.length) - first), pageBg) : null}
              />
            ))}
          </div>
        )}
        {ended && (
          <p class="file-note term-ended" role="status">
            <span>{t(gone ? "term.gone" : "term.ended_now")}</span>
            {controller && (
              <Button kind="neutral" small needsLink onClick={() => newDesk(false)}>
                {t("term.new")}
              </Button>
            )}
          </p>
        )}
      </div>
      {controller && !ended && (
        <textarea
          ref={input}
          class="dterm-input"
          dir="ltr"
          rows={1}
          spellcheck={false}
          autocomplete="off"
          autocapitalize="off"
          aria-label={t("dterm.input", { name: label })}
          aria-disabled={canType ? undefined : "true"}
          onFocus={() => (focusBuf.value = buf)}
          onInput={(e) => {
            if (!(e as InputEvent).isComposing) takeInput(e.currentTarget as HTMLTextAreaElement);
          }}
          onCompositionEnd={(e) => takeInput(e.currentTarget as HTMLTextAreaElement)}
        />
      )}
      {!pinned && (
        <button type="button" class="dterm-follow tb" onClick={toEnd}>
          ↓ {t("term.follow")}
        </button>
      )}
      {menu && (
        <RowMenu title={menu.row || label} items={menuItems(menu, lines.map((l) => l.text).join("\n"), canType)} onClose={() => setMenu(null)} />
      )}
      {pasting.value && focused && canType && <PasteSheet buf={buf} onClose={() => (pasting.value = false)} />}
      {ask !== null && (
        <PasteAsk
          text={ask}
          onClose={() => {
            setAsk(null);
            input.current?.focus({ preventScroll: true });
          }}
          onPaste={() => {
            type(pasteText(ask));
            setAsk(null);
            input.current?.focus({ preventScroll: true });
          }}
        />
      )}
    </div>
  );
}

/** List is the shells on the right (two or more): a click shows one. */
function List({ terms, shown, focus, onKill }: { terms: TermItem[]; shown: readonly number[]; focus: number | null; onKill: (x: TermItem) => void }) {
  return (
    <ul class="dterm-list" aria-label={t("dterm.list")}>
      {terms.map((x) => {
        const name = x.task || x.title;
        const on = shown.includes(x.buf);
        return (
          <li key={x.buf} class={`dterm-tab${on ? " is-shown" : ""}${x.buf === focus ? " is-on" : ""}`} data-buf={x.buf}>
            <button type="button" class="dterm-tab-open" aria-current={x.buf === focus ? "true" : undefined} onClick={() => show(x.buf)}>
              <Cod name="terminal" />
              <span class="trunc" dir="auto">
                {name}
              </span>
              {x.busy && <span class="chip-dot dot-busy" aria-hidden="true" />}
              {x.exited && <span class="chip-dot dot-warn" aria-hidden="true" />}
            </button>
            <button type="button" class="tb tb-icon dterm-tab-kill" aria-label={t("dterm.kill_one", { name })} title={t("dterm.kill_one", { name })} onClick={() => onKill(x)}>
              <Cod name="trash" />
            </button>
          </li>
        );
      })}
    </ul>
  );
}

export function DeskTerm() {
  useEffect(() => link.subscribe("terms"), []);
  const terms = readTerms(store.topic("terms").value?.data);
  const controller = link.meta.value?.role === "controller";
  const [kill, setKill] = useState<TermItem | null>(null);
  const [about, setAbout] = useState(false);
  const [ring, setRing] = useState(false);
  const bells = rang.value;
  useEffect(() => {
    if (bells === 0) return;
    setRing(true);
    const timer = setTimeout(() => setRing(false), RING_MS);
    return () => clearTimeout(timer);
  }, [bells]);
  // A terminal route (#/term/9) shows that shell.
  const r = route.value;
  const want = r.tab === "term" && /^\d+$/.test(r.rest) ? Number(r.rest) : NaN;
  useEffect(() => {
    if (Number.isInteger(want)) show(want);
  }, [want]);

  const has = (b: number) => terms.some((x) => x.buf === b);
  let shown = panes.value.filter(has);
  if (shown.length === 0) {
    const first = (terms.find((x) => x.active) ?? terms[0])?.buf;
    shown = first === undefined ? [] : [first];
  }
  const focus = focusBuf.value !== null && shown.includes(focusBuf.value) ? focusBuf.value : shown[0] ?? null;
  const focusItem = terms.find((x) => x.buf === focus);
  const st = store.topic("terms").value;

  return (
    <section class="view dterm" data-view="term" aria-label={t("tab.term")}>
      <div class="dterm-actions" role="toolbar" aria-label={t("tab.term")}>
        {ring && <span class="bell-dot" role="status" aria-label={t("term.bell")} />}
        {focus !== null && (
          <button type="button" class="tb tb-icon head-btn term-info-btn" aria-label={t("term.about")} title={t("term.about")} onClick={() => setAbout(true)}>
            ⓘ
          </button>
        )}
        {controller && (
          <>
            <button type="button" class="tb tb-icon dterm-new" aria-label={t("dterm.new")} title={`${t("dterm.new")} (Ctrl+Shift+\`)`} disabled={!live.value} onClick={() => newDesk(false)}>
              <Cod name="add" />
            </button>
            <button type="button" class="tb tb-icon dterm-split" aria-label={t("dterm.split")} title={`${t("dterm.split")} (Ctrl+Shift+5)`} disabled={!live.value || focus === null} onClick={() => newDesk(true)}>
              <Cod name="split-horizontal" />
            </button>
            <button type="button" class="tb tb-icon dterm-kill" aria-label={t("dterm.kill")} title={t("dterm.kill")} disabled={!live.value || !focusItem} onClick={() => focusItem && setKill(focusItem)}>
              <Cod name="trash" />
            </button>
          </>
        )}
      </div>
      {!st?.data ? (
        <SkeletonRows rows={6} />
      ) : terms.length === 0 ? (
        <div class="empty-state dterm-none">
          <p>{t("term.none")}</p>
          {controller && (
            <Button kind="neutral" needsLink onClick={() => newDesk(false)}>
              {t("term.open_one")}
            </Button>
          )}
        </div>
      ) : (
        <div class="dterm-body">
          <div class="dterm-panes">
            {shown.map((b) => (
              <Pane key={b} buf={b} n={terms.findIndex((x) => x.buf === b) + 1} item={terms.find((x) => x.buf === b)} focused={b === focus} controller={controller} />
            ))}
          </div>
          {(terms.length > 1 || shown.length > 1) && <List terms={terms} shown={shown} focus={focus} onKill={setKill} />}
        </div>
      )}
      {!controller && st?.data !== undefined && <span class="chip dterm-viewonly">{t("viewonly")}</span>}
      {about && focus !== null && (
        <About
          buf={focus}
          cols={readTerm(store.topic("term:" + focus).value?.data)?.cols ?? 0}
          screen={readTerm(store.topic("term:" + focus).value?.data)?.screen === true}
          onClose={() => setAbout(false)}
        />
      )}
      {kill && (
        <AskLaptop
          label={t("term.close")}
          text={t("term.close_body", { n: terms.findIndex((x) => x.buf === kill.buf) + 1 })}
          send={() => link.request("term.close", { buf: kill.buf })}
          onDone={() => setKill(null)}
          onConfirmed={() => (panes.value = panes.peek().filter((b) => b !== kill.buf))}
        />
      )}
    </section>
  );
}
