// The terminal view (WEB-20, web UX 3.3, 5.6): the laptop's shells as
// chips (a busy one has an amber dot), the chosen shell's rows live. Rows
// the laptop wrapped at its width are joined again and wrap at the
// phone's width, LTR and isolated. The view follows new output while the
// reader is at the end; scrolling up stops that and shows Follow. Older
// rows page in from the top.
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import type { TermItem, TermNewRes } from "../../../../webproto/proto";
import { rang } from "../lib/attention";
import { usePinch } from "../lib/rowfont";
import { go, route } from "../lib/router";
import { dropOlder, loadingOlder, loadOlder, older, stitch } from "../lib/term";
import { toast } from "../lib/toasts";
import { ReqError } from "../net/link";
import { link, store } from "../session";
import { isGone } from "../state/files";
import { readTerm, readTerms, reflow } from "../state/term";
import { t } from "../strings";
import { Button } from "../ui/Button";
import { errText } from "../ui/CardSheet";
import { Chip } from "../ui/Chip";
import { FontSize } from "../ui/FontSize";
import { Sheet } from "../ui/Sheet";
import { TermRow } from "../ui/rows";
import { SkeletonRows } from "../ui/Skeleton";
import { Refused, TopicBody } from "./Placeholder";
import { TermInput } from "./TermInput";
import { View } from "./View";

/** NEAR_BOTTOM_PX: closer than this to the end, new output keeps the view at the end. */
const NEAR_BOTTOM_PX = 48;
/** RING_MS is how long the bell dot shows after a shell rang. */
const RING_MS = 3000;

/** wrapRows: long rows wrap (on) or scroll sideways (off). */
export const wrapRows = signal(true);
/** asRows holds the shells shown as rows although screen mode is on
 * (the guess can be wrong; the Rows toggle fixes it). */
export const asRows = signal<ReadonlySet<number>>(new Set());

function setAsRows(buf: number, on: boolean): void {
  const next = new Set(asRows.peek());
  if (on) next.add(buf);
  else next.delete(buf);
  asRows.value = next;
}

/** showOnLaptop shows the shell's tab on the laptop. */
function showOnLaptop(buf: number): void {
  link.request("term.show_on_laptop", { buf }).then(
    () => toast("ok", t("laptop.shown")),
    (e: unknown) => {
      if (e instanceof ReqError) toast("warn", errText(e));
    },
  );
}

/** Screen is a full-screen program's screen at the laptop's size: a fixed
 * grid, read-only, that scrolls sideways; keys still work. */
function Screen({ buf, rows, cols, height }: { buf: number; rows: readonly string[]; cols: number; height: number }) {
  const shown = height > 0 ? rows.slice(-height) : rows;
  return (
    <div class="term-screen-box">
      <div class="file-note is-info term-banner" role="status">
        <span>{t("term.screen_banner", { cols, rows: height || shown.length })}</span>
        <Button kind="neutral" small needsLink onClick={() => showOnLaptop(buf)}>
          {t("laptop.open")}
        </Button>
        <Button kind="quiet" small onClick={() => setAsRows(buf, true)}>
          {t("term.rows")}
        </Button>
      </div>
      <pre class="term-screen" dir="ltr" style={cols > 0 ? { width: cols + "ch" } : undefined} aria-readonly="true">
        {shown.join("\n")}
      </pre>
    </div>
  );
}

/** About is the ⓘ sheet: the honest width note and the view's toggles. */
function About({ buf, cols, screen, onClose }: { buf: number; cols: number; screen: boolean; onClose: () => void }) {
  return (
    <Sheet onLater={onClose} label={t("term.about")}>
      <section class="edit term-about" role="dialog" aria-modal="true" aria-labelledby="term-about-t">
        <h2 class="edit-title" id="term-about-t">
          {t("term.about")}
        </h2>
        <p class="group-text">{t("term.info", { cols: cols || 80 })}</p>
        <div class="term-toggles">
          <Chip on={wrapRows.value} onClick={() => (wrapRows.value = !wrapRows.peek())}>
            {t("term.wrap")}
          </Chip>
          <Chip on={screen && !asRows.value.has(buf)} onClick={() => setAsRows(buf, !asRows.peek().has(buf))}>
            {t("term.screen")}
          </Chip>
        </div>
        <FontSize />
        <div class="edit-actions">
          <Button kind="neutral" onClick={onClose}>
            {t("close")}
          </Button>
          <Button kind="primary" needsLink onClick={() => showOnLaptop(buf)}>
            {t("laptop.open")}
          </Button>
        </div>
      </section>
    </Sheet>
  );
}

/** newTerm starts a shell on the laptop and shows it here. */
export function newTerm(): void {
  link.request("term.new").then(
    (data) => go({ tab: "term", rest: String((data as TermNewRes).buf), query: {} }),
    (e: unknown) => {
      if (e instanceof ReqError) toast("warn", errText(e));
    },
  );
}

function showTerm(buf: number): void {
  go({ tab: "term", rest: String(buf), query: {} });
}

/** Chips is the shell picker: one chip per shell, the chosen one on. */
function Chips({ terms, cur }: { terms: TermItem[]; cur: number | undefined }) {
  return (
    <div class="term-chips" role="tablist" aria-label={t("tab.term")}>
      {terms.map((x, i) => (
        <span key={x.buf} class="term-chip" role="tab" aria-selected={x.buf === cur ? "true" : "false"} data-buf={x.buf}>
          <Chip on={x.buf === cur} dot={x.busy ? "busy" : x.exited ? "warn" : undefined} onClick={() => showTerm(x.buf)}>
            <span class="trunc term-chip-title" dir="auto">
              {x.task || x.title}
            </span>
            <span class="term-chip-n">{i + 1}</span>
          </Chip>
        </span>
      ))}
    </div>
  );
}

function Rows({ buf, item, body }: { buf: number; item: TermItem | undefined; body: { current: HTMLElement | null } }) {
  const topic = "term:" + buf;
  const ts = store.topic(topic).value;
  const snap = readTerm(ts?.data);
  const prev = useRef<{ first: number; rows: readonly string[] } | null>(null);
  const keep = useRef<number | null>(null);
  const stick = useRef(true);
  const [pinned, setPinned] = useState(true);

  useEffect(() => {
    dropOlder();
    prev.current = null;
    stick.current = true;
    setPinned(true);
  }, [buf]);
  // Rows the window drops from its top stay joined to the paged ones.
  useEffect(() => {
    if (!snap) return;
    const p = prev.current;
    if (p && snap.first !== p.first) stitch(buf, p.first, p.rows, snap.first);
    prev.current = { first: snap.first, rows: snap.rows };
  }, [snap?.first, snap?.rows]);

  useEffect(() => {
    const el = body.current;
    if (!el) return;
    const onScroll = () => {
      const at = el.scrollHeight - el.scrollTop - el.clientHeight < NEAR_BOTTOM_PX;
      stick.current = at;
      setPinned(at);
    };
    el.addEventListener("scroll", onScroll, { passive: true });
    return () => el.removeEventListener("scroll", onScroll);
  }, []);
  useLayoutEffect(() => {
    const el = body.current;
    if (!el) return;
    if (keep.current !== null) {
      el.scrollTop += el.scrollHeight - keep.current; // older rows came in above: stay put
      keep.current = null;
    } else if (stick.current) el.scrollTop = el.scrollHeight;
  });

  if (!snap) {
    const r = link.refused.value.get(topic);
    if (r) return <Refused topic={topic}>{r.msg}</Refused>;
    return <SkeletonRows rows={10} />;
  }
  const o = older.value;
  const joined = o && o.buf === buf && o.first + o.rows.length === snap.first;
  const first = joined ? o.first : snap.first;
  const lines = reflow(joined ? [...o.rows, ...snap.rows] : snap.rows, first, snap.cols);
  const more = joined ? o.more : snap.more;
  const gone = isGone(ts?.data);
  const screen = snap.screen && !asRows.value.has(buf);
  const follow = () => {
    stick.current = true;
    setPinned(true);
    const el = body.current;
    if (el) el.scrollTop = el.scrollHeight;
  };
  return (
    <div class={`topic term${ts?.stale ? " is-stale" : ""}`}>
      {more && (
        <button
          type="button"
          class="term-older"
          disabled={loadingOlder.value}
          onClick={() => {
            keep.current = body.current?.scrollHeight ?? null;
            stick.current = false;
            loadOlder(buf, first);
          }}
        >
          {t("term.older")}
        </button>
      )}
      {screen ? (
        <Screen buf={buf} rows={snap.rows} cols={snap.cols} height={snap.height} />
      ) : (
        <div class={`term-rows${wrapRows.value ? "" : " is-nowrap"}`} role="log" aria-label={item?.title || t("tab.term")}>
          {lines.map((l) => (
            <TermRow key={l.row} text={l.text} />
          ))}
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
      {!pinned && (
        <span class="term-follow">
          <Chip onClick={follow}>↓ {t("term.follow")}</Chip>
        </span>
      )}
    </div>
  );
}

function NoTerms({ controller }: { controller: boolean }) {
  return (
    <div class="empty-state">
      <p>{t("term.none")}</p>
      {controller && (
        <Button kind="neutral" needsLink onClick={newTerm}>
          {t("term.open_one")}
        </Button>
      )}
    </div>
  );
}

export function TermView() {
  const body = useRef<HTMLDivElement | null>(null);
  usePinch(body);
  useEffect(() => link.subscribe("terms"), []);
  const terms = readTerms(store.topic("terms").value?.data);
  const r = route.value;
  const want = r.tab === "term" && /^\d+$/.test(r.rest) ? Number(r.rest) : NaN;
  const buf = Number.isInteger(want) ? want : (terms.find((x) => x.active) ?? terms[0])?.buf;
  const item = terms.find((x) => x.buf === buf);
  useEffect(() => {
    if (buf === undefined) return;
    const topic = "term:" + buf;
    link.subscribe(topic);
    return () => link.unsubscribe(topic);
  }, [buf]);
  const controller = link.meta.value?.role === "controller";
  const [ring, setRing] = useState(false);
  const [about, setAbout] = useState(false);
  const bells = rang.value;
  useEffect(() => {
    if (bells === 0) return;
    setRing(true);
    const timer = setTimeout(() => setRing(false), RING_MS);
    return () => clearTimeout(timer);
  }, [bells]);
  const ts = buf === undefined ? undefined : store.topic("term:" + buf).value;
  const ended = isGone(ts?.data) || item?.exited === true;
  let foot;
  if (ts && buf !== undefined) {
    foot = controller ? (
      <TermInput buf={buf} ended={ended} />
    ) : (
      <div class="composer">
        <p class="composer-note">{t("viewonly")}</p>
      </div>
    );
  }
  return (
    <View
      tab="term"
      title={t("tab.term")}
      bodyRef={body}
      bodyClass="term-body"
      foot={foot}
      actions={
        <>
          {ring && <span class="bell-dot" role="status" aria-label={t("term.bell")} />}
          {buf !== undefined && ts && (
            <button type="button" class="head-btn term-info-btn" aria-label={t("term.about")} onClick={() => setAbout(true)}>
              ⓘ
            </button>
          )}
          {controller && terms.length > 0 && (
            <Button kind="quiet" small needsLink label={t("term.new")} onClick={newTerm}>
              +
            </Button>
          )}
        </>
      }
    >
      <TopicBody
        topic="terms"
        empty={<NoTerms controller={controller} />}
      >
        {terms.length === 0 ? (
          <div class="empty">
            <NoTerms controller={controller} />
          </div>
        ) : (
          <>
            <Chips terms={terms} cur={buf} />
            {buf !== undefined && <Rows key={buf} buf={buf} item={item} body={body} />}
          </>
        )}
      </TopicBody>
      {about && buf !== undefined && (
        <About buf={buf} cols={readTerm(ts?.data)?.cols ?? 0} screen={readTerm(ts?.data)?.screen === true} onClose={() => setAbout(false)} />
      )}
    </View>
  );
}
