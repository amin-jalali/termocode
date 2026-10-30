// RUN AND DEBUG (WEBP-4): the laptop's debug session (the debug topic)
// as its RUN AND DEBUG view shows it. The head starts the chosen launch
// configuration (or runs it without the debugger); while a session runs,
// a toolbar steps it. VARIABLES opens rows (debug.expand), WATCH adds and
// removes expressions, CALL STACK picks a frame and shows its line,
// BREAKPOINTS lists them by file and line, LOADED SCRIPTS asks the
// debugger. #/more/debug
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useRef, useState } from "preact/hooks";
import type { DebugBreakpoint, DebugSnap, DebugSources, DebugVar } from "../../../../../webproto/proto";
import { goTo, panelTab } from "../../lib/lsp";
import { setPanes } from "../../lib/workbench";
import { link, live, store } from "../../session";
import { readDebugSnap } from "../../state/run";
import { t, type StringKey } from "../../strings";
import { Cod } from "../../ui/icons";
import { SubView } from "../more/SubView";
import { ask, canAct, IconBtn, Note, Pane } from "./bits";

const panes = signal<Record<string, boolean>>({ vars: true, watch: true, stack: true, bps: true, scripts: false });
/** closedScopes are the scopes the page folded (the rest follow the hub). */
const closedScopes = signal<ReadonlySet<string>>(new Set());

function setPane(id: string, open: boolean): void {
  panes.value = { ...panes.peek(), [id]: open };
}

/** debugSnap is the debug topic now. */
export function debugSnap(): DebugSnap | null {
  return readDebugSnap(store.topic("debug").value?.data);
}

/** showConsole shows the DEBUG CONSOLE panel tab. */
export function showConsole(): void {
  panelTab.value = "debug-console";
  setPanes({ panel: true });
}

const STEPS: readonly { op: string; icon: string; label: StringKey; keys: string; paused?: boolean }[] = [
  { op: "continue", icon: "debug-continue", label: "dbg.continue", keys: "F5", paused: true },
  { op: "pause", icon: "debug-pause", label: "runs.pause", keys: "F6" },
  { op: "step_over", icon: "debug-step-over", label: "dbg.step_over", keys: "F10", paused: true },
  { op: "step_into", icon: "debug-step-into", label: "dbg.step_into", keys: "F11", paused: true },
  { op: "step_out", icon: "debug-step-out", label: "dbg.step_out", keys: "Shift+F11", paused: true },
  { op: "restart", icon: "debug-restart", label: "ctr.restart", keys: "Ctrl+Shift+F5" },
  { op: "stop", icon: "debug-stop", label: "stop", keys: "Shift+F5" },
];

/** Toolbar steps the live session (the laptop's own steps). */
export function Toolbar({ s }: { s: DebugSnap }) {
  return (
    <span class="dbg-bar rt" role="toolbar" aria-label={t("dbg.toolbar")}>
      {STEPS.filter((x) => (x.op !== "continue" || s.stopped) && (x.op !== "pause" || !s.stopped)).map((x) => (
        <IconBtn key={x.op} icon={x.icon} act={x.op} label={t(x.label)} keys={x.keys} off={x.paused === true && !s.stopped} onClick={() => ask("debug.control", { op: x.op })} />
      ))}
    </span>
  );
}

function Value({ v }: { v: DebugVar }) {
  return (
    <span class="dv-val trunc" dir="ltr">
      <span class="dv-name">{v.name}</span>
      <span class="dv-sep">: </span>
      <span class={`dv-v${/^-?[\d.]+$/.test(v.value) ? " is-num" : /^["'`]/.test(v.value) ? " is-str" : ""}`}>{v.value}</span>
    </span>
  );
}

function VarRows({ s, parent, vars, depth }: { s: DebugSnap; parent: string; vars: readonly DebugVar[]; depth: number }) {
  return (
    <>
      {vars.map((v) => {
        const key = parent + "/" + v.name;
        const open = v.ref ? s.open.includes(key) : false;
        const kids = open ? s.children.find((c) => c.key === key) : undefined;
        return (
          <div key={key} role="treeitem" aria-expanded={v.ref ? (open ? "true" : "false") : undefined}>
            <div class="dv-row" style={{ "--d": depth }} data-key={key} title={v.type ? `${v.value} (${v.type})` : v.value} onClick={() => v.ref && ask("debug.expand", { key, open: !open })}>
              <span class="tx-twist">{v.ref ? <Cod name={open ? "chevron-down" : "chevron-right"} /> : null}</span>
              <Value v={v} />
            </div>
            {open && kids && (kids.error ? <Note>{kids.error}</Note> : <VarRows s={s} parent={key} vars={kids.vars} depth={depth + 1} />)}
          </div>
        );
      })}
    </>
  );
}

function Variables({ s }: { s: DebugSnap }) {
  if (!s.active) return <Note>{t("dbg.not_debugging")}</Note>;
  if (!s.stopped) return <Note>{t("dbg.running")}</Note>;
  if (s.scopes.length === 0) return <Note>{t("loading")}</Note>;
  return (
    <div role="tree" aria-label={t("dbg.vars")}>
      {s.scopes.map((sc) => {
        const open = !closedScopes.value.has(sc.key) && (!sc.expensive || s.open.includes(sc.key));
        const vars = sc.loaded ? sc.vars : s.children.find((c) => c.key === sc.key)?.vars;
        const flip = () => {
          const n = new Set(closedScopes.peek());
          if (open) n.add(sc.key);
          else n.delete(sc.key);
          closedScopes.value = n;
          ask("debug.expand", { key: sc.key, open: !open });
        };
        return (
          <div key={sc.key} role="treeitem" aria-expanded={open ? "true" : "false"}>
            <div class="dv-row dv-scope" data-key={sc.key} onClick={flip}>
              <span class="tx-twist">
                <Cod name={open ? "chevron-down" : "chevron-right"} />
              </span>
              <span class="trunc">{sc.name}</span>
            </div>
            {open && (vars ? <VarRows s={s} parent={sc.key} vars={vars} depth={1} /> : <Note>{t("loading")}</Note>)}
          </div>
        );
      })}
    </div>
  );
}

function Watch({ s, act }: { s: DebugSnap; act: boolean }) {
  const [adding, setAdding] = useState(false);
  return (
    <div class="dw">
      {s.watches.map((w) => (
        <div key={w.expr} class="dv-row" data-expr={w.expr}>
          <span class="tx-twist" />
          <span class="dv-val trunc" dir="ltr">
            <span class="dv-name">{w.expr}</span>
            <span class="dv-sep">: </span>
            <span class={`dv-v${w.error ? " is-err" : ""}`}>{s.stopped ? w.value || "…" : t("dbg.na")}</span>
          </span>
          {act && (
            <span class="tx-acts">
              <IconBtn icon="close" act="unwatch" label={t("dbg.watch_remove")} onClick={() => ask("debug.watch", { op: "remove", expr: w.expr })} />
            </span>
          )}
        </div>
      ))}
      {act &&
        (adding ? (
          <input
            class="dw-input field-box"
            autoFocus
            dir="ltr"
            aria-label={t("dbg.watch_add")}
            placeholder={t("dbg.watch_add")}
            onKeyDown={(e) => {
              const v = (e.currentTarget as HTMLInputElement).value.trim();
              if (e.key === "Escape") setAdding(false);
              if (e.key === "Enter") {
                setAdding(false);
                if (v) ask("debug.watch", { op: "add", expr: v });
              }
            }}
            onBlur={() => setAdding(false)}
          />
        ) : (
          s.watches.length === 0 && <Note>{t("dbg.watch_none")}</Note>
        ))}
      {act && !adding && (
        <span class="dw-add-wrap">
          <IconBtn icon="add" act="watch-add" label={t("dbg.watch_add")} onClick={() => setAdding(true)} />
        </span>
      )}
    </div>
  );
}

function Stack({ s, act }: { s: DebugSnap; act: boolean }) {
  if (!s.active) return <Note>{t("dbg.not_debugging")}</Note>;
  if (!s.stopped) return <Note>{t("dbg.running")}</Note>;
  const multi = s.threads.length > 1;
  return (
    <div role="list" aria-label={t("dbg.stack")}>
      {multi &&
        s.threads
          .filter((th) => th.id !== s.thread)
          .map((th) => (
            <div key={th.id} class="dv-row dv-thread" role="listitem">
              <span class="tx-twist" />
              <span class="trunc">{th.name}</span>
              <span class="tx-desc">{th.stopped ? t("dbg.paused") : t("dbg.running")}</span>
            </div>
          ))}
      {s.frames.map((f) => (
        <div
          key={f.id}
          role="listitem"
          class={`dv-row dv-frame${f.id === s.frame ? " is-picked" : ""}${f.outside ? " is-out" : ""}`}
          data-frame={f.id}
          onClick={() => {
            if (act && f.id !== s.frame) ask("debug.frame", { id: f.id });
            if (f.path) goTo(f.path, Math.max(0, f.line - 1), Math.max(0, (f.col ?? 1) - 1));
          }}
        >
          <span class="tx-twist">{f.id === s.frame ? <Cod name="debug-stackframe" /> : null}</span>
          <span class="dv-fn trunc" dir="ltr">
            {f.name}
          </span>
          <span class="dv-at trunc" dir="ltr">
            {(f.path ? f.path.split("/").pop() : f.file) ?? ""}:{f.line}
          </span>
        </div>
      ))}
    </div>
  );
}

/** bpIcon is a breakpoint's glyph as the editor draws it. */
export function bpIcon(b: DebugBreakpoint): string {
  if (b.verified === "no") return "debug-breakpoint-unverified";
  if (b.log) return "debug-breakpoint-log";
  if (b.condition || b.hit) return "debug-breakpoint-conditional";
  return "debug-breakpoint";
}

function Breakpoints({ s, act }: { s: DebugSnap; act: boolean }) {
  if (s.breakpoints.length === 0) return <Note>{t("dbg.bp_none")}</Note>;
  return (
    <div role="list" aria-label={t("dbg.bps")}>
      {s.breakpoints.map((b) => {
        const name = b.path.split("/").pop() ?? b.path;
        const dir = b.path.slice(0, Math.max(0, b.path.length - name.length - 1));
        return (
          <div key={b.path + ":" + b.line} role="listitem" class="dv-row dv-bp" data-bp={`${b.path}:${b.line}`} onClick={() => goTo(b.path, b.line - 1, 0)} title={b.text}>
            <span class={`dv-bp-icon${b.verified === "no" ? " is-off" : ""}`} aria-hidden="true">
              <Cod name={bpIcon(b)} />
            </span>
            <span class="trunc" dir="ltr">
              {name}
            </span>
            <span class="tx-desc trunc" dir="ltr">
              {b.condition ? t("dbg.bp_if", { cond: b.condition }) : b.log ? t("dbg.bp_log", { msg: b.log }) : dir}
            </span>
            <span class="dv-line">{b.line}</span>
            {act && (
              <span class="tx-acts">
                <IconBtn icon="close" act="bp-remove" label={t("dbg.bp_remove")} onClick={() => ask("debug.bp", { op: "remove", path: b.path, line: b.line })} />
              </span>
            )}
          </div>
        );
      })}
    </div>
  );
}

function Scripts({ s }: { s: DebugSnap }) {
  const [res, setRes] = useState<DebugSources | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    if (!s.active || !live.value) return;
    setErr("");
    link.request("debug.sources").then(
      (r) => setRes(r as DebugSources),
      (e: unknown) => setErr(e instanceof Error ? e.message : ""),
    );
  }, [s.active, s.name, s.stopped]);
  if (!s.active) return <Note>{t("dbg.not_debugging")}</Note>;
  if (err) return <Note>{err}</Note>;
  if (!res) return <Note>{t("loading")}</Note>;
  if (res.unsupported) return <Note>{t("dbg.scripts_no")}</Note>;
  return (
    <div role="list" aria-label={t("dbg.scripts")}>
      {res.sources.map((x, i) => (
        <div key={i} role="listitem" class={`dv-row${x.path ? "" : " is-out"}`} onClick={() => x.path && goTo(x.path, 0, 0)}>
          <span class="tx-twist" />
          <span class="trunc" dir="ltr">
            {x.name}
          </span>
          {x.path && (
            <span class="tx-desc trunc" dir="ltr">
              {x.path}
            </span>
          )}
        </div>
      ))}
    </div>
  );
}

function Head({ s, act }: { s: DebugSnap; act: boolean }) {
  const cfg = s.configs.find((c) => c.name === s.selected);
  return (
    <div class="dbg-head">
      {act && (
        <div class="dbg-start">
          <IconBtn
            icon={cfg && !cfg.debug ? "run" : "debug-start"}
            act="start"
            label={cfg && !cfg.debug ? t("dbg.run_cfg") : t("dbg.start")}
            keys="F5"
            off={!cfg || s.active}
            onClick={() => ask(cfg?.debug ? "debug.start" : "debug.run", { config: s.selected ?? "" })}
          />
          <select
            class="dbg-cfg"
            aria-label={t("dbg.config")}
            value={s.selected ?? ""}
            disabled={s.configs.length === 0}
            onChange={(e) => ask("debug.select", { config: (e.currentTarget as HTMLSelectElement).value })}
          >
            {s.configs.length === 0 && <option value="">{t("dbg.no_config")}</option>}
            {s.configs.map((c) => (
              <option key={c.name} value={c.name}>
                {c.name}
              </option>
            ))}
          </select>
          {cfg?.debug && <IconBtn icon="run" act="run" label={t("dbg.run_no_debug")} keys="Ctrl+F5" off={s.active} onClick={() => ask("debug.run", { config: cfg.name })} />}
          <IconBtn icon="debug-console" act="console" label={t("dbg.console")} onClick={showConsole} />
        </div>
      )}
      {s.active && (
        <div class="dbg-live">
          {act && <Toolbar s={s} />}
          <span class={`dbg-status${s.stopped ? " is-paused" : ""}`} role="status">
            {s.status || s.name}
          </span>
        </div>
      )}
      {s.hint && <p class="dbg-hint">{s.hint}</p>}
      {s.config_error && <Note>{s.config_error}</Note>}
    </div>
  );
}

export function DebugView() {
  useEffect(() => link.subscribe("debug"), []);
  const s = debugSnap();
  const act = canAct();
  const p = panes.value;
  return (
    <SubView title={t("act.run")} name="debug">
      {!s ? (
        <Note>{t("loading")}</Note>
      ) : (
        <div class="dbg">
          <Head s={s} act={act} />
          <Pane id="vars" title={t("dbg.vars")} open={p.vars} onToggle={() => setPane("vars", !p.vars)}>
            <Variables s={s} />
          </Pane>
          <Pane id="watch" title={t("dbg.watch")} open={p.watch} onToggle={() => setPane("watch", !p.watch)}>
            <Watch s={s} act={act} />
          </Pane>
          <Pane id="stack" title={t("dbg.stack")} open={p.stack} onToggle={() => setPane("stack", !p.stack)}>
            <Stack s={s} act={act} />
          </Pane>
          <Pane
            id="bps"
            title={t("dbg.bps")}
            open={p.bps}
            count={s.breakpoints.length}
            onToggle={() => setPane("bps", !p.bps)}
            actions={act && s.breakpoints.length > 0 && <ClearAll />}
          >
            <Breakpoints s={s} act={act} />
          </Pane>
          <Pane id="scripts" title={t("dbg.scripts")} open={p.scripts} onToggle={() => setPane("scripts", !p.scripts)}>
            <Scripts s={s} />
          </Pane>
        </div>
      )}
    </SubView>
  );
}

/** ClearAll removes every breakpoint after an in-row question (Cancel
 * first and focused). */
function ClearAll() {
  const [asking, setAsking] = useState(false);
  const safe = useRef<HTMLButtonElement | null>(null);
  useEffect(() => {
    if (asking) safe.current?.focus();
  }, [asking]);
  if (!asking) return <IconBtn icon="close-all" act="bp-clear" label={t("dbg.bp_clear")} onClick={() => setAsking(true)} />;
  return (
    <span class="dbg-ask" role="group" aria-label={t("dbg.bp_clear_q")}>
      <span>{t("dbg.bp_clear_q")}</span>
      <button type="button" class="btn btn-neutral btn-small" ref={safe} onClick={() => setAsking(false)}>
        {t("cancel")}
      </button>
      <button
        type="button"
        class="btn btn-danger btn-small"
        onClick={() => {
          setAsking(false);
          ask("debug.bp", { op: "clear" });
        }}
      >
        {t("dbg.bp_clear")}
      </button>
    </span>
  );
}
