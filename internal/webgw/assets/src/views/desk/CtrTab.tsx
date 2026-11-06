// A container read in an editor tab (desktop): an inspect as JSON (env
// values masked by the laptop), an image's layers, one stats sample, the
// disk usage, a Docker Hub search or a repository's tags. A search
// result or a tag offers Pull and Tags; pulling asks the laptop.
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useState } from "preact/hooks";
import type { ContainerInspect, ContainerStats, DiskUsage, ImageHistory, ImageSearch, RemoteTags } from "../../../../../webproto/proto";
import { showFile } from "../../lib/workbench";
import { ctrTab, type CtrWhat } from "../../lib/vtabs";
import { link } from "../../session";
import { t } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { Cod } from "../../ui/icons";
import { Act } from "../../ui/pane";
import { gitErrText } from "../git/errors";

type Load = { s: "loading" } | { s: "ok"; v: unknown } | { s: "err"; msg: string };

/** reads keeps each tab's last answer, so a tab drawn again (another
 * group, a new route) shows it without asking again. */
const reads = signal<ReadonlyMap<string, Load>>(new Map());

function setRead(key: string, l: Load): void {
  const n = new Map(reads.peek());
  n.set(key, l);
  reads.value = n;
}

/** ctrRead is the op and args of a tab. */
function ctrRead(what: CtrWhat, arg: string): [string, unknown] {
  if (what.startsWith("inspect-")) return ["containers.inspect", { kind: what.slice(8), id: arg }];
  if (what === "history") return ["containers.history", { ref: arg }];
  if (what === "stats") return ["containers.stats", { id: arg }];
  if (what === "search") return ["containers.search", { term: arg }];
  if (what === "tags") return ["containers.tags", { ref: arg }];
  return ["containers.disk_usage", {}];
}

/** CtrTab is one container read. */
export function CtrTab({ what, arg, label }: { what: CtrWhat; arg: string; label: string; slot: string }) {
  const key = what + "\n" + arg;
  const l = reads.value.get(key) ?? { s: "loading" };
  const [pull, setPull] = useState("");
  const read = () => {
    setRead(key, { s: "loading" });
    const [op, args] = ctrRead(what, arg);
    link.request(op, args).then(
      (v) => setRead(key, { s: "ok", v }),
      (e: unknown) => setRead(key, { s: "err", msg: gitErrText(e) }),
    );
  };
  useEffect(() => {
    if (!reads.peek().has(key)) read();
  }, [key]);
  const pullRow = (ref: string) => <Act icon="cloud-download" act="pull" label={t("ctr.pull")} onClick={() => setPull(ref)} />;
  let body: h.JSX.Element;
  if (l.s === "loading") body = <p class="ctrt-note">{t("loading")}</p>;
  else if (l.s === "err") body = <p class="ctrt-note c-err">{l.msg}</p>;
  else if (what.startsWith("inspect-")) {
    const v = l.v as ContainerInspect;
    body = (
      <>
        {(v.masked ?? 0) > 0 && <p class="ctrt-note">{t("ctr.masked", { n: v.masked ?? 0 })}</p>}
        <pre class="ctrt-pre" dir="ltr" tabIndex={0}>{v.text}</pre>
      </>
    );
  } else if (what === "history") {
    const v = l.v as ImageHistory;
    body = (
      <table class="mcpd-table ctrt-table">
        <thead>
          <tr>
            <th scope="col">{t("ctr.created")}</th>
            <th scope="col">{t("ctr.step")}</th>
            <th scope="col">{t("ctr.size")}</th>
          </tr>
        </thead>
        <tbody>
          {v.rows.map((r, i) => (
            <tr key={i}>
              <td dir="ltr">{r.created}</td>
              <td dir="ltr" class="ctrt-by">{r.created_by}</td>
              <td dir="ltr">{r.size}</td>
            </tr>
          ))}
        </tbody>
      </table>
    );
  } else if (what === "stats") {
    const v = l.v as ContainerStats;
    const rows: [string, string][] = [[t("ctr.cpu"), v.cpu], [t("ctr.mem"), `${v.mem} (${v.mem_perc})`], [t("ctr.net"), v.net], [t("ctr.block"), v.block], [t("ctr.pids"), v.pids]];
    body = (
      <table class="mcpd-table ctrt-table">
        <tbody>
          {rows.map(([k, x]) => (
            <tr key={k}>
              <th scope="row">{k}</th>
              <td dir="ltr">{x}</td>
            </tr>
          ))}
        </tbody>
      </table>
    );
  } else if (what === "search") {
    const v = l.v as ImageSearch;
    body =
      v.results.length === 0 ? (
        <p class="ctrt-note">{t("ctr.no_results")}</p>
      ) : (
        <ul class="ctrt-list">
          {v.results.map((r) => (
            <li key={r.name} class="ctrt-item" data-ref={r.name}>
              <Cod name={r.official ? "check" : "package"} />
              <span class="ctrt-name" dir="ltr">{r.name}</span>
              <span class="tr-desc">{r.description}</span>
              <span class="count">{r.stars ?? 0}</span>
              <Act icon="tag" act="tags" label={t("ctr.tags")} onClick={() => showFile(ctrTab("tags", r.name), true)} />
              {pullRow(r.name)}
            </li>
          ))}
        </ul>
      );
  } else if (what === "tags") {
    const v = l.v as RemoteTags;
    body = (
      <>
        {v.note && <p class="ctrt-note">{v.note}</p>}
        <ul class="ctrt-list">
          {v.tags.map((x) => (
            <li key={x} class="ctrt-item" data-ref={v.repo + ":" + x}>
              <Cod name="tag" />
              <span class="ctrt-name" dir="ltr">{x}</span>
              {pullRow(v.repo + ":" + x)}
            </li>
          ))}
        </ul>
        {v.truncated && <p class="ctrt-note">{t("ctr.more_tags")}</p>}
      </>
    );
  } else {
    body = <pre class="ctrt-pre" dir="ltr" tabIndex={0}>{(l.v as DiskUsage).lines.join("\n")}</pre>;
  }
  return (
    <section class="view ctrt" data-view="ctr-tab" data-what={what} aria-label={label}>
      <header class="ctrt-head">
        <h1 dir="ltr">{label}</h1>
        <Act icon="refresh" label={t("ctr.refresh")} onClick={read} />
      </header>
      {body}
      {pull && (
        <AskLaptop label={t("ctr.pull")} text={t("ctr.pull_q", { ref: pull })} send={() => link.request("containers.image", { verb: "pull", ref: pull })} onDone={() => setPull("")} />
      )}
    </section>
  );
}
