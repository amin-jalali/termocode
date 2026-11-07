// The "What can I do?" block, the MCP block (the Home's MCP chip) and the
// pinned tracker blocks (MOB-13). Phone layout only: one column, 44 px
// targets, every state as color + shape (● ◐ ✓ ✗). A tap on a prompt or
// a tool fills the composer; nothing is sent, nothing spends tokens.
// Data and runs stay on the laptop (catalog.ts).
import { h } from "./jsx";
import type { CatalogFeature, CatalogServer, TrackerPin } from "../../../../webproto/proto";
import { ago, gateText, newCatalog, reloadCatalog, reloadPins, runPin, search, STR, unpin, type CatalogHost, type CatalogState, type PinsState } from "./catalog";

export { newCatalog, newPins, stopPins } from "./catalog";

/** ErrorLine is the one-line error with Retry (MOBUX-21). */
function ErrorLine({ text, retry, host }: { text: string; retry: () => void; host: CatalogHost }) {
  return (
    <div class="hcat-err" role="alert">
      <span aria-hidden="true">✗</span> {text}
      <button type="button" class="hcat-btn" disabled={!host.live()} onClick={retry}>
        {STR.retry}
      </button>
    </div>
  );
}

function Skeleton() {
  return (
    <div class="hcat-skel" aria-busy="true" aria-label={STR.loading}>
      <span />
      <span />
      <span />
    </div>
  );
}

function Feature({ f, host }: { f: CatalogFeature; host: CatalogHost }) {
  return (
    <li class="hcat-feature">
      <div class="hcat-head">
        <span class="hcat-name">{f.title}</span>
        <span class="hcat-count">{STR.tools(f.tools, f.asks)}</span>
      </div>
      <div class="hcat-hint">{f.hint}</div>
      <div class="hcat-try">
        {f.try.map((p) => (
          <button type="button" class="hcat-chip" key={p} onClick={() => host.fill(p)} aria-label={`Put in the box: ${p}`}>
            {p}
          </button>
        ))}
        {f.view ? (
          <button type="button" class="hcat-chip is-view" onClick={() => host.openView(f.view!)}>
            {STR.openView} ›
          </button>
        ) : null}
      </div>
    </li>
  );
}

function Server({ s, host }: { s: CatalogServer; host: CatalogHost }) {
  const mark = s.state === "running" ? "●" : s.state === "off" || s.state === "invalid" ? "✗" : "◐";
  return (
    <li class="hcat-server">
      <div class="hcat-head">
        <span class="hcat-name">
          <span aria-hidden="true">{mark}</span> {s.name}
        </span>
        <span class="hcat-count">
          {s.state}
          {s.cached ? " · cached" : ""}
        </span>
      </div>
      {s.tools.length === 0 ? (
        <div class="hcat-hint">{s.known ? STR.noMatch : STR.noTools}</div>
      ) : (
        <ul class="hcat-tools">
          {s.tools.map((t) => (
            <li key={t.name}>
              <button type="button" class="hcat-row" disabled={t.gate === "off" || !s.trusted} onClick={() => host.fill(t.text)}>
                <span class="hcat-tool">{t.title || t.name}</span>
                <span class={`hcat-gate gate-${s.trusted ? t.gate : "off"}`}>{gateText(t, s)}</span>
                {t.hint ? <span class="hcat-hint">{t.hint}</span> : null}
              </button>
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}

/** CatalogBlock is "What can I do?" (feature "") or the MCP block ("mcp"). */
export function CatalogBlock({ host, st }: { host: CatalogHost; st: CatalogState }) {
  const l = st.load.value;
  const c = l.data;
  const title = st.feature === "mcp" ? STR.mcpTitle : STR.whatTitle;
  return (
    <section class="hblock hcat" aria-label={title}>
      <header class="hcat-title">
        <h2>{title}</h2>
      </header>
      {st.feature === "" ? <p class="hcat-hint">{STR.whatHint}</p> : null}
      <input
        class="hcat-search"
        type="search"
        placeholder={STR.search}
        aria-label={STR.search}
        maxLength={64}
        value={st.query.value}
        onInput={(e) => search(host, st, (e.currentTarget as HTMLInputElement).value)}
      />
      {l.error ? <ErrorLine text={l.error} host={host} retry={() => void reloadCatalog(host, st)} /> : null}
      {l.busy && !c ? <Skeleton /> : null}
      {c ? (
        <div class={l.busy ? "is-stale" : ""}>
          {c.features.length ? (
            <ul class="hcat-list">
              {c.features.map((f) => (
                <Feature key={f.id} f={f} host={host} />
              ))}
            </ul>
          ) : null}
          {c.servers.length ? (
            <ul class="hcat-list">
              {st.feature === "" ? <li class="hcat-sub">{STR.mcpTitle}</li> : null}
              {c.servers.map((s) => (
                <Server key={s.name} s={s} host={host} />
              ))}
            </ul>
          ) : null}
          {c.note ? <p class="hcat-hint">{c.note}</p> : null}
          {!c.features.length && !c.servers.length && !c.note ? <p class="hcat-hint">{STR.noMatch}</p> : null}
          {c.truncated ? <p class="hcat-hint">{STR.more}</p> : null}
        </div>
      ) : null}
    </section>
  );
}

function Pin({ p, host, st }: { p: TrackerPin; host: CatalogHost; st: PinsState }) {
  const run = st.runs.value[p.id];
  const b = run?.data;
  const ctl = host.controller();
  return (
    <section class="hblock htrack" aria-label={p.title}>
      <header class="hcat-head">
        <span class="hcat-name">📌 {p.title}</span>
        <span class="hcat-count">{b ? ago(b.at) : ""}</span>
      </header>
      <div class="hcat-hint">
        mcp.{p.server}.{p.tool}
        {p.args ? ` · ${p.args}` : ""}
      </div>
      {!p.runs ? (
        <div class="hcat-err">
          <span aria-hidden="true">✗</span> {STR.cannotRun}: {p.why}
        </div>
      ) : null}
      {run?.error ? <ErrorLine text={run.error} host={host} retry={() => void runPin(host, st, p)} /> : null}
      {run?.busy && !b ? <Skeleton /> : null}
      {b?.error ? (
        <div class="hcat-err">
          <span aria-hidden="true">✗</span> {b.error}
        </div>
      ) : null}
      {b && !b.error ? (
        <ul class={`hcat-tools${run?.busy ? " is-stale" : ""}`}>
          {b.rows.map((r, i) => (
            <li key={`${r.ref || ""}:${i}`} class="htrack-row">
              <span class="hcat-tool">{r.title || STR.noTitle}</span>
              {r.hint ? <span class="hcat-hint">{r.hint}</span> : null}
              {r.ref && ctl ? (
                <button type="button" class="hcat-btn" aria-label={`${STR.toChat}: ${r.title}`} onClick={() => host.toChat(r.title, `${p.server}:${r.ref}`)}>
                  {STR.toChat}
                </button>
              ) : null}
            </li>
          ))}
          {b.text ? <li class="hcat-hint">{b.text}</li> : null}
          {!b.rows.length && !b.text ? <li class="hcat-hint">{STR.empty}</li> : null}
          {b.truncated ? <li class="hcat-hint">{STR.more}</li> : null}
        </ul>
      ) : null}
      {ctl ? (
        <div class="hcat-actions">
          <button type="button" class="hcat-btn" onClick={() => unpin(host, st, p)}>
            {STR.unpin}
          </button>
          <button type="button" class="hcat-btn" disabled={!p.runs || !host.live() || run?.busy} onClick={() => void runPin(host, st, p)}>
            {STR.refresh}
          </button>
        </div>
      ) : null}
    </section>
  );
}

/** TrackerBlocks are the pinned tracker blocks at the top of the Home. */
export function TrackerBlocks({ host, st }: { host: CatalogHost; st: PinsState }) {
  const l = st.list.value;
  if (l.error && !l.data) return <ErrorLine text={l.error} host={host} retry={() => void reloadPins(host, st)} />;
  const pins = l.data?.pins ?? [];
  if (!pins.length) return null;
  return (
    <div class="htrack-list" aria-label={STR.pinnedTitle}>
      {pins.map((p) => (
        <Pin key={p.id} p={p} host={host} st={st} />
      ))}
    </div>
  );
}

/** PinsHint is the line the "What can I do?" block shows under MCP when
 * nothing is pinned yet. */
export function PinsHint() {
  return <p class="hcat-hint">{STR.pinHow}</p>;
}

/** chipBlock maps the Home's MCP and "What can I do?" chips (MOBUX-3 ids
 * "mcp", "whatcan") to their block; null for another chip. */
export function chipBlock(host: CatalogHost, id: string) {
  if (id !== "mcp" && id !== "whatcan") return null;
  const st = newCatalog(host, id === "mcp" ? "mcp" : "");
  return () => <CatalogBlock host={host} st={st} />;
}
