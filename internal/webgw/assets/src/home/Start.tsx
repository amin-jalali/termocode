// The Home's empty state (MOBUX-4, design sheet 32): what the phone
// shows before the first message or chip.
//   summary    ✦ Here is your laptop today. + what matters, in one line
//   My work    one grouped card: a row per non-empty state (needs you ·
//              working · done today), its colour tile and shape, the
//              newest title under it, its count; a tap opens the list
//              behind it through a chip (no tokens)
//   Shortcuts  Testing (its failures) and Git changes with live counts,
//              then the chip strip (it leaves the box's foot meanwhile)
//   quiet day  nothing waits, runs or ended: one line and chips that
//              only fill the box (a failing test, the changes, a task)
// A long press on a row, or a section's ⋯, offers it to the AI (the
// action sheet: ask, add to message). Data only from the laptop's topics
// (state/home.ts homeSummary, status, tests); nothing is stored here.
import { h, H } from "./jsx";

type El = h.JSX.Element;
import { hs, type HomeKey } from "./strings";
import { PinnedViews } from "./saved";
import { openActions } from "./ActionSheet";
import type { HomeSummary } from "../state/home";

/** StartCard names a My work row; the Home maps it to the chip it opens. */
export type StartCard = "needs" | "working" | "done";

/** Row is one row of a section. */
interface Row {
  title: string;
  meta?: string;
  n: string;
  bad?: boolean;
  tile: string;
  icon: string;
  /** the laptop's view the row stands for (its reference for the AI) */
  view: string;
  open(): void;
}

// The tiles' icons (24 px paths), drawn when a row is: the page's h is
// not here before init.
const BOLT = "M13 2L4 14h7l-1 8 9-12h-7z";
const PLAY = "M7 4l13 8-13 8z";
const CHECK = "M20 6L9 17l-5-5";
const FLASK = "M9 3h6M10 3v6L4 20h16L14 9V3";
const MYWORK: HomeKey = "start.mywork";
const SHORTCUTS: HomeKey = "start.shortcuts";
const DIFF = "M12 3v14M5 10h14M5 21h14";

/** tests reads the tests topic, if the page has it: failures, total, the
 * first failing test's name. */
function tests(): { failed: number; total: number; first?: string } | null {
  const d = H.store.topic("tests").value?.data as { counts?: { failed: number; total: number }; items?: { kind: string; state: string; label: string }[] } | undefined;
  if (!d?.counts) return null;
  return { ...d.counts, first: d.items?.find((x) => x.kind === "case" && x.state === "failed")?.label };
}

function Section({ title, rows, chat, foot }: { title: HomeKey; rows: Row[]; chat: string; foot?: El }) {
  const name = hs(title);
  const offer = (label: string, view: string) => openActions({ chat, title: label, ref: { kind: "view", spec: JSON.stringify({ view }) } });
  return (
    <section class="home-sec" aria-label={name}>
      <div class="home-sec-h">
        <h2>{name}</h2>
        <button type="button" class="home-sec-more" aria-label={hs("start.more", { title: name })} onClick={() => offer(name, rows[0]?.view ?? "inbox")}>
          ⋯
        </button>
      </div>
      {rows.length > 0 && (
        <div class="home-card">
          {rows.map((r) => (
            <button
              key={r.title}
              type="button"
              class="home-row"
              onClick={r.open}
              onContextMenu={(e: Event) => {
                e.preventDefault();
                offer(r.title, r.view);
              }}
            >
              <span class="home-tile" data-t={r.tile}>
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                  <path d={r.icon} />
                </svg>
              </span>
              <span class="ph-row-main home-row-main">
                <span class="ph-row-title trunc">{r.title}</span>
                {r.meta && (
                  <span class="ph-row-meta home-row-sub trunc" dir="auto">
                    {r.meta}
                  </span>
                )}
              </span>
              <span class={"ph-row-end home-row-age" + (r.bad ? " is-bad" : "")}>{r.n}</span>
            </button>
          ))}
        </div>
      )}
      {foot}
    </section>
  );
}

/** HomeStart is the Home before its first message. */
export function HomeStart({ sum, chat, chips, onCard, onFill }: { sum: HomeSummary; chat: string; chips: El; onCard: (c: StartCard) => void; onFill: (text: string) => void }) {
  const dirty = H.status.value.dirty;
  const t = tests();
  const failed = t?.failed ?? 0;
  const work: Row[] = [];
  const add = ({ card, tile, icon, view }: { card: StartCard; tile: string; icon: string; view: string }) => {
    const { n, first } = sum[card];
    if (n > 0) work.push({ title: hs(("start." + card) as HomeKey), meta: first, n: String(n), tile, icon, view, open: () => onCard(card) });
  };
  add({ card: "needs", tile: "a", icon: BOLT, view: "inbox" });
  add({ card: "working", tile: "b", icon: PLAY, view: "board" });
  add({ card: "done", tile: "g", icon: CHECK, view: "board" });

  if (sum.known && work.length === 0) {
    const fix = t?.first;
    return (
      <div class="home-start home-today">
        <div class="home-quiet">
          <span class="star" aria-hidden="true">
            ✦
          </span>
          <strong>{hs("start.quiet")}</strong>
          <p>{hs("start.quiet_hint")}</p>
          <nav class="home-qchips" aria-label={hs("chips")}>
            {fix && (
              <button type="button" class="chip tone-ai" onClick={() => onFill(hs("start.q_fix_fill", { name: fix }))}>
                {hs("start.q_fix", { name: fix })}
              </button>
            )}
            {dirty > 0 && (
              <button type="button" class="chip" onClick={() => onFill(hs("start.q_review_fill", { n: dirty }))}>
                {hs("start.q_review", { n: dirty })}
              </button>
            )}
            <button type="button" class="chip" aria-label={hs("start.q_task")} onClick={() => H.go({ tab: "more", rest: "new-task", query: {} })}>
              {hs("start.q_task")}
            </button>
          </nav>
        </div>
        <PinnedViews />
      </div>
    );
  }

  const facts = [
    failed > 0 && hs("start.f_tests", { n: failed }),
    sum.needs.n > 0 && hs("start.f_needs", { n: sum.needs.n }),
    sum.working.n > 0 && hs("start.f_working", { n: sum.working.n }),
    dirty > 0 && hs("start.f_dirty", { n: dirty }),
  ].filter(Boolean);
  const short: Row[] = [];
  if (t) short.push({ title: hs("start.testing"), n: failed > 0 ? hs("start.failed", { n: failed }) : String(t.total), bad: failed > 0, tile: "g", icon: FLASK, view: "tests", open: () => H.go({ tab: "more", rest: "testing", query: {} }) });
  if (dirty > 0) short.push({ title: hs("start.git"), n: String(dirty), tile: "r", icon: DIFF, view: "git", open: () => H.goTab("git") });
  return (
    <div class="home-start home-today">
      <p class="home-hello" dir="auto">
        <span class="star" aria-hidden="true">
          ✦
        </span>{" "}
        <strong>{hs("start.hello")}</strong>
        {facts.length > 0 && <span class="home-hello-hint">{facts.join(" · ")}</span>}
      </p>
      {work.length > 0 && <Section title={MYWORK} rows={work} chat={chat} />}
      <Section title={SHORTCUTS} rows={short} chat={chat} foot={chips} />
      <PinnedViews />
    </div>
  );
}
