// Kubernetes (WEBP-4): the clusters the laptop's kubectl knows, read
// only, like the laptop's Kubernetes view: a context and a namespace,
// their pods, deployments, services and jobs, and for the picked one its
// logs (pods) or kubectl describe. Restarting a deployment asks the
// laptop (a card there). Nothing here changes the laptop's kubeconfig.
// The lists read again every 10 s while the view shows. #/more/kube
import { h, type ComponentChildren } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { KubeContexts, KubeList, KubeText } from "../../../../../webproto/proto";
import { useNow } from "../../lib/time";
import { link, live } from "../../session";
import { age } from "../../state/git";
import { t, type StringKey } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { TermRow } from "../../ui/rows";
import { SkeletonRows } from "../../ui/Skeleton";
import { gitFailed } from "../git/errors";
import { SubView } from "../more/SubView";
import { ask, canAct, IconBtn, Note } from "./bits";

const EVERY_MS = 10_000;

type Kind = "pod" | "deployment" | "service" | "job";
interface Pick {
  kind: Kind;
  name: string;
  containers?: readonly string[];
}

/** Row is one object: its name, then its facts in columns. */
function Row({ p, on, facts, onPick }: { p: Pick; on: boolean; facts: readonly string[]; onPick: () => void }) {
  return (
    <button type="button" class={`kb-row${on ? " is-picked" : ""}`} data-kube={`${p.kind}/${p.name}`} onClick={onPick}>
      <span class="kb-name trunc" dir="ltr">
        {p.name}
      </span>
      {facts.map((f, i) => (
        <span key={i} class="kb-fact trunc" dir="ltr">
          {f}
        </span>
      ))}
    </button>
  );
}

function Detail({ scope, pick, act }: { scope: { context: string; namespace: string }; pick: Pick; act: boolean }) {
  const [mode, setMode] = useState<"logs" | "describe">(pick.kind === "pod" ? "logs" : "describe");
  const [ctr, setCtr] = useState(pick.containers?.[0] ?? "");
  const [text, setText] = useState<KubeText | null>(null);
  const [restart, setRestart] = useState(false);
  useEffect(() => setMode(pick.kind === "pod" ? "logs" : "describe"), [pick.kind, pick.name]);
  const read = () => {
    setText(null);
    if (mode === "logs") ask("kube.logs", { ...scope, pod: pick.name, container: ctr }, (r) => setText(r as KubeText));
    else ask("kube.describe", { ...scope, kind: pick.kind, name: pick.name }, (r) => setText(r as KubeText));
  };
  useEffect(read, [mode, ctr, pick.kind, pick.name, scope.context, scope.namespace]);
  return (
    <section class="kb-detail" aria-label={pick.name}>
      <div class="kb-bar">
        <span class="kb-what trunc" dir="ltr">
          {pick.kind}/{pick.name}
        </span>
        {pick.kind === "pod" && (
          <button type="button" class={`tb${mode === "logs" ? " is-on" : ""}`} onClick={() => setMode("logs")}>
            {t("ctr.logs")}
          </button>
        )}
        <button type="button" class={`tb${mode === "describe" ? " is-on" : ""}`} onClick={() => setMode("describe")}>
          {t("kube.describe")}
        </button>
        {mode === "logs" && (pick.containers?.length ?? 0) > 1 && (
          <select class="kb-sel" aria-label={t("kube.container")} value={ctr} onChange={(e) => setCtr((e.currentTarget as HTMLSelectElement).value)}>
            {pick.containers?.map((c) => (
              <option key={c}>{c}</option>
            ))}
          </select>
        )}
        <IconBtn icon="refresh" act="detail-refresh" label={t("ctr.refresh")} onClick={read} />
        {act && pick.kind === "deployment" && (
          <button type="button" class="tb" data-act="restart" onClick={() => setRestart(true)}>
            {t("kube.restart")}
          </button>
        )}
      </div>
      {!text ? (
        <SkeletonRows rows={6} />
      ) : text.lines.length === 0 ? (
        <Note>{t("svc.logs_none")}</Note>
      ) : (
        <div class="tres-rows kb-text" dir="ltr">
          {text.lines.map((l, i) => (
            <TermRow key={i} text={l} />
          ))}
        </div>
      )}
      {restart && (
        <AskLaptop
          label={t("ctr.restart")}
          text={t("kube.restart_q", { name: pick.name, ns: scope.namespace })}
          send={() => link.request("kube.restart", { ...scope, kind: "deployment", name: pick.name })}
          onDone={() => setRestart(false)}
        />
      )}
    </section>
  );
}

export function KubeView() {
  const act = canAct();
  const [ctxs, setCtxs] = useState<KubeContexts | null>(null);
  const [scope, setScope] = useState<{ context: string; namespace: string } | null>(null);
  const [nss, setNss] = useState<readonly string[]>([]);
  const [list, setList] = useState<KubeList | null>(null);
  const [err, setErr] = useState("");
  const [pick, setPick] = useState<Pick | null>(null);
  const now = useNow(true, 30_000);
  useEffect(() => {
    if (!live.value) return;
    link.request("kube.contexts").then(
      (r) => {
        const c = r as KubeContexts;
        setCtxs(c);
        setScope({ context: c.context ?? "", namespace: c.namespace ?? "" });
      },
      (e: unknown) => setErr(e instanceof Error ? e.message : t("err.internal")),
    );
  }, [live.value]);
  useEffect(() => {
    if (!scope) return;
    ask("kube.namespaces", { context: scope.context }, (r) => setNss((r as { namespaces: string[] }).namespaces ?? []));
  }, [scope?.context]);
  const load = () => {
    if (!scope || !live.value) return;
    link.request("kube.list", scope).then(
      (r) => {
        setList(r as KubeList);
        setErr("");
      },
      (e: unknown) => {
        setErr(e instanceof Error ? e.message : "");
        gitFailed(e);
      },
    );
  };
  useEffect(() => {
    load();
    const id = setInterval(load, EVERY_MS);
    return () => clearInterval(id);
  }, [scope?.context, scope?.namespace, live.value]);
  const section = (label: StringKey, rows: ComponentChildren[]) => (
    <section class="kb-sec" aria-label={t(label)}>
      <h2 class="svc-head">
        {t(label)} <span class="rp-count">{rows.length}</span>
      </h2>
      {rows.length === 0 ? <Note>{t("kube.none")}</Note> : rows}
    </section>
  );
  const is = (k: Kind, n: string) => pick?.kind === k && pick.name === n;
  const ago = (ms?: number) => (ms ? age(ms, now) : "");
  const actions = (
    <span class="rt kb-scope">
      <select class="kb-sel" aria-label={t("kube.context")} value={scope?.context ?? ""} onChange={(e) => (setPick(null), setScope({ context: (e.currentTarget as HTMLSelectElement).value, namespace: "" }))}>
        {ctxs?.contexts.map((c) => (
          <option key={c}>{c}</option>
        ))}
      </select>
      <select class="kb-sel" aria-label={t("kube.ns")} value={list?.namespace ?? scope?.namespace ?? ""} onChange={(e) => scope && (setPick(null), setScope({ ...scope, namespace: (e.currentTarget as HTMLSelectElement).value }))}>
        {(nss.length ? nss : [list?.namespace ?? scope?.namespace ?? ""]).map((n) => (
          <option key={n}>{n}</option>
        ))}
      </select>
      <IconBtn icon="refresh" act="refresh" label={t("ctr.refresh")} onClick={load} />
    </span>
  );
  return (
    <SubView title={t("act.kube")} name="kube" actions={actions}>
      {err && !list ? (
        <Note>{err}</Note>
      ) : !list ? (
        <SkeletonRows rows={6} />
      ) : (
        <div class={`kb${pick ? " has-detail" : ""}`}>
          <div class="kb-lists">
            {err && <p class="dbg-hint">{err}</p>}
            {Object.entries(list.errors ?? {}).map(([k, v]) => (
              <p key={k} class="dbg-hint" dir="auto">
                {k}: {v}
              </p>
            ))}
            {section(
              "kube.pods",
              list.pods.map((p) => (
                <Row key={p.name} p={{ kind: "pod", name: p.name }} on={is("pod", p.name)} facts={[p.ready, p.status, String(p.restarts), ago(p.created)]} onPick={() => setPick({ kind: "pod", name: p.name, containers: p.containers })} />
              )),
            )}
            {section(
              "kube.deploys",
              list.deployments.map((d) => (
                <Row key={d.name} p={{ kind: "deployment", name: d.name }} on={is("deployment", d.name)} facts={[d.ready, String(d.available), ago(d.created)]} onPick={() => setPick({ kind: "deployment", name: d.name })} />
              )),
            )}
            {section(
              "act.services",
              list.services.map((s) => (
                <Row key={s.name} p={{ kind: "service", name: s.name }} on={is("service", s.name)} facts={[s.type, s.cluster_ip ?? "", s.ports ?? ""]} onPick={() => setPick({ kind: "service", name: s.name })} />
              )),
            )}
            {section(
              "kube.jobs",
              list.jobs.map((j) => (
                <Row key={j.name} p={{ kind: "job", name: j.name }} on={is("job", j.name)} facts={[j.status, j.completions, ago(j.created)]} onPick={() => setPick({ kind: "job", name: j.name })} />
              )),
            )}
          </div>
          {pick && scope && <Detail scope={{ context: list.context, namespace: list.namespace }} pick={pick} act={act} />}
        </div>
      )}
    </SubView>
  );
}
