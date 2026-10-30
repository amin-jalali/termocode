// The desktop's Containers view, a tree like the laptop editor's
// container tools: the engine and its context, the containers grouped by
// their compose project, each with its state's icon (running, paused,
// stopped), its image dim and its status; on hover Logs, Start, Restart,
// Stop and Remove. Start and restart ask in the view first (Cancel first
// and focused); stop and remove ask the laptop. Logs show in the panel's
// OUTPUT tab, secret values hidden by the laptop.
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useState } from "preact/hooks";
import { panelTab } from "../../lib/lsp";
import { toast } from "../../lib/toasts";
import { setPanes } from "../../lib/workbench";
import { link, live } from "../../session";
import { readContainers, type ContainersView, type CtrView } from "../../state/more";
import { t } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { errText } from "../../ui/CardSheet";
import { Cod } from "../../ui/icons";
import { Act, Ask, PaneSection, PaneView, Row, Tree } from "../../ui/pane";
import { Portal } from "../../ui/Portal";
import { gitFailed as failed } from "../git/errors";

type Load = { s: "loading" } | { s: "ok"; v: ContainersView } | { s: "err"; msg: string };

const list = signal<Load | null>(null);
const shut = signal<ReadonlySet<string>>(new Set());

/** Logs is what the OUTPUT panel shows: one container's last lines. */
export interface Logs {
  id: string;
  name: string;
  lines: string[];
  redacted: number;
  truncated: boolean;
  loading: boolean;
  err: string;
}
export const logs = signal<Logs | null>(null);

function reload(): void {
  if (!list.peek()) list.value = { s: "loading" };
  link.request("containers.list", {}).then(
    (r) => (list.value = { s: "ok", v: readContainers(r) }),
    (e: unknown) => (list.value = { s: "err", msg: errText(e) }),
  );
}

/** showLogs reads a container's log tail into the panel's OUTPUT tab. */
export function showLogs(c: { id: string; name: string }): void {
  logs.value = { id: c.id, name: c.name, lines: [], redacted: 0, truncated: false, loading: true, err: "" };
  panelTab.value = "output";
  setPanes({ panel: true });
  link.request("containers.logs", { id: c.id }).then(
    (r) => {
      const o = typeof r === "object" && r !== null ? (r as Record<string, unknown>) : {};
      if (logs.peek()?.id !== c.id) return;
      logs.value = {
        id: c.id,
        name: c.name,
        lines: Array.isArray(o.lines) ? o.lines.filter((x): x is string => typeof x === "string") : [],
        redacted: typeof o.redacted === "number" ? o.redacted : 0,
        truncated: o.truncated === true,
        loading: false,
        err: "",
      };
    },
    (e: unknown) => {
      if (logs.peek()?.id === c.id) logs.value = { ...c, lines: [], redacted: 0, truncated: false, loading: false, err: errText(e) };
    },
  );
}

const RUNNING: ReadonlySet<string> = new Set(["running", "restarting"]);

function icon(c: CtrView) {
  if (c.busy) return <Cod name="loading" />;
  if (RUNNING.has(c.state)) return <span class="c-ok"><Cod name="vm-running" /></span>;
  if (c.state === "paused") return <span class="c-warn"><Cod name="vm-active" /></span>;
  return <span class="c-err"><Cod name="vm-outline" /></span>;
}

type Pending = { id: string; op: "start" | "restart" } | null;
type Laptop = { c: CtrView; op: "stop" | "remove" } | null;

function CtrRows({ cs, level, canAct, pending, setPending, setLaptop }: { cs: CtrView[]; level: number; canAct: boolean; pending: Pending; setPending: (p: Pending) => void; setLaptop: (l: Laptop) => void }) {
  const act = (c: CtrView, op: "start" | "restart") => {
    setPending(null);
    link.request("containers." + op, { id: c.id }).then((r) => {
      const st = (r as { state?: unknown } | null)?.state;
      toast("ok", t("ctr.done", { name: c.name, state: typeof st === "string" ? st : "" }));
      reload();
    }, failed);
  };
  return (
    <>
      {cs.map((c) => {
        const running = RUNNING.has(c.state) || c.state === "paused";
        const can = canAct && !c.busy;
        return (
          <Fragment key={c.id}>
            <Row
              level={level}
              icon={icon(c)}
              label={c.name}
              desc={c.image}
              title={[c.name, c.image, c.status, c.ports].filter(Boolean).join(" — ")}
              data={{ id: c.id, state: c.state }}
              onClick={() => showLogs(c)}
              end={<span class="tr-age">{c.status}</span>}
              actions={
                <>
                  <Act icon="output" label={t("ctr.logs")} onClick={() => showLogs(c)} />
                  {can && !running && <Act icon="play" label={t("ctr.start")} onClick={() => setPending({ id: c.id, op: "start" })} />}
                  {can && running && <Act icon="debug-restart" label={t("ctr.restart")} onClick={() => setPending({ id: c.id, op: "restart" })} />}
                  {can && running && <Act icon="debug-stop" label={t("ctr.stop")} onClick={() => setLaptop({ c, op: "stop" })} />}
                  {can && !running && <Act icon="trash" label={t("ctr.remove")} onClick={() => setLaptop({ c, op: "remove" })} />}
                </>
              }
            />
            {pending?.id === c.id && (
              <Ask
                text={t(pending.op === "start" ? "ctr.start_q" : "ctr.restart_q", { name: c.name })}
                verb={t(pending.op === "start" ? "ctr.start" : "ctr.restart")}
                onYes={() => act(c, pending.op)}
                onNo={() => setPending(null)}
              />
            )}
          </Fragment>
        );
      })}
    </>
  );
}

export function DeskContainers() {
  const on = live.value;
  useEffect(() => {
    if (on) reload();
  }, [on]);
  const [pending, setPending] = useState<Pending>(null);
  const [laptop, setLaptop] = useState<Laptop>(null);
  const canAct = link.meta.value?.role !== "viewer";
  const l = list.value;
  const v = l?.s === "ok" ? l.v : null;
  // Compose projects first (as folders), then the containers of none.
  const projects = new Map<string, CtrView[]>();
  const loose: CtrView[] = [];
  for (const c of v?.containers ?? []) {
    if (c.project) projects.set(c.project, [...(projects.get(c.project) ?? []), c]);
    else loose.push(c);
  }
  const flip = (k: string) => {
    const n = new Set(shut.peek());
    if (n.has(k)) n.delete(k);
    else n.add(k);
    shut.value = n;
  };
  const rows = { canAct, pending, setPending, setLaptop };
  return (
    <PaneView id="containers" title={t("more.containers")} actions={<Act icon="refresh" label={t("ctr.refresh")} disabled={!on} onClick={reload} />}>
      <PaneSection id="containers" title={t("more.containers")} open={!shut.value.has("#")} onToggle={() => flip("#")} count={v ? v.containers.length : undefined}>
        {!l || l.s === "loading" ? (
          <p class="pv-msg">{t("loading")}</p>
        ) : l.s === "err" ? (
          <p class="pv-msg c-err">{l.msg}</p>
        ) : v && v.containers.length === 0 ? (
          <p class="pv-msg">{t("ctr.none")}</p>
        ) : (
          <Tree label={t("more.containers")}>
            {[...projects].map(([p, cs]) => (
              <Fragment key={p}>
                <Row level={0} twist={shut.value.has(p) ? "closed" : "open"} onToggle={() => flip(p)} icon={<Cod name="package" />} label={p} data={{ project: p }} end={<span class="count">{cs.length}</span>} />
                {!shut.value.has(p) && <CtrRows cs={cs} level={1} {...rows} />}
              </Fragment>
            ))}
            <CtrRows cs={loose} level={0} {...rows} />
          </Tree>
        )}
        {v && v.more > 0 && <p class="pv-msg">{t("ctr.more", { n: v.more })}</p>}
        {v && <p class="pv-msg">{t("ctr.engine", { engine: v.engine, context: v.context })}</p>}
      </PaneSection>
      {laptop && (
        <AskLaptop
          label={laptop.op === "stop" ? t("ctr.stop") : t("ctr.remove")}
          text={t(laptop.op === "stop" ? "ctr.stop_q" : "ctr.remove_q", { name: laptop.c.name })}
          send={() => link.request("containers." + laptop.op, { id: laptop.c.id })}
          onDone={() => {
            setLaptop(null);
            reload();
          }}
        />
      )}
    </PaneView>
  );
}

/** OutputView is the panel's OUTPUT tab: the chosen container's logs. */
export function OutputView() {
  const l = logs.value;
  return (
    <section class="view outv" data-view="output" aria-label={t("tasks.output")}>
      {!l ? (
        <p class="outv-note">{t("wb.output_none")}</p>
      ) : (
        <>
          <Portal into="panel-view-actions">
            <div class="outv-head">
              <span class="outv-channel" title={l.id}>
                <Cod name="vm-running" /> {l.name}
              </span>
              <Act icon="refresh" label={t("ctr.refresh")} onClick={() => showLogs(l)} />
            </div>
          </Portal>
          {l.redacted > 0 && <p class="outv-note">{t("ctr.redacted", { n: l.redacted })}</p>}
          {l.truncated && <p class="outv-note">{t("ctr.older")}</p>}
          <div class="outv-body" dir="ltr" tabIndex={0}>
            {l.loading ? (
              <p class="outv-note">{t("loading")}</p>
            ) : l.err ? (
              <p class="outv-note c-err">{l.err}</p>
            ) : l.lines.length === 0 ? (
              <p class="outv-note">{t("ctr.no_logs")}</p>
            ) : (
              l.lines.map((x, i) => (
                <div class="outv-line" key={i}>
                  {x}
                </div>
              ))
            )}
          </div>
        </>
      )}
    </section>
  );
}
