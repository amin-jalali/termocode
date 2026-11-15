// The phone's terminal (PHONE-TERM, owner 2026-11-15: "emulate the
// terminal exactly"): the proven mobile terminal pattern. The terminal
// fills the screen between the status bar and the key bar; a tap on it
// opens the phone keyboard and every key goes straight to the shell
// (the terminal engine, term.js, takes the typing, input methods and
// paste; termtype.ts turns it into bytes). No command box. The key bar
// sits right above the keyboard (Esc, Tab, sticky Ctrl and Alt, the
// arrows, and more to the side) and the tab bar hides while the
// keyboard is up (lib/viewport.ts). The shells are one slim strip of
// tabs with ⓘ and + at its end; a long press on a line offers Copy.
// The laptop's shell keeps its width: its rows are joined where the
// laptop wrapped them and wrap again at the phone's width. A full-screen
// program shows as the laptop's grid (Screen).
import { Fragment, h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { TermItem } from "../../../../../webproto/proto";
import { rang } from "../../lib/attention";
import { copyText as copy } from "../../lib/clip";
import { linkItems } from "../../lib/links";
import { useLongPress } from "../../lib/longpress";
import { prefs } from "../../lib/prefs";
import { go, route } from "../../lib/router";
import { rowPx, usePinch } from "../../lib/rowfont";
import { dropOlder, loadingOlder, loadOlder, older, stitch } from "../../lib/term";
import { colorsOn, dropColors, paintLine, termColors, wantColors } from "../../lib/termcolors";
import { tactile, toTactile } from "../../lib/tactile";
import { useWakeLock } from "../../lib/wakelock";
import { loadTerm, type TermModule } from "../../lib/xterm";
import { barKeys } from "../../lib/keys";
import { link, live, store } from "../../session";
import { isGone } from "../../state/files";
import { readTerm, readTerms, reflow, type Line } from "../../state/term";
import { t } from "../../strings";
import { Button } from "../../ui/Button";
import { Chip } from "../../ui/Chip";
import { RowMenu } from "../../ui/RowMenu";
import { Sheet } from "../../ui/Sheet";
import { SkeletonRows } from "../../ui/Skeleton";
import { KeysSheet } from "../KeysSheet";
import { Refused } from "../Placeholder";
import { About, asRows, newTerm, Screen } from "../Term";
import { KEYS, MAX_PASTE, PasteSheet, pasting } from "../TermInput";
import { pressTermKey, setTermInputMode, termInputMode, typeBuf, typeTerm } from "./termtype";

type XTerm = ReturnType<TermModule["open"]>;

/** cur is the shown shell's engine (Follow scrolls it to the end). */
const cur: { current: XTerm | null } = { current: null };

/** READ_ROWS is how many rows "Read the last rows" reads. */
const READ_ROWS = 20;
/** RING_MS is how long the bell dot shows after a shell rang. */
const RING_MS = 3000;

function cssVar(name: string, or: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || or;
}

/** look is the terminal's look from the page's theme. */
function look(el: HTMLElement) {
  const bg = cssVar("--bg-panel", "#1e1e1e");
  const fg = cssVar("--text", "#cccccc");
  return {
    fontFamily: getComputedStyle(el).fontFamily || "monospace",
    fontSize: rowPx(),
    lineHeight: 1.25,
    theme: { background: bg, foreground: fg, cursor: fg, cursorAccent: bg, selectionBackground: cssVar("--sel", "#264f78") },
  };
}

function showTerm(buf: number): void {
  go({ tab: "term", rest: String(buf), query: {} });
}

/** Strip is the shells as one slim line of tabs, ⓘ and + at its end. */
function Strip({ terms, cur, controller, info, ring }: { terms: TermItem[]; cur: number | undefined; controller: boolean; info: (() => void) | null; ring: boolean }) {
  return (
    <div class="pterm-strip">
      <div class="term-chips" role="tablist" aria-label={t("tab.term")}>
        {terms.map((x, i) => (
          <span key={x.buf} class="term-chip" data-buf={x.buf}>
            <Chip tab on={x.buf === cur} dot={x.busy ? "busy" : x.exited ? "warn" : undefined} onClick={() => showTerm(x.buf)}>
              <span class="trunc term-chip-title" dir="auto">
                {x.task || x.title}
              </span>
              <span class="term-chip-n">{i + 1}</span>
            </Chip>
          </span>
        ))}
      </div>
      {ring && <span class="bell-dot" role="status" aria-label={t("term.bell")} />}
      {info && (
        <button type="button" class="head-btn term-info-btn" aria-label={t("term.about")} onClick={info}>
          ⓘ
        </button>
      )}
      {controller && terms.length > 0 && (
        <Button kind="quiet" small needsLink label={t("term.new")} onClick={newTerm}>
          +
        </Button>
      )}
    </div>
  );
}

/** PasteAsk asks before a paste of several lines goes to the shell. */
function PasteAsk({ text, onClose }: { text: string; onClose: () => void }) {
  const n = text.replace(/(\r\n?|\n)$/, "").split(/\r\n?|\n/).length;
  return (
    <Sheet onLater={onClose} label={t("key.paste")}>
      <section class="ask pterm-paste" role="dialog" aria-modal="true" aria-labelledby="pterm-paste-t">
        <p class="ask-text" id="pterm-paste-t">
          {t("term.paste.many", { n })}
        </p>
        <pre class="dterm-paste-text" dir="ltr">
          {text.length > 2000 ? text.slice(0, 2000) + "…" : text}
        </pre>
        <div class="choices">
          <button type="button" class="btn btn-neutral" onClick={onClose}>
            {t("cancel")}
          </button>
          <button
            type="button"
            class="btn btn-primary"
            onClick={() => {
              typeTerm(text.replace(/\r\n?|\n/g, "\r"));
              onClose();
            }}
          >
            {t("key.paste")}
          </button>
        </div>
      </section>
    </Sheet>
  );
}

/** Rows is one shell in the terminal engine. */
function Rows({ buf, item, canType, onEnd }: { buf: number; item: TermItem | undefined; canType: boolean; onEnd: (at: boolean) => void }) {
  const topic = "term:" + buf;
  const ts = store.topic(topic).value;
  const snap = readTerm(ts?.data);
  const host = useRef<HTMLDivElement | null>(null);
  const xt = useRef<XTerm | null>(null);
  const prev = useRef<{ first: number; rows: readonly string[] } | null>(null);
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);
  const [menu, setMenu] = useState<string | null>(null);
  const [ask, setAsk] = useState<string | null>(null);
  const [said, setSaid] = useState("");
  const lastY = useRef(0);

  useEffect(() => {
    dropOlder();
    dropColors();
  }, [buf]);
  // Rows the window drops from its top stay joined to the paged ones.
  useEffect(() => {
    if (!snap) return;
    const p = prev.current;
    if (p && snap.first !== p.first) stitch(buf, p.first, p.rows, snap.first);
    prev.current = { first: snap.first, rows: snap.rows };
  }, [snap?.first, snap?.rows]);

  const screen = !!snap?.screen && !asRows.value.has(buf);
  useEffect(() => {
    const el = host.current;
    if (!el || screen || !snap) return;
    let gone = false;
    loadTerm().then(
      (m) => {
        if (gone || !host.current) return;
        xt.current = m.open(host.current, {
          ...look(host.current),
          label: item?.title || t("tab.term"),
          onData: typeTerm,
          onPaste: (text) => {
            if (new TextEncoder().encode(text).length > MAX_PASTE) setAsk(null);
            else setAsk(text);
          },
          onScroll: onEnd,
        });
        cur.current = xt.current;
        setReady(true);
      },
      () => setFailed(true),
    );
    return () => {
      gone = true;
      if (cur.current === xt.current) cur.current = null;
      xt.current?.dispose();
      xt.current = null;
      setReady(false);
    };
  }, [buf, screen, !!snap]);

  // Colours of the rows on the laptop's screen, once output settles.
  const on = colorsOn.value;
  useEffect(() => {
    if (!snap || !on || screen) return;
    wantColors(buf, snap.tick, snap.first, () => {
      const s = readTerm(store.topic(topic).peek()?.data);
      return s && { first: s.first, rows: s.rows, tick: s.tick };
    });
  }, [snap?.tick, on]);
  useEffect(() => () => dropColors(), []);

  const o = older.value;
  const joined = !!snap && !!o && o.buf === buf && o.first + o.rows.length === snap.first;
  const first = snap ? (joined ? o!.first : snap.first) : 0;
  const all = snap ? (joined ? [...o!.rows, ...snap.rows] : snap.rows) : [];
  const lines: Line[] = snap ? reflow(all, first, snap.cols) : [];
  const colors = on ? termColors.value : null;
  // Draw what changed.
  useEffect(() => {
    const x = xt.current;
    if (!x || !snap) return;
    const bg = cssVar("--bg-panel", "#1e1e1e");
    x.show(
      lines.map((l, i) => ({
        row: l.row,
        text: l.text,
        segs: colors ? paintLine(colors, buf, l.row, all.slice(l.row - first, (lines[i + 1]?.row ?? first + all.length) - first), bg) : null,
      })),
      canType,
    );
  });
  useEffect(() => xt.current?.setInputMode(termInputMode.value), [ready, termInputMode.value]);
  // On a touch device the Tactile Code keyboard types into the shell
  // (pressTermKey) instead of the system keyboard (TK-2).
  useEffect(() => {
    const input = ready ? host.current?.querySelector("textarea") : null;
    if (!input) return;
    return tactile(input, { key: (e) => void pressTermKey(e), pad: "arrows", inputMode: setTermInputMode });
  }, [ready, buf]);
  // The text size follows the pinch and the ⓘ sheet's buttons.
  const px = prefs.value.rowPx;
  useEffect(() => {
    if (xt.current && host.current) xt.current.setLook(look(host.current));
  }, [px, ready]);
  useLongPress(host, () => setMenu(xt.current?.lineAt(lastY.current) ?? ""));

  if (!snap) {
    const r = link.refused.value.get(topic);
    if (r) return <Refused topic={topic}>{r.msg}</Refused>;
    return <SkeletonRows rows={10} />;
  }
  const more = joined ? o!.more : snap.more;
  const gone = isGone(ts?.data);
  return (
    <div class={`topic term pterm${ts?.stale ? " is-stale" : ""}`}>
      {more && !screen && (
        <button type="button" class="term-older" disabled={loadingOlder.value} onClick={() => loadOlder(buf, first)}>
          {t("term.older")}
        </button>
      )}
      {/* A screen reader hears the rows only when asked (web UX 8.1). */}
      <button type="button" class="sr-focus term-read" onClick={() => setSaid(lines.slice(-READ_ROWS).map((l) => l.text).join("\n"))}>
        {t("term.read_last")}
      </button>
      <div class="sr-only term-said" role="status">
        {said}
      </div>
      {screen ? (
        <Screen buf={buf} rows={snap.rows} cols={snap.cols} height={snap.height} />
      ) : (
        <div
          class="pterm-x"
          ref={host}
          dir="ltr"
          role="log"
          aria-label={item?.title || t("tab.term")}
          data-ready={ready ? "1" : undefined}
          onTouchStart={(e: TouchEvent) => (lastY.current = e.touches[0]?.clientY ?? 0)}
          onMouseDown={(e: MouseEvent) => (lastY.current = e.clientY)}
        >
          {failed && (
            <Button kind="neutral" small onClick={() => setFailed(false)}>
              {t("retry")}
            </Button>
          )}
        </div>
      )}
      {(gone || item?.exited) && (
        <p class="file-note term-ended" role="status">
          <span>{t(gone ? "term.gone" : "term.ended_now")}</span>
          <Button kind="neutral" small needsLink onClick={newTerm}>
            {t("term.new")}
          </Button>
        </p>
      )}
      {menu !== null && (
        <RowMenu
          title={menu || t("tab.term")}
          items={[
            ...(xt.current?.selection() ? [{ label: t("term.copy_row"), pick: () => copy(xt.current?.selection() ?? "") }] : []),
            { label: t("term.copy_row"), pick: () => copy(menu) },
            { label: t("term.copy_all"), pick: () => copy(xt.current?.text() ?? lines.map((l) => l.text).join("\n")) },
            ...(canType ? [{ label: t("term.paste"), pick: () => (pasting.value = true) }] : []),
            ...linkItems(menu),
          ]}
          onClose={() => setMenu(null)}
        />
      )}
      {ask !== null && <PasteAsk text={ask} onClose={() => setAsk(null)} />}
    </div>
  );
}

/** KeyBar is the extra keys above the keyboard. A tap never takes the
 * focus from the terminal, so the keyboard stays open. */
function KeyBar({ buf, off }: { buf: number; off: boolean }) {
  const [editing, setEditing] = useState(false);
  const keep = (e: Event) => e.preventDefault();
  return (
    <div class={`composer term-input pterm-keys${off ? " is-off" : ""}`}>
      {/* tactile.css shows it, pinned before the keys, while the system keyboard is up. */}
      <button type="button" class="key key-tk" hidden aria-label={t("key.tk")} title={t("key.tk")} onPointerDown={keep} onMouseDown={keep} onClick={toTactile}>
        ⌨
      </button>
      <div class="keybar" role="toolbar" aria-label={t("tab.term")}>
        {!live.value && <span class="chip keybar-off">{t("offline.input")}</span>}
        {barKeys(prefs.value.keys).map((id) => {
          const k = KEYS[id];
          const m = k.mod?.value ?? 0;
          return (
            <button
              key={id}
              type="button"
              class={`key${k.cls ? " " + k.cls : ""}${m === 1 ? " is-on" : ""}${m === 2 ? " is-locked" : ""}`}
              aria-label={k.aria ? t(k.aria) : undefined}
              aria-pressed={k.mod ? (m > 0 ? "true" : "false") : undefined}
              aria-disabled={off ? "true" : undefined}
              onMouseDown={keep}
              onClick={() => {
                if (!off) k.press(buf);
              }}
            >
              {t(k.label)}
            </button>
          );
        })}
        <button type="button" class="key key-edit" aria-label={t("key.edit.label")} onClick={() => setEditing(true)}>
          {t("key.edit")}
        </button>
        <button type="button" class="key key-paste" aria-disabled={off ? "true" : undefined} onMouseDown={keep} onClick={() => !off && (pasting.value = true)}>
          {t("key.paste")}
        </button>
      </div>
      {pasting.value && !off && <PasteSheet buf={buf} onClose={() => (pasting.value = false)} />}
      {editing && <KeysSheet keys={KEYS} onClose={() => setEditing(false)} />}
    </div>
  );
}

function NoTerms({ controller }: { controller: boolean }) {
  return (
    <div class="empty">
      <div class="empty-state">
        <p>{t("term.none")}</p>
        {controller && (
          <Button kind="neutral" needsLink onClick={newTerm}>
            {t("term.open_one")}
          </Button>
        )}
      </div>
    </div>
  );
}

export function PhoneTermView() {
  const body = useRef<HTMLDivElement | null>(null);
  usePinch(body);
  useEffect(() => link.subscribe("terms"), []);
  const termsTopic = store.topic("terms").value;
  const terms = readTerms(termsTopic?.data);
  const r = route.value;
  const want = r.tab === "term" && /^\d+$/.test(r.rest) ? Number(r.rest) : NaN;
  const buf = Number.isInteger(want) ? want : (terms.find((x) => x.active) ?? terms[0])?.buf;
  const item = terms.find((x) => x.buf === buf);
  useWakeLock(prefs.value.wakeLock && item?.busy === true && !item.exited);
  useEffect(() => {
    if (buf === undefined) return;
    const topic = "term:" + buf;
    link.subscribe(topic);
    return () => link.unsubscribe(topic);
  }, [buf]);
  const controller = link.meta.value?.role === "controller";
  const [ring, setRing] = useState(false);
  const [about, setAbout] = useState(false);
  const [atEnd, setAtEnd] = useState(true);
  const bells = rang.value;
  useEffect(() => {
    if (bells === 0) return;
    setRing(true);
    const timer = setTimeout(() => setRing(false), RING_MS);
    return () => clearTimeout(timer);
  }, [bells]);
  const ts = buf === undefined ? undefined : store.topic("term:" + buf).value;
  const snap = readTerm(ts?.data);
  const ended = isGone(ts?.data) || item?.exited === true;
  const canType = controller && live.value && !ended && buf !== undefined;
  useEffect(() => {
    typeBuf.value = canType ? buf! : null;
    return () => (typeBuf.value = null);
  }, [canType, buf]);
  const refused = link.refused.value.get("terms");
  return (
    <section class="view pterm-view" data-view="term" aria-labelledby="vh-term">
      <h1 class="sr-only" id="vh-term">
        {t("tab.term")}
      </h1>
      {terms.length > 0 && <Strip terms={terms} cur={buf} controller={controller} ring={ring} info={buf !== undefined && ts ? () => setAbout(true) : null} />}
      <div class="view-body term-body pterm-body" ref={body}>
        {refused ? (
          <Refused topic="terms">{refused.msg}</Refused>
        ) : !termsTopic ? (
          <SkeletonRows rows={10} />
        ) : terms.length === 0 ? (
          <NoTerms controller={controller} />
        ) : (
          buf !== undefined && <Rows key={buf} buf={buf} item={item} canType={canType} onEnd={setAtEnd} />
        )}
        {!atEnd && (
          <span class="term-follow">
            <Chip
              onClick={() => {
                cur.current?.toEnd();
                setAtEnd(true);
              }}
            >
              ↓ {t("term.follow")}
            </Chip>
          </span>
        )}
      </div>
      {ts && buf !== undefined && (controller ? <KeyBar buf={buf} off={!live.value || ended} /> : <div class="composer"><p class="composer-note">{t("viewonly")}</p></div>)}
      {about && buf !== undefined && <About buf={buf} cols={snap?.cols ?? 0} screen={snap?.screen === true} wrap={false} onClose={() => setAbout(false)} />}
    </section>
  );
}
