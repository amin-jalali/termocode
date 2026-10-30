// SERVICES (WEBP-4): the project's dev services as the laptop's SERVICES
// view lists them (the services topic): Docker Compose services and
// local dev commands, each with its state and ports. Start and Restart
// ask in the row first; Stop asks the laptop (a card there); Logs shows
// the newest lines under the row. A local command the laptop never ran
// asks there first. #/more/services
import { Fragment, h } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { ServiceDone, ServiceItem, ServiceLogs } from "../../../../../webproto/proto";
import { toast } from "../../lib/toasts";
import { link, store } from "../../session";
import { readServicesSnap } from "../../state/run";
import { t } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { Cod } from "../../ui/icons";
import { TermRow } from "../../ui/rows";
import { SkeletonRows } from "../../ui/Skeleton";
import { SubView } from "../more/SubView";
import { ask, canAct, Confirm, IconBtn, Note } from "./bits";

const UP: ReadonlySet<string> = new Set(["running", "healthy", "unhealthy"]);

/** tone is a service's dot colour. */
function tone(s: ServiceItem): string {
  if (s.busy || s.state === "unhealthy") return "busy";
  if (UP.has(s.state)) return "up";
  if (s.state === "exited" && (s.exit ?? 0) !== 0) return "bad";
  return "off";
}

function Logs({ name }: { name: string }) {
  const [logs, setLogs] = useState<ServiceLogs | null>(null);
  const read = () => ask("services.logs", { name }, (r) => setLogs(r as ServiceLogs));
  useEffect(read, [name]);
  return (
    <div class="svc-logs">
      {!logs ? (
        <SkeletonRows rows={3} />
      ) : logs.lines.length === 0 ? (
        <Note>{t("svc.logs_none")}</Note>
      ) : (
        <div class="tres-rows" dir="ltr">
          {logs.lines.map((l, i) => (
            <TermRow key={i} text={l} />
          ))}
        </div>
      )}
      <IconBtn icon="refresh" act="logs-refresh" label={t("ctr.refresh")} onClick={read} />
    </div>
  );
}

function Row({ s, act }: { s: ServiceItem; act: boolean }) {
  const [q, setQ] = useState<"" | "start" | "restart" | "stop">("");
  const [logs, setLogs] = useState(false);
  const up = UP.has(s.state);
  const run = (op: "start" | "restart") => {
    setQ("");
    ask("services." + op, { name: s.name }, (r) => {
      const d = (r ?? {}) as ServiceDone;
      if (d.asked) toast("info", t("svc.asked"));
      else if (d.note) toast("info", d.note);
    });
  };
  const ports = s.ports.map((p) => `${p.published}${p.target && p.target !== p.published ? "→" + p.target : ""}`).join(", ");
  return (
    <div class="svc" data-svc={s.name}>
      <div class="dv-row svc-row" tabIndex={0} onKeyDown={(e) => e.key === "Enter" && e.target === e.currentTarget && setLogs(!logs)} title={[s.status, s.command, s.source].filter(Boolean).join("\n")} onClick={() => setLogs(!logs)}>
        <span class={`svc-dot is-${tone(s)}`} aria-hidden="true">
          <Cod name={s.busy ? "loading" : "circle-filled"} />
        </span>
        <span class="trunc">{s.name}</span>
        <span class="tx-desc trunc" dir="ltr">
          {s.state}
          {s.state === "exited" && s.exit ? ` (${s.exit})` : ""}
          {ports && ` · ${ports}`}
        </span>
        {act && !q && (
          <span class="tx-acts">
            {up ? (
              <IconBtn icon="debug-stop" act="stop" label={t("stop")} off={s.busy} onClick={() => setQ("stop")} />
            ) : (
              <IconBtn icon="play" act="start" label={t("ctr.start")} off={s.busy} onClick={() => setQ("start")} />
            )}
            <IconBtn icon="debug-restart" act="restart" label={t("ctr.restart")} off={s.busy} onClick={() => setQ("restart")} />
            <IconBtn icon="output" act="logs" label={t("ctr.logs")} onClick={() => setLogs(!logs)} />
          </span>
        )}
      </div>
      {(q === "start" || q === "restart") && (
        <Confirm text={t(q === "start" ? "ctr.start_q" : "ctr.restart_q", { name: s.name })} verb={t(q === "start" ? "ctr.start" : "ctr.restart")} onNo={() => setQ("")} onYes={() => run(q)} />
      )}
      {q === "stop" && <AskLaptop label={t("stop")} text={t("svc.stop_q", { name: s.name })} send={() => link.request("services.stop", { name: s.name })} onDone={() => setQ("")} />}
      {logs && <Logs name={s.name} />}
    </div>
  );
}

export function ServicesView() {
  useEffect(() => link.subscribe("services"), []);
  const s = readServicesSnap(store.topic("services").value?.data);
  const act = canAct();
  const compose = s?.services.filter((x) => x.kind === "compose") ?? [];
  const local = s?.services.filter((x) => x.kind === "local") ?? [];
  const group = (title: string, list: readonly ServiceItem[]) =>
    list.length > 0 && (
      <section class="svc-group" aria-label={title}>
        <h2 class="svc-head">{title}</h2>
        {list.map((x) => (
          <Row key={x.kind + x.name} s={x} act={act} />
        ))}
      </section>
    );
  return (
    <SubView title={t("act.services")} name="services" actions={<IconBtn icon="refresh" act="refresh" label={t("ctr.refresh")} onClick={() => ask("services.refresh")} />}>
      <div class="svcs">
        {!s || s.state === "loading" ? (
          <SkeletonRows rows={4} />
        ) : s.state === "none" ? (
          <Note>{t("svc.none")}</Note>
        ) : (
          <>
            {s.problem && <p class="dbg-hint">{s.problem}</p>}
            {s.config_error && <Note>{s.config_error}</Note>}
            {group(s.project ? `${t("svc.compose")} · ${s.project}` : t("svc.compose"), compose)}
            {group(t("svc.local"), local)}
          </>
        )}
      </div>
    </SubView>
  );
}
