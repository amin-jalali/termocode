// Run and Debug on the phone (UI page 11). The page changes shape with
// the session instead of showing five empty panes:
//   idle    the launch configuration as one pill and a split ▶ Start ▾
//           (Run without debugging and the console under ▾), the
//           breakpoints with + Add breakpoint, and one "While debugging"
//           group (Watch; Variables · Call stack shown when paused).
//   running a floating control pill (pause, restart, stop) and the lists.
//   paused  the pill with continue and the steps, where it stopped, the
//           variables and the call stack as rows.
// The Debug console is docked at the bottom and opens as a sheet. A long
// press on a variable, a frame or a breakpoint gives Ask AI about this /
// Add to chat (a variable as the debug.state view of that one row, a
// frame or a breakpoint as its source line), Watch it and Copy value.
// The laptop does everything (the debug ops); a viewer only looks.
import type { Attach, DebugBreakpoint, DebugSnap, DebugVar } from "../../../../webproto/proto";
import { Fragment, h, H } from "./host";
import { aiItems, askAI, copy, Ic, openMenu, PressMenu, press, Section, useChat, viewAttach, type IconName } from "./kit";
import { tx, type PGKey } from "./strings";

/** snapOf is the debug topic (null: not here yet or odd). */
export function snapOf(data: unknown): DebugSnap | null {
  const d = data as Record<string, unknown> | null;
  return d && typeof d === "object" && Array.isArray(d.frames) && Array.isArray(d.breakpoints) && Array.isArray(d.console) && Array.isArray(d.configs) ? (data as DebugSnap) : null;
}

const base = (p: string) => p.split("/").pop() ?? p;

/** The control pill's buttons; paused: only when the program is paused,
 * running: only while it runs. */
const STEPS: readonly { op: string; icon: IconName; label: PGKey; paused?: boolean; running?: boolean; tone?: string }[] = [
  { op: "continue", icon: "play", label: "rd.continue", paused: true, tone: "go" },
  { op: "pause", icon: "pause", label: "rd.pause", running: true },
  { op: "step_over", icon: "stepOver", label: "rd.step_over", paused: true },
  { op: "step_into", icon: "stepInto", label: "rd.step_into", paused: true },
  { op: "step_out", icon: "stepOut", label: "rd.step_out", paused: true },
  { op: "restart", icon: "restart", label: "rd.restart" },
  { op: "stop", icon: "stop", label: "rd.stop", tone: "stop" },
];

const lineAttach = (path: string, line: number, label: string): Attach => ({ kind: "lines", path, from: line, to: line, label: label.slice(0, 120) });
const varAttach = (v: DebugVar) => viewAttach("debug.state", v.name, { filter: [{ field: "part", op: "eq", value: "variables" }, { field: "name", op: "eq", value: v.name }], layout: "detail" });

type Sheet = "" | "cfg" | "bp" | "watch" | "console";

export function DebugPage() {
  useChat();
  H.useEffect(() => H.link.subscribe("debug"), []);
  const s = snapOf(H.store.topic("debug").value?.data);
  const [sheet, setSheet] = H.useState<Sheet>("");
  // The console bar is the view's foot: under the scrolling body, so it
  // never lies over a row (or its ✕) the way a sticky bar did.
  return (
    <H.View
      tab="more"
      name="debug"
      title={tx("rd.title")}
      back={{ label: tx("pg.back"), onClick: () => H.go({ tab: "more", rest: "", query: {} }) }}
      foot={s ? <ConsoleBar s={s} onOpen={() => setSheet("console")} /> : undefined}
    >
      {s ? <Debug s={s} sheet={sheet} setSheet={setSheet} /> : <div class="pg-empty">…</div>}
      <PressMenu />
    </H.View>
  );
}

function Debug({ s, sheet, setSheet }: { s: DebugSnap; sheet: Sheet; setSheet: (v: Sheet) => void }) {
  const act = H.canAct();
  const cfg = s.configs.find((c) => c.name === s.selected);
  const close = () => setSheet("");
  const top = s.frames.find((f) => f.id === s.frame) ?? s.frames[0];
  return (
    <div class={`pg pg-debug${s.active ? " is-live" : ""}`}>
      {act && s.active && <Controls s={s} />}
      {act && !s.active && (
        <div class="rd-head">
          <button type="button" class="rd-cfg" data-act="config" aria-label={tx("rd.config")} disabled={s.configs.length === 0} onClick={() => setSheet("cfg")}>
            <Ic n="bug" size={16} />
            <span class="nm">{s.selected || tx("rd.no_config")}</span>
            <Ic n="down" size={16} />
          </button>
          <span class="rd-split">
            <button type="button" class="go" data-act="start" disabled={!cfg} onClick={() => H.ask(cfg?.debug ? "debug.start" : "debug.run", { config: s.selected ?? "" })}>
              <Ic n="play" size={14} /> {tx(cfg && !cfg.debug ? "rd.run" : "rd.start")}
            </button>
            <button
              type="button"
              class="more"
              data-act="start-more"
              aria-label={tx("rd.more_start")}
              onClick={(e) =>
                openMenu(e.clientX, e.clientY, s.selected ?? "", [
                  ...(cfg?.debug ? [{ id: "run", label: tx("rd.run_no_debug"), icon: "play" as IconName, run: () => H.ask("debug.run", { config: cfg.name }) }] : []),
                  { id: "console", label: tx("rd.console"), icon: "term", run: () => setSheet("console") },
                ])
              }
            >
              <Ic n="down" size={16} />
            </button>
          </span>
        </div>
      )}
      {s.active && (
        <p class={`rd-status${s.stopped ? " is-paused" : ""}`} role="status">
          {s.stopped && top ? tx("rd.paused_at", { status: s.status || s.name || "", at: `${top.path ? base(top.path) : top.file ?? ""}:${top.line}` }) : s.status || tx("rd.running")}
        </p>
      )}
      {s.hint && <p class="pg-hint">{s.hint}</p>}
      {s.config_error && <p class="pg-hint rd-err">{s.config_error}</p>}
      {s.active && s.stopped && <Variables s={s} act={act} />}
      {s.active && s.stopped && <Stack s={s} act={act} />}
      {s.active && s.watches.length > 0 && <Watches s={s} act={act} />}
      <Breakpoints s={s} act={act} onAdd={() => setSheet("bp")} />
      {!s.active && (
        <Section id="while" title={tx("rd.while")}>
          <button type="button" class="pg-row" data-act="watch" onClick={() => setSheet("watch")}>
            <Ic n="watch" />
            <span class="t">
              <b>{tx("rd.watch")}</b>
            </span>
            <span class="r">
              {s.watches.length}
              {act && " · +"}
            </span>
            <span class="pg-chev">
              <Ic n="chev" />
            </span>
          </button>
          <div class="pg-row">
            <Ic n="list" />
            <span class="t">
              <b>{tx("rd.vars_stack")}</b>
            </span>
            <span class="r">{tx("rd.when_paused")}</span>
          </div>
        </Section>
      )}
      {sheet === "cfg" && <ConfigSheet s={s} onClose={close} />}
      {sheet === "bp" && <AddBreakpoint onClose={close} />}
      {sheet === "watch" && <WatchSheet s={s} act={act} onClose={close} />}
      {sheet === "console" && <ConsoleSheet s={s} act={act} onClose={close} />}
    </div>
  );
}

/** Controls is the floating pill that steps the live session. */
function Controls({ s }: { s: DebugSnap }) {
  return (
    <div class="rd-pill" role="toolbar" aria-label={tx("rd.controls")}>
      {STEPS.filter((x) => (!x.paused || s.stopped) && (!x.running || !s.stopped)).map((x) => (
        <button type="button" key={x.op} class={x.tone} data-act={x.op} aria-label={tx(x.label)} title={tx(x.label)} onClick={() => H.ask("debug.control", { op: x.op })}>
          <Ic n={x.icon} size={20} />
        </button>
      ))}
    </div>
  );
}

function VarRows({ s, parent, vars, depth, act }: { s: DebugSnap; parent: string; vars: readonly DebugVar[]; depth: number; act: boolean }) {
  return (
    <>
      {vars.map((v) => {
        const key = parent + "/" + v.name;
        const open = !!v.ref && s.open.includes(key);
        const kids = open ? s.children.find((c) => c.key === key) : undefined;
        const menu = press((x, y) =>
          openMenu(x, y, `${v.name} = ${v.value}`.slice(0, 60), [
            ...aiItems(varAttach(v), tx("rd.ask_var") + v.name),
            ...(act ? [{ id: "watch", label: tx("rd.watch_it"), icon: "watch" as IconName, run: () => H.ask("debug.watch", { op: "add", expr: v.name }) }] : []),
            { id: "copy", label: tx("pg.copy"), icon: "copy", run: () => copy(v.value) },
          ]),
        );
        return (
          <Fragment key={key}>
            <button
              type="button"
              class="pg-row rd-var"
              data-key={key}
              style={{ paddingInlineStart: `${12 + depth * 14}px` }}
              aria-expanded={v.ref ? (open ? "true" : "false") : undefined}
              onClick={() => v.ref && H.ask("debug.expand", { key, open: !open })}
              onContextMenu={menu}
            >
              <span class="t">
                <b class="mono">
                  {v.ref ? (open ? "▾ " : "▸ ") : ""}
                  {v.name}
                </b>
              </span>
              <span class={`r mono${/^-?[\d.]+$/.test(v.value) ? " num" : ""}`} title={v.type ? `${v.value} (${v.type})` : v.value}>
                {v.value}
              </span>
            </button>
            {kids && (kids.error ? <p class="pg-hint">{kids.error}</p> : <VarRows s={s} parent={key} vars={kids.vars} depth={depth + 1} act={act} />)}
          </Fragment>
        );
      })}
    </>
  );
}

function Variables({ s, act }: { s: DebugSnap; act: boolean }) {
  // The cheap scopes' variables (Locals, Arguments …) as one list; an
  // expensive scope (Globals) only when opened on the laptop or desktop.
  const scopes = s.scopes.filter((sc) => sc.loaded || s.children.some((c) => c.key === sc.key));
  const n = scopes.reduce((k, sc) => k + (sc.loaded ? sc.vars : s.children.find((c) => c.key === sc.key)?.vars ?? []).length, 0);
  if (!n) return null;
  const gear = (
    <button
      type="button"
      class="pg-tool"
      data-act="customize"
      aria-label={tx("pg.customize", { name: tx("rd.vars") })}
      onClick={(e) =>
        openMenu(e.clientX, e.clientY, tx("rd.vars"), [
          { id: "ask-view", label: tx("pg.ask_view"), icon: "spark", ai: true, run: () => askAI(viewAttach("debug.state", tx("rd.vars"), { group: "part" }), tx("pg.ask_view_text", { page: tx("rd.title") })) },
        ])
      }
    >
      <Ic n="gear" size={17} />
    </button>
  );
  return (
    <Section id="vars" title={tx("rd.vars")} count={n} tool={gear}>
      {scopes.map((sc) => (
        <VarRows key={sc.key} s={s} parent={sc.key} vars={sc.loaded ? sc.vars : s.children.find((c) => c.key === sc.key)?.vars ?? []} depth={0} act={act} />
      ))}
    </Section>
  );
}

function Stack({ s, act }: { s: DebugSnap; act: boolean }) {
  if (!s.frames.length) return null;
  return (
    <Section id="stack" title={tx("rd.stack")} count={s.frames.length}>
      {s.frames.map((f) => {
        const at = `${f.path ? base(f.path) : f.file ?? ""}:${f.line}`;
        const menu = f.path ? press((x, y) => openMenu(x, y, f.name, aiItems(lineAttach(f.path!, f.line, `${f.name} ${at}`), tx("rd.ask_frame") + `${f.name} (${at})`))) : undefined;
        return (
          <button
            type="button"
            key={f.id}
            class={`pg-row rd-frame${f.id === s.frame ? " is-on" : ""}${f.outside ? " is-out" : ""}`}
            data-frame={f.id}
            aria-current={f.id === s.frame ? "true" : undefined}
            onClick={() => {
              if (act && f.id !== s.frame) H.ask("debug.frame", { id: f.id });
              if (f.path) H.openAt(f.path, f.line);
            }}
            onContextMenu={menu}
          >
            <span class="t">
              <b class="mono">{f.name}</b>
              <small>{at}</small>
            </span>
          </button>
        );
      })}
    </Section>
  );
}

function Watches({ s, act }: { s: DebugSnap; act: boolean }) {
  return (
    <Section id="watch" title={tx("rd.watch")} count={s.watches.length}>
      {s.watches.map((w) => (
        <div class="pg-row rd-watch" key={w.expr} data-expr={w.expr}>
          <span class="t">
            <b class="mono">{w.expr}</b>
          </span>
          <span class={`r mono${w.error ? " bad" : ""}`}>{s.stopped ? w.value || "…" : tx("rd.na")}</span>
          {act && (
            <button type="button" class="pg-tool" data-act="unwatch" aria-label={tx("rd.watch_remove", { expr: w.expr })} onClick={() => H.ask("debug.watch", { op: "remove", expr: w.expr })}>
              ✕
            </button>
          )}
        </div>
      ))}
    </Section>
  );
}

function Breakpoints({ s, act, onAdd }: { s: DebugSnap; act: boolean; onAdd: () => void }) {
  if (!s.breakpoints.length && !act) return null;
  return (
    <Section id="bps" title={tx("rd.bps")} count={s.breakpoints.length || undefined}>
      {s.breakpoints.map((b: DebugBreakpoint) => {
        const at = `${base(b.path)}:${b.line}`;
        const menu = press((x, y) =>
          openMenu(x, y, at, [
            ...aiItems(lineAttach(b.path, b.line, at), tx("rd.ask_bp") + at),
            { id: "open", label: tx("rd.bp_open"), icon: "file", run: () => H.openAt(b.path, b.line) },
            ...(act ? [{ id: "remove", label: tx("rd.bp_remove"), icon: "stop" as IconName, run: () => H.ask("debug.bp", { op: "remove", path: b.path, line: b.line }) }] : []),
          ]),
        );
        const ok = b.verified !== "no";
        return (
          <button type="button" class="pg-row rd-bp" key={b.path + ":" + b.line} data-bp={`${b.path}:${b.line}`} onClick={() => H.openAt(b.path, b.line)} onContextMenu={menu}>
            <span class={`rd-dot${ok ? "" : " off"}`} aria-hidden="true" />
            <span class="t">
              <b class="mono">{at}</b>
              {(b.condition || b.log || b.text) && <small class="mono">{b.condition ? `if ${b.condition}` : b.log ? `log ${b.log}` : b.text}</small>}
            </span>
            <span class={`rd-ok${b.verified === "yes" ? " on" : ""}`} role="img" aria-label={tx(b.verified === "yes" ? "rd.bp_ok" : "rd.bp_wait")}>
              {b.verified === "yes" && <Ic n="check" size={14} />}
            </span>
          </button>
        );
      })}
      {act && (
        <button type="button" class="pg-row add" data-act="bp-add" onClick={onAdd}>
          <Ic n="plus" />
          <span class="t">
            <b>{tx("rd.bp_add")}</b>
          </span>
        </button>
      )}
    </Section>
  );
}

/** ConsoleBar is the Debug console docked at the bottom: its last line. */
function ConsoleBar({ s, onOpen }: { s: DebugSnap; onOpen: () => void }) {
  const last = ([...s.console].reverse().find((l) => l.cat === "input")?.text ?? s.console[s.console.length - 1]?.text ?? "").replace(/^›\s*/, "");
  return (
    <button type="button" class="rd-con" data-act="console" onClick={onOpen}>
      <Ic n="term" size={16} />
      <b>{tx("rd.console")}</b>
      {last && <span class="mono">› {last}</span>}
      <Ic n="down" size={16} />
    </button>
  );
}

function SheetHead({ title, onClose }: { title: string; onClose: () => void }) {
  return (
    <div class="pg-sh">
      <button type="button" class="pg-tool" aria-label={tx("pg.back")} onClick={onClose}>
        <Ic n="back" />
      </button>
      <b>{title}</b>
    </div>
  );
}

function ConfigSheet({ s, onClose }: { s: DebugSnap; onClose: () => void }) {
  return (
    <H.Sheet label={tx("rd.config")} onLater={onClose}>
      <div class="pg-picker" data-picker="config">
        <SheetHead title={tx("rd.config")} onClose={onClose} />
        <div role="radiogroup" aria-label={tx("rd.config")}>
          {s.configs.map((c) => (
            <button
              type="button"
              class="pg-row pg-pick"
              key={c.name}
              data-choice={c.name}
              role="radio"
              aria-checked={c.name === s.selected ? "true" : "false"}
              onClick={() => {
                if (c.name !== s.selected) H.ask("debug.select", { config: c.name });
                onClose();
              }}
            >
              <span class="ck">{c.name === s.selected && <Ic n="check" />}</span>
              <span class="t">
                <b>{c.name}</b>
                <small>{[c.type, c.debug ? "" : tx("rd.run"), c.source].filter(Boolean).join(" · ")}</small>
              </span>
            </button>
          ))}
        </div>
      </div>
    </H.Sheet>
  );
}

function AddBreakpoint({ onClose }: { onClose: () => void }) {
  const [path, setPath] = H.useState("");
  const [line, setLine] = H.useState("");
  const n = parseInt(line, 10);
  const ok = path.trim() !== "" && n > 0;
  return (
    <H.Sheet label={tx("rd.bp_add")} onLater={onClose}>
      <form
        class="pg-picker rd-form"
        data-picker="bp"
        onSubmit={(e) => {
          e.preventDefault();
          if (!ok) return;
          H.ask("debug.bp", { op: "set", path: path.trim().replace(/^\.\//, ""), line: n });
          onClose();
        }}
      >
        <SheetHead title={tx("rd.bp_add")} onClose={onClose} />
        <label class="rd-field">
          <span>{tx("rd.bp_file")}</span>
          <input name="path" dir="ltr" autoComplete="off" autoCapitalize="off" spellcheck={false} value={path} placeholder={tx("rd.bp_file_eg")} onInput={(e) => setPath((e.target as HTMLInputElement).value)} />
        </label>
        <label class="rd-field">
          <span>{tx("rd.bp_line")}</span>
          <input name="line" inputMode="numeric" value={line} placeholder="27" onInput={(e) => setLine((e.target as HTMLInputElement).value)} />
        </label>
        <div class="pg-acts">
          <button type="button" class="pg-btn" onClick={onClose}>
            {tx("pg.cancel")}
          </button>
          <button type="submit" class="pg-btn pri" data-act="bp-save" disabled={!ok}>
            {tx("rd.add")}
          </button>
        </div>
      </form>
    </H.Sheet>
  );
}

function WatchSheet({ s, act, onClose }: { s: DebugSnap; act: boolean; onClose: () => void }) {
  const [expr, setExpr] = H.useState("");
  return (
    <H.Sheet label={tx("rd.watch")} onLater={onClose}>
      <div class="pg-picker" data-picker="watch">
        <SheetHead title={tx("rd.watch")} onClose={onClose} />
        {s.watches.map((w) => (
          <div class="pg-row pg-pick" key={w.expr} data-expr={w.expr}>
            <span class="t">
              <b class="mono">{w.expr}</b>
              <small class="mono">{s.stopped ? w.value || "…" : tx("rd.na")}</small>
            </span>
            {act && (
              <button type="button" class="pg-tool" data-act="unwatch" aria-label={tx("rd.watch_remove", { expr: w.expr })} onClick={() => H.ask("debug.watch", { op: "remove", expr: w.expr })}>
                ✕
              </button>
            )}
          </div>
        ))}
        {act && (
          <form
            class="pg-step"
            onSubmit={(e) => {
              e.preventDefault();
              const v = expr.trim();
              if (v) H.ask("debug.watch", { op: "add", expr: v });
              setExpr("");
            }}
          >
            <input dir="ltr" value={expr} aria-label={tx("rd.watch_add")} placeholder={tx("rd.watch_add")} autoCapitalize="off" spellcheck={false} onInput={(e) => setExpr((e.target as HTMLInputElement).value)} />
            <button type="submit" class="pg-btn pri" data-act="watch-save">
              {tx("rd.add")}
            </button>
          </form>
        )}
      </div>
    </H.Sheet>
  );
}

function ConsoleSheet({ s, act, onClose }: { s: DebugSnap; act: boolean; onClose: () => void }) {
  const [expr, setExpr] = H.useState("");
  const box = H.useRef<HTMLDivElement | null>(null);
  H.useEffect(() => {
    const el = box.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [s.console.length]);
  const can = act && s.active && s.stopped;
  return (
    <H.Sheet label={tx("rd.console")} onLater={onClose}>
      <div class="pg-picker" data-picker="console">
        <SheetHead title={tx("rd.console")} onClose={onClose} />
        <div class="rd-log mono" ref={box} dir="ltr" role="log" aria-label={tx("rd.console")}>
          {s.console.length === 0 && <p class="pg-hint">{tx("rd.console_none")}</p>}
          {s.console.map((l, i) => (
            <div key={i} class={`c-${l.cat}`}>
              {l.text || " "}
            </div>
          ))}
        </div>
        <form
          class="pg-step"
          onSubmit={(e) => {
            e.preventDefault();
            const v = expr.trim();
            if (v && can) H.ask("debug.eval", { expr: v });
            setExpr("");
          }}
        >
          <input dir="ltr" class="mono" value={expr} disabled={!can} aria-label={tx("rd.console_in")} placeholder={tx(can ? "rd.console_in" : "rd.console_off")} autoCapitalize="off" spellcheck={false} onInput={(e) => setExpr((e.target as HTMLInputElement).value)} />
        </form>
      </div>
    </H.Sheet>
  );
}
