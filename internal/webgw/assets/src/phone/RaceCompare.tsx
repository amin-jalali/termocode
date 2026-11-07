// RaceCompare (MOBUX-10): one race on the phone. A small table on top
// (lane, files, tests, cost, judge ★) and below it one card per lane in a
// strip that swipes sideways and snaps to one card; tapping a table row
// or a dot scrolls to that lane. Merge never merges here: it opens a
// confirm card (Cancel first and focused, Merge arms after 600 ms), and
// the host asks the laptop, which shows its own card (MOB-62).
// The view runs no op itself: the host passes the race and the actions.
import { Fragment, h } from "preact";
import { useRef, useState } from "preact/hooks";
import { t } from "../strings";
import { Block } from "./Block";
import { StatusMark } from "./Mark";
import { raceRows, raceTotal, type Race, type RaceLane, type RaceRow } from "./race";
import { PhoneSheet } from "./Sheet";

const MAX_FILES = 6;

function Counts({ row }: { row: RaceRow }) {
  if (!row.filesKnown) return <span class="ph-race-num">—</span>;
  return (
    <span class="ph-race-num">
      {row.files} <span class="ph-race-add">+{row.add}</span> <span class="ph-race-del">−{row.del}</span>
    </span>
  );
}

function Judge({ on }: { on: boolean }) {
  return on ? (
    <span class="ph-race-star" role="img" aria-label={t("ph.race.judge")}>
      ★
    </span>
  ) : (
    <span class="ph-race-star is-off" aria-hidden="true">
      ·
    </span>
  );
}

function Table({ rows, current, onPick }: { rows: RaceRow[]; current: number; onPick: (i: number) => void }) {
  return (
    <div class="ph-race-table" role="table" aria-label={t("ph.race.table")}>
      <div class="ph-race-row is-head" role="row">
        <span role="columnheader">{t("race.col.lane")}</span>
        <span role="columnheader">{t("race.col.files")}</span>
        <span role="columnheader">{t("race.col.tests")}</span>
        <span role="columnheader">{t("race.col.cost")}</span>
        <span role="columnheader" aria-label={t("ph.race.judge")}>
          ★
        </span>
      </div>
      {rows.map((row, i) => (
        <button
          key={row.id}
          type="button"
          role="row"
          class={`ph-race-row${i === current ? " is-current" : ""}`}
          aria-current={i === current ? "true" : undefined}
          onClick={() => onPick(i)}
        >
          <span role="cell" class="ph-race-name">
            <StatusMark s={row.status} live={row.status === "working"} />
            <bdi dir="auto">{row.title}</bdi>
          </span>
          <span role="cell">
            <Counts row={row} />
          </span>
          <span role="cell">
            <StatusMark s={row.tests} />
          </span>
          <span role="cell" class="ph-race-num">
            {row.cost}
          </span>
          <span role="cell">
            <Judge on={row.judge} />
          </span>
        </button>
      ))}
    </div>
  );
}

function LaneCard({
  lane,
  row,
  index,
  count,
  onReview,
  onAsk,
}: {
  lane: RaceLane;
  row: RaceRow;
  index: number;
  count: number;
  onReview?: (l: RaceLane) => void;
  onAsk: (l: RaceLane) => void;
}) {
  const files = lane.files ?? [];
  return (
    <article class={`ph-race-lane${row.judge ? " is-judge" : ""}`} data-id={lane.id} aria-label={t("ph.race.lane_of", { n: index + 1, count, name: lane.title })}>
      <header class="ph-race-lane-head">
        <StatusMark s={row.status} live={row.status === "working"} />
        <bdi class="ph-race-lane-title" dir="auto">
          {lane.title}
        </bdi>
        {row.judge && <Judge on />}
      </header>
      <p class="ph-race-lane-meta">
        <bdi dir="ltr">{lane.agent}</bdi>
        {lane.branch && (
          <>
            {" · "}
            <bdi dir="ltr">{lane.branch}</bdi>
          </>
        )}
      </p>
      <dl class="ph-race-facts">
        <dt>{t("race.col.files")}</dt>
        <dd>
          <Counts row={row} />
        </dd>
        <dt>{t("race.col.tests")}</dt>
        <dd>
          <StatusMark s={row.tests} />
          {lane.verify?.state === "fail" && lane.verify.exit !== undefined && <span class="ph-race-num">{t("ph.race.exit", { code: lane.verify.exit })}</span>}
        </dd>
        <dt>{t("race.col.cost")}</dt>
        <dd class="ph-race-num">{row.cost}</dd>
      </dl>
      {lane.note && <p class="ph-race-note">{lane.note}</p>}
      {files.length > 0 ? (
        <ul class="ph-race-files">
          {files.slice(0, MAX_FILES).map((f) => (
            <li key={f.path}>
              <span class="ph-race-fst" aria-hidden="true">
                {f.status}
              </span>
              <bdi dir="ltr">{f.path}</bdi>
            </li>
          ))}
          {files.length > MAX_FILES && <li class="ph-race-more">{t("race.more_files", { n: files.length - MAX_FILES })}</li>}
        </ul>
      ) : (
        lane.stats && <p class="ph-race-note">{t("race.no_changes")}</p>
      )}
      <div class="ph-race-acts">
        {onReview && files.length > 0 && (
          <button type="button" class="ph-race-act" onClick={() => onReview(lane)}>
            {t("ph.race.review")}
          </button>
        )}
        {row.merge && (
          <button type="button" class="ph-race-act is-primary" onClick={() => onAsk(lane)}>
            {t("ph.race.merge")}
          </button>
        )}
      </div>
    </article>
  );
}

export function RaceCompare({
  race,
  canAct,
  onMerge,
  onReview,
  onSummarise,
  onVerify,
}: {
  race: Race;
  /** canAct: the phone may ask for writes (a viewer sees no Merge). */
  canAct: boolean;
  /** onMerge asks the laptop to merge the lane; the laptop shows its card. */
  onMerge: (l: RaceLane) => void;
  onReview?: (l: RaceLane) => void;
  onSummarise?: () => void;
  onVerify?: () => void;
}) {
  const rows = raceRows(race).map((r) => (canAct ? r : { ...r, merge: false }));
  const [current, setCurrent] = useState(0);
  const [ask, setAsk] = useState<RaceLane | null>(null);
  const strip = useRef<HTMLDivElement | null>(null);

  const goTo = (i: number) => {
    setCurrent(i);
    const el = strip.current?.children[i] as HTMLElement | undefined;
    el?.scrollIntoView({ block: "nearest", inline: "start" });
  };
  // The card most in view is the current one (a swipe moves it).
  const onScroll = () => {
    const s = strip.current;
    if (!s || s.clientWidth === 0) return;
    const i = Math.round(Math.abs(s.scrollLeft) / s.clientWidth);
    if (i !== current && i >= 0 && i < rows.length) setCurrent(i);
  };

  const title = t("ph.race.title", { n: race.number, task: race.prompt });
  const done = race.lanes.filter((l) => l.state === "done" || l.state === "stopped").length;
  return (
    <Block
      kind={t("ph.race.kind")}
      title={title}
      status={done === race.lanes.length ? "review" : "working"}
      live={done !== race.lanes.length}
      actions={
        (onSummarise || onVerify) && (
          <>
            {onSummarise && (
              <button type="button" class="ph-race-act" onClick={onSummarise}>
                {t("ph.race.summarise")}
              </button>
            )}
            {onVerify && race.verify && (
              <button type="button" class="ph-race-act" onClick={onVerify}>
                {t("ph.race.verify")}
              </button>
            )}
          </>
        )
      }
    >
      <p class="ph-race-sub">
        {t("ph.race.sub", { done, count: race.lanes.length })} · <span class="ph-race-num">{raceTotal(race)}</span>
      </p>
      {race.summary && race.summary.length > 0 && (
        <ul class="ph-race-summary" aria-label={t("ph.race.summary")}>
          {race.summary.map((s, i) => (
            <li key={i}>{s}</li>
          ))}
        </ul>
      )}
      <Table rows={rows} current={current} onPick={goTo} />
      <div class="ph-race-lanes" ref={strip} onScroll={onScroll}>
        {race.lanes.map((l, i) => (
          <LaneCard key={l.id} lane={l} row={rows[i]} index={i} count={race.lanes.length} onReview={onReview} onAsk={setAsk} />
        ))}
      </div>
      <div class="ph-race-dots" role="tablist" aria-label={t("ph.race.lanes")}>
        {rows.map((row, i) => (
          <button
            key={row.id}
            type="button"
            role="tab"
            class={`ph-race-dot${i === current ? " is-current" : ""}`}
            aria-selected={i === current ? "true" : "false"}
            aria-label={row.title}
            onClick={() => goTo(i)}
          >
            <span aria-hidden="true" />
          </button>
        ))}
      </div>
      {ask && (
        <PhoneSheet
          title={t("ph.race.merge_ask", { name: ask.title })}
          onLater={() => setAsk(null)}
          actions={[
            { label: t("cancel"), safe: true, onClick: () => setAsk(null) },
            {
              label: t("ph.race.merge"),
              risky: true,
              onClick: () => {
                const l = ask;
                setAsk(null);
                onMerge(l);
              },
            },
          ]}
        >
          <p class="ph-race-note">{t("ph.race.merge_note", { others: race.lanes.length - 1 })}</p>
        </PhoneSheet>
      )}
    </Block>
  );
}
