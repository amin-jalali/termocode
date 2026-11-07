// Races (MOB-61, mobile plan §3.9): the laptop's agent races, newest
// first. Each race shows its task, the AI's short summary and the race
// compare block: one row per lane with a state shape, files, lines,
// verify and cost; ★ marks the merged lane. A tap on a lane shows the
// files it changed. Read only: merge and discard stay on the laptop.
// On the phone, "New lane" starts one agent in a new worktree (MOB-65,
// LaneStart.tsx) after the laptop's card.
// #/more/races
import { h } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { RaceItem } from "../../../../../webproto/proto";
import { layout } from "../../lib/layout";
import { link, store } from "../../session";
import { SkeletonRows } from "../../ui/Skeleton";
import { t } from "../../strings";
import { compareRows, raceMark, readRaces, type CompareRow } from "../../state/races";
import { LaneStart } from "./LaneStart";
import { SubView } from "./SubView";

function verifyText(r: CompareRow): string {
  if (r.verify === "pass") return "✓";
  if (r.verify === "fail") return r.exit ? `✗ ${r.exit}` : "✗";
  if (r.verify === "running") return "◐";
  return "–";
}

function LaneFiles({ race, lane }: { race: RaceItem; lane: string }) {
  const l = race.lanes.find((x) => x.id === lane);
  if (!l) return null;
  if (!l.stats) return <p class="run-why">{t("race.files_later")}</p>;
  const files = l.changed ?? [];
  if (files.length === 0) return <p class="run-why">{t("race.no_changes")}</p>;
  return (
    <ul class="rows" aria-label={t("race.files")}>
      {files.map((f) => (
        <li key={f.path} class="row" dir="ltr">
          {f.status} {f.path}
          {f.binary ? ` · ${t("race.binary")}` : ` +${f.add ?? 0} −${f.del ?? 0}`}
        </li>
      ))}
      {(l.files ?? 0) > files.length && <li class="row">{t("race.more_files", { n: String((l.files ?? 0) - files.length) })}</li>}
    </ul>
  );
}

function RaceBlock({ r }: { r: RaceItem }) {
  const [open, setOpen] = useState("");
  const rows = compareRows(r);
  return (
    <section class={`run st-${r.state}`} data-key={r.id} aria-label={t("race.title", { n: String(r.number) })}>
      <p class="run-title" dir="auto">
        {raceMark(r.state)} #{r.number} · {r.prompt}
      </p>
      <p class="run-meta" dir="ltr">
        {t(`race.state.${r.state}`)}
        {r.verify ? ` · ${t("race.verify_set")}` : ""}
      </p>
      {r.summary && r.summary.length > 0 && (
        <div class="card-subject" dir="auto">
          {r.summary.map((s, i) => (
            <p key={i}>{s}</p>
          ))}
          {r.summary_cost && <p class="run-why">{r.summary_cost}</p>}
        </div>
      )}
      <div class="db-grid-box">
        <table class="db-grid">
          <thead>
            <tr>
              <th scope="col">{t("race.col.lane")}</th>
              <th scope="col">{t("race.col.files")}</th>
              <th scope="col">{t("race.col.lines")}</th>
              <th scope="col">{t("race.col.tests")}</th>
              <th scope="col">{t("race.col.cost")}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((x) => (
              <tr key={x.id} class={`st-${x.state}`}>
                <td>
                  <button
                    type="button"
                    class="more-link"
                    aria-expanded={open === x.id}
                    aria-label={t("race.lane", { name: x.title, state: t(`race.lane.${x.state}`) })}
                    onClick={() => setOpen(open === x.id ? "" : x.id)}
                  >
                    {x.mark} {x.title}
                    {x.winner ? " ★" : ""}
                  </button>
                </td>
                <td dir="ltr">{x.files}</td>
                <td dir="ltr">{x.lines || "–"}</td>
                <td dir="ltr">{verifyText(x)}</td>
                <td dir="ltr">{x.cost || "–"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {open && <LaneFiles race={r} lane={open} />}
    </section>
  );
}

export function RacesView() {
  useEffect(() => link.subscribe("races"), []);
  const st = store.topic("races").value;
  const { state, races } = readRaces(st?.data);
  const [lane, setLane] = useState(false);
  // Phone layout only; a viewer cannot start one.
  const canLane = layout.value !== "desktop" && link.meta.value?.role === "controller";
  return (
    <SubView
      title={t("more.races")}
      actions={
        canLane ? (
          <button type="button" class="btn lane-new" data-act="lane" onClick={() => setLane(true)}>
            {t("lane.new")}
          </button>
        ) : undefined
      }
    >
      {lane && <LaneStart onDone={() => setLane(false)} />}
      {!st || state === "loading" ? (
        <SkeletonRows rows={4} />
      ) : state === "none" ? (
        <p class="empty">{t("race.no_repo")}</p>
      ) : state === "error" ? (
        <p class="empty">{t("race.read_error")}</p>
      ) : races.length === 0 ? (
        <p class="empty">{t("race.none")}</p>
      ) : (
        <div class="runs">
          {races.map((r) => (
            <RaceBlock key={r.id} r={r} />
          ))}
        </div>
      )}
    </SubView>
  );
}
