// The terminal view (WEB-20, web UX 3.3, 5.6): the laptop's shells as
// chips (a busy one has an amber dot), the chosen shell's rows live. Rows
// the laptop wrapped at its width are joined again and wrap at the
// phone's width, LTR and isolated. The view follows new output while the
// reader is at the end; scrolling up stops that and shows Follow. Older
// rows page in from the top.
import { Fragment, h } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import type { TermItem, TermNewRes } from "../../../../webproto/proto";
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
import { TermRow } from "../ui/rows";
import { SkeletonRows } from "../ui/Skeleton";
import { Refused, TopicBody } from "./Placeholder";
import { View } from "./View";

/** NEAR_BOTTOM_PX: closer than this to the end, new output keeps the view at the end. */
const NEAR_BOTTOM_PX = 48;

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
      <div class="term-rows" role="log" aria-label={item?.title || t("tab.term")}>
        {lines.map((l) => (
          <TermRow key={l.row} text={l.text} />
        ))}
      </div>
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
  return (
    <View
      tab="term"
      title={t("tab.term")}
      bodyRef={body}
      bodyClass="term-body"
      actions={
        controller && terms.length > 0 ? (
          <Button kind="quiet" small needsLink label={t("term.new")} onClick={newTerm}>
            +
          </Button>
        ) : undefined
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
    </View>
  );
}
