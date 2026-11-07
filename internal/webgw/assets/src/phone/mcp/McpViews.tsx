// The phone's MCP default views (MOBUX-14, mobile plan 3.11.6): one
// searchable list of every tool of every MCP server the laptop knows,
// each row with its shape mark (▦ table, ☰ list, ▭ card, ▤ detail,
// ¶ text, { } raw). A row opens its view: a short form from the tool's
// scalar inputs, then the result drawn as a typed block, with the raw
// JSON one tap away ("{ } Raw"). A tool that may not run from the phone
// (not read-only, hints not trusted, outside the profile, not trusted
// on the laptop) says why in one line and offers "Ask the AI" instead,
// so the AI's own cards apply. Nothing here starts a server or trusts
// one: the laptop decides and fetches. Phone layout; 44 px targets;
// English only.
//
// Only lazy phone chunks import this file (the Home and its "All tools"
// menu, MOBUX-5), so app.js does not grow. The page hands it request
// and the role, so this file holds no link of its own.
import { h, type ComponentChildren } from "preact";
import { useEffect, useState } from "preact/hooks";
import { BlockView } from "../../ui/Blocks";
import { Button } from "../../ui/Button";
import { Segmented } from "../../ui/Segmented";
import { ms } from "./strings";
import {
  checkArgs,
  isProblem,
  OP_RUN,
  OP_VIEWS,
  prettyRaw,
  rowsOf,
  shapeOf,
  type MCPViewData,
  type MCPViewInfo,
  type MCPViewParam,
  type MCPViews as Views,
  type Row,
} from "./mcpops";

export interface McpViewsProps {
  /** request sends one op to the laptop (the page's link.request). */
  request(op: string, args?: unknown): Promise<unknown>;
  /** canRun: the link is up and the role is controller. */
  canRun: boolean;
  /** onAsk puts a request for the AI into the composer. */
  onAsk(text: string): void;
  /** open is a view id to open at once (a deep link or a pinned row). */
  open?: string;
}

function errText(e: unknown): string {
  return e instanceof Error && e.message ? e.message : ms("mcpv.failed");
}

/** McpViews is the list, or the open view. */
export function McpViews({ request, canRun, onAsk, open }: McpViewsProps) {
  const [data, setData] = useState<Views | null>(null);
  const [err, setErr] = useState("");
  const [q, setQ] = useState("");
  const [sel, setSel] = useState<Row | null>(null);

  const load = () => {
    setErr("");
    request(OP_VIEWS).then(
      (d) => {
        const v = d as Views;
        setData(v);
        if (open && !sel) {
          const hit = rowsOf(v, "").find((r) => r.view.id === open);
          if (hit) setSel(hit);
        }
      },
      (e: unknown) => setErr(errText(e)),
    );
  };
  useEffect(load, []);

  if (sel) return <ViewPage row={sel} request={request} canRun={canRun} onAsk={onAsk} onBack={() => setSel(null)} />;

  const rows = rowsOf(data, q);
  const servers = data?.servers ?? [];
  return (
    <section class="mcpv" aria-label={ms("mcpv.title")}>
      <h2 class="mcpv-title">{ms("mcpv.title")}</h2>
      <input
        class="mcpv-search"
        type="search"
        value={q}
        placeholder={ms("mcpv.search")}
        aria-label={ms("mcpv.search")}
        onInput={(e) => setQ((e.currentTarget as HTMLInputElement).value)}
      />
      {err && (
        <p class="mcpv-err" role="alert">
          <span aria-hidden="true">✗ </span>
          {err} <Button small onClick={load}>{ms("mcpv.retry")}</Button>
        </p>
      )}
      {!data && !err && <div class="mcpv-skel" aria-busy="true" aria-label={ms("mcpv.running")} />}
      {data && servers.length === 0 && <p class="mcpv-empty">{ms("mcpv.none")}</p>}
      {data && servers.length > 0 && rows.length === 0 && q && <p class="mcpv-empty">{ms("mcpv.no_match")}</p>}
      {servers
        .filter((s) => !s.known && !q)
        .map((s) => (
          <p key={"u:" + s.name} class="mcpv-unknown">
            <span class="mcpv-server">{s.name}</span> <span aria-hidden="true">○ </span>
            {s.off ? ms("mcpv.off") : ms("mcpv.unknown")}
          </p>
        ))}
      <ul class="mcpv-list">
        {rows.map((r) => (
          <ToolRow key={r.view.id} row={r} onOpen={() => setSel(r)} />
        ))}
      </ul>
    </section>
  );
}

function ToolRow({ row, onOpen }: { row: Row; onOpen(): void }) {
  const v = row.view;
  const shape = shapeOf(v.layout);
  return (
    <li>
      <button type="button" class={`mcpv-row${v.run ? "" : " is-ask"}`} onClick={onOpen}>
        <span class="mcpv-mark" role="img" aria-label={ms(shape.key)}>
          {shape.mark}
        </span>
        <span class="mcpv-row-text">
          <span class="mcpv-row-title" dir="auto">
            {v.title}
          </span>
          <span class="mcpv-row-sub" dir="auto">
            {row.server} · {v.tool}
            {!v.run && v.why ? ` · ${v.why}` : ""}
          </span>
        </span>
        {!v.run && <span class="mcpv-ask-tag">{ms("mcpv.ask_ai")}</span>}
      </button>
    </li>
  );
}

type Show = "view" | "raw";

function ViewPage({ row, request, canRun, onAsk, onBack }: { row: Row; request: McpViewsProps["request"]; canRun: boolean; onAsk(text: string): void; onBack(): void }) {
  const v = row.view;
  const [values, setValues] = useState<Record<string, string>>({});
  const [why, setWhy] = useState("");
  const [busy, setBusy] = useState(false);
  const [res, setRes] = useState<MCPViewData | null>(null);
  const [show, setShow] = useState<Show>("view");
  const askText = ms("mcpv.ask_text", { tool: v.tool, server: row.server });
  const typed = v.typed === "schema" ? ms("mcpv.typed_schema") : v.typed === "learned" ? ms("mcpv.typed_learned") : ms("mcpv.typed_none");

  const run = () => {
    const args = checkArgs(v, values);
    if (isProblem(args)) {
      setWhy(ms(args.key, { label: args.label }));
      return;
    }
    setWhy("");
    setBusy(true);
    request(OP_RUN, { view: v.id, args }).then(
      (d) => {
        setBusy(false);
        setRes(d as MCPViewData);
        setShow("view");
      },
      (e: unknown) => {
        setBusy(false);
        setWhy(errText(e));
      },
    );
  };
  // A view without params runs at once when it may.
  useEffect(() => {
    if (v.run && canRun && !(v.params ?? []).length) run();
  }, []);

  const ask = !v.run ? v.why || ms("mcpv.ask_ai") : res?.ask;
  return (
    <section class="mcpv mcpv-page" aria-label={v.title}>
      <Button kind="quiet" onClick={onBack} icon={<span aria-hidden="true">←</span>}>
        {ms("mcpv.back")}
      </Button>
      <h2 class="mcpv-title" dir="auto">
        {v.title}
      </h2>
      <p class="mcpv-row-sub">
        {row.server} · {v.tool} · <span aria-hidden="true">{shapeOf(v.layout).mark} </span>
        {typed}
      </p>
      {v.description && (
        <p class="mcpv-desc" dir="auto">
          {v.description}
        </p>
      )}
      {ask ? (
        <div class="mcpv-ask">
          <p>
            <span aria-hidden="true">ⓘ </span>
            {ask}
          </p>
          <Button kind="ai" wide onClick={() => onAsk(askText)}>
            {ms("mcpv.ask_ai")}
          </Button>
        </div>
      ) : (
        <form
          class="mcpv-form"
          onSubmit={(e) => {
            e.preventDefault();
            run();
          }}
        >
          {(v.params ?? []).map((p) => (
            <ParamField key={p.id} p={p} v={v} value={values[p.id] ?? ""} onChange={(x) => setValues({ ...values, [p.id]: x })} />
          ))}
          {why && (
            <p class="mcpv-err" role="alert">
              <span aria-hidden="true">✗ </span>
              {why}
            </p>
          )}
          {!canRun && <p class="mcpv-empty">{ms("mcpv.viewer")}</p>}
          <Button kind="primary" wide needsLink disabled={!canRun || busy} onClick={run}>
            {busy ? ms("mcpv.running") : res ? ms("mcpv.again") : ms("mcpv.run")}
          </Button>
        </form>
      )}
      {res && !res.ask && <Result res={res} show={show} onShow={setShow} />}
    </section>
  );
}

function Result({ res, show, onShow }: { res: MCPViewData; show: Show; onShow(s: Show): void }) {
  return (
    <div class="mcpv-result" aria-live="polite">
      <Segmented<Show>
        label={ms("mcpv.raw_label")}
        value={show}
        choices={[
          { value: "view", label: ms("mcpv.shaped") },
          { value: "raw", label: ms("mcpv.raw") },
        ]}
        onChange={onShow}
      />
      {show === "view" && res.block ? (
        <BlockView block={res.block} />
      ) : (
        <div class="mcpv-raw" role="group" aria-label={ms("mcpv.shape.json")}>
          <pre dir="ltr">{prettyRaw(res.raw ?? "")}</pre>
          {res.truncated && <p class="mcpv-empty">{ms("mcpv.raw_cut")}</p>}
        </div>
      )}
    </div>
  );
}

function ParamField({ p, v, value, onChange }: { p: MCPViewParam; v: MCPViewInfo; value: string; onChange(x: string): void }) {
  const label = p.required ? ms("mcpv.required", { label: p.label }) : p.label;
  const id = `mcpv-p-${p.id}`;
  const input = (e: Event) => onChange((e.currentTarget as HTMLInputElement | HTMLSelectElement).value);
  let control: ComponentChildren;
  if (p.kind === "enum" || p.kind === "bool") {
    const opts = p.kind === "bool" ? [
      { v: "true", l: ms("mcpv.yes") },
      { v: "false", l: ms("mcpv.no") },
    ] : (p.enum ?? []).map((x) => ({ v: x, l: x }));
    control = (
      <select id={id} class="mcpv-input" value={value} onChange={input}>
        <option value="">{ms("mcpv.pick")}</option>
        {opts.map((o) => (
          <option key={o.v} value={o.v}>
            {o.l}
          </option>
        ))}
      </select>
    );
  } else {
    const list = p.kind === "uri" && (v.resources ?? []).length ? `${id}-list` : undefined;
    control = (
      <span>
        <input
          id={id}
          class="mcpv-input"
          type="text"
          value={value}
          list={list}
          inputMode={p.kind === "int" || p.kind === "number" ? "decimal" : undefined}
          autoComplete="off"
          spellcheck={false}
          placeholder={p.kind === "uri" ? ms("mcpv.uri_pick") : undefined}
          onInput={input}
        />
        {list && (
          <datalist id={list}>
            {(v.resources ?? []).map((r) => (
              <option key={r.uri} value={r.uri}>
                {r.name}
              </option>
            ))}
          </datalist>
        )}
      </span>
    );
  }
  return (
    <div class="mcpv-field">
      <label for={id} dir="auto">
        {label}
      </label>
      {control}
      {p.hint && (
        <span class="mcpv-hint" dir="auto">
          {p.hint}
        </span>
      )}
    </div>
  );
}
