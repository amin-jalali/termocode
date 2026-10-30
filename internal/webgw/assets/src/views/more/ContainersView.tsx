// Containers (WEB-44, web design 2.9): the host's containers as the
// laptop's engine lists them, with a log tail. Start and restart ask in
// the card first (Cancel first and focused); stop and remove ask the
// laptop. The list is read when the page opens and with Refresh, and
// again after an action. #/more/containers
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { toast } from "../../lib/toasts";
import { link } from "../../session";
import { readContainers, type ContainersView as List, type CtrView } from "../../state/more";
import { t } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { Button } from "../../ui/Button";
import { SkeletonRows } from "../../ui/Skeleton";
import { gitFailed as failed } from "../git/errors";
import { SubView } from "./SubView";

interface Logs {
  lines: string[];
  redacted: number;
  truncated: boolean;
}

function readLogs(r: unknown): Logs {
  const o = typeof r === "object" && r !== null ? (r as Record<string, unknown>) : {};
  return {
    lines: Array.isArray(o.lines) ? o.lines.filter((x): x is string => typeof x === "string") : [],
    redacted: typeof o.redacted === "number" ? o.redacted : 0,
    truncated: o.truncated === true,
  };
}

const RUNNING: ReadonlySet<string> = new Set(["running", "restarting", "paused"]);

function Ctr({ c, canAct, reload }: { c: CtrView; canAct: boolean; reload(): void }) {
  const [open, setOpen] = useState(false);
  const [logs, setLogs] = useState<Logs | null>(null);
  const [asking, setAsking] = useState<"" | "start" | "restart">("");
  const [laptop, setLaptop] = useState<"" | "stop" | "remove">("");
  const safe = useRef<HTMLButtonElement | null>(null);
  useEffect(() => {
    if (asking) safe.current?.focus();
  }, [asking]);
  const running = RUNNING.has(c.state);
  const showLogs = () => {
    if (logs) return setLogs(null);
    link.request("containers.logs", { id: c.id }).then((r) => setLogs(readLogs(r)), failed);
  };
  const act = (op: "start" | "restart") => {
    setAsking("");
    link.request("containers." + op, { id: c.id }).then((r) => {
      const st = (r as { state?: unknown } | null)?.state;
      toast("ok", t("ctr.done", { name: c.name, state: typeof st === "string" ? st : "" }));
      reload();
    }, failed);
  };
  return (
    <section class={`ctr st-${c.state}${c.busy ? " is-busy" : ""}`} data-id={c.id}>
      <button type="button" class="ctr-line" aria-expanded={open ? "true" : "false"} onClick={() => setOpen(!open)}>
        <span class={`task-dot st-${running ? "running" : c.state === "exited" ? "failed" : "ok"}`} aria-hidden="true" />
        <span class="ctr-name" dir="ltr">
          {c.name}
        </span>
        <span class="ctr-state">{c.state}</span>
      </button>
      <p class="ctr-meta" dir="ltr">
        {c.image}
        {c.project && ` · ${c.project}`}
        {c.status && ` · ${c.status}`}
        {c.ports && ` · ${c.ports}`}
      </p>
      {open && (
        <div class="ctr-actions">
          {asking ? (
            <div class="row-ask" role="group" aria-label={t(asking === "start" ? "ctr.start_q" : "ctr.restart_q", { name: c.name })}>
              <p class="row-ask-text">{t(asking === "start" ? "ctr.start_q" : "ctr.restart_q", { name: c.name })}</p>
              <div class="choices">
                <button type="button" class="btn btn-neutral" ref={safe} onClick={() => setAsking("")}>
                  {t("cancel")}
                </button>
                <Button kind="primary" needsLink onClick={() => act(asking)}>
                  {asking === "start" ? t("ctr.start") : t("ctr.restart")}
                </Button>
              </div>
            </div>
          ) : (
            <div class="choices">
              <Button small needsLink onClick={showLogs}>
                {logs ? t("ctr.hide_logs") : t("ctr.logs")}
              </Button>
              {canAct && !c.busy && !running && (
                <Button small needsLink onClick={() => setAsking("start")}>
                  {t("ctr.start")}
                </Button>
              )}
              {canAct && !c.busy && running && (
                <Button small needsLink onClick={() => setAsking("restart")}>
                  {t("ctr.restart")}
                </Button>
              )}
              {canAct && !c.busy && running && (
                <Button small kind="danger" needsLink onClick={() => setLaptop("stop")}>
                  {t("ctr.stop")}
                </Button>
              )}
              {canAct && !c.busy && !running && (
                <Button small kind="danger" needsLink onClick={() => setLaptop("remove")}>
                  {t("ctr.remove")}
                </Button>
              )}
            </div>
          )}
          {logs && (
            <div class="ctr-logs">
              {logs.redacted > 0 && <p class="ctr-note">{t("ctr.redacted", { n: logs.redacted })}</p>}
              {logs.truncated && <p class="ctr-note">{t("ctr.older")}</p>}
              {logs.lines.length === 0 ? (
                <p class="empty">{t("ctr.no_logs")}</p>
              ) : (
                <div class="task-rows">
                  {logs.lines.map((l, i) => (
                    <div class="trow" key={i} dir="ltr">
                      {l}
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>
      )}
      {laptop && (
        <AskLaptop
          label={laptop === "stop" ? t("ctr.stop") : t("ctr.remove")}
          text={t(laptop === "stop" ? "ctr.stop_q" : "ctr.remove_q", { name: c.name })}
          send={() => link.request("containers." + laptop, { id: c.id })}
          onDone={() => {
            setLaptop("");
            reload();
          }}
        />
      )}
    </section>
  );
}

export function ContainersView() {
  const [list, setList] = useState<List | null>(null);
  const [err, setErr] = useState("");
  const [n, setN] = useState(0);
  const reload = () => setN((x) => x + 1);
  useEffect(() => {
    link.request("containers.list", {}).then(
      (r) => {
        setErr("");
        setList(readContainers(r));
      },
      (e: unknown) => {
        setErr(e instanceof Error ? e.message : t("err.internal"));
      },
    );
  }, [n]);
  const canAct = link.meta.value?.role !== "viewer";
  return (
    <SubView title={t("more.containers")}>
      <div class="ctrs">
        <div class="ctrs-head">
          <p class="view-note" dir="ltr">
            {list ? t("ctr.engine", { engine: list.engine, context: list.context }) : ""}
          </p>
          <Button small needsLink onClick={reload}>
            {t("ctr.refresh")}
          </Button>
        </div>
        {err ? (
          <p class="empty refused" dir="auto">
            {err}
          </p>
        ) : !list ? (
          <SkeletonRows rows={5} />
        ) : list.containers.length === 0 ? (
          <p class="empty">{t("ctr.none")}</p>
        ) : (
          list.containers.map((c) => <Ctr key={c.id} c={c} canAct={canAct} reload={reload} />)
        )}
        {list && list.more > 0 && <p class="view-note">{t("ctr.more", { n: list.more })}</p>}
      </div>
    </SubView>
  );
}
