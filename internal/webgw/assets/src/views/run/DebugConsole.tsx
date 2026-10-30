// DEBUG CONSOLE, a tab of the desktop's bottom panel (WEBP-4): the
// laptop's Debug Console (program output, the debugger's messages, the
// lines typed and their answers), newest at the bottom. The input
// evaluates a line in the paused frame (debug.eval); its answer comes
// back in the console. Up and Down walk this page's earlier lines.
import { h } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { link } from "../../session";
import { t } from "../../strings";
import { ask, canAct, Note } from "./bits";
import { debugSnap } from "./DebugView";

const history: string[] = [];

export function DebugConsole() {
  useEffect(() => link.subscribe("debug"), []);
  const s = debugSnap();
  const box = useRef<HTMLDivElement | null>(null);
  const [at, setAt] = useState(-1);
  const lines = s?.console ?? [];
  useLayoutEffect(() => {
    const el = box.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [lines.length, lines[lines.length - 1]?.text]);
  const can = canAct() && s?.active === true;
  return (
    <div class="dcon" data-panel-view="debug-console">
      <div class="dcon-rows" ref={box} dir="ltr" role="log" aria-label={t("dbg.console")}>
        {s?.console_cut && <Note>{t("tasks.older")}</Note>}
        {lines.length === 0 && <Note>{t("dbg.console_none")}</Note>}
        {lines.map((l, i) => (
          <div key={i} class={`dcon-row c-${l.cat}`}>
            {l.text || " "}
          </div>
        ))}
      </div>
      <div class="dcon-in">
        <span class="dcon-prompt" aria-hidden="true">
          ›
        </span>
        <input
          class="dcon-input"
          dir="ltr"
          disabled={!can}
          aria-label={t("dbg.eval")}
          placeholder={can ? t("dbg.eval") : t("dbg.eval_off")}
          onKeyDown={(e) => {
            const el = e.currentTarget as HTMLInputElement;
            if (e.key === "Enter" && el.value.trim()) {
              const v = el.value.trim();
              history.push(v);
              setAt(-1);
              el.value = "";
              ask("debug.eval", { expr: v });
            } else if (e.key === "ArrowUp" && history.length) {
              const n = at < 0 ? history.length - 1 : Math.max(0, at - 1);
              setAt(n);
              el.value = history[n];
              e.preventDefault();
            } else if (e.key === "ArrowDown" && at >= 0) {
              const n = at + 1;
              setAt(n < history.length ? n : -1);
              el.value = n < history.length ? history[n] : "";
              e.preventDefault();
            }
          }}
        />
      </div>
    </div>
  );
}
