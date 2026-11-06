// The laptop's Lua extension panels and status items (WEBP-7). Every
// panel is a section of one Extensions view in the side bar, with the
// panel's lines in the extension markup's styles (theme colors, no raw
// escapes); a row picked runs the panel's select handler on the laptop,
// as a click there does. The status items sit in the status bar; one
// with a command is a button that runs it.
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useState } from "preact/hooks";
import type { ExtPanelView, ExtSpan, StatusExt } from "../../../../../webproto/proto";
import { registerSide } from "../../lib/views";
import { toast } from "../../lib/toasts";
import { link, live, store } from "../../session";
import { t } from "../../strings";
import { PaneSection, PaneView, Row, Tree } from "../../ui/pane";

/** extStatus reads the status topic's ext part (null: no extension has
 * a panel or a status item). */
export function extStatus(): StatusExt | null {
  const d = store.topic("status").value?.data;
  const x = typeof d === "object" && d !== null ? (d as Record<string, unknown>).ext : undefined;
  if (typeof x !== "object" || x === null) return null;
  const e = x as Partial<StatusExt>;
  return { panels: Array.isArray(e.panels) ? e.panels : [], items: Array.isArray(e.items) ? e.items : [] };
}

/** hasExtPanels: the activity bar shows the Extensions item. */
export function hasExtPanels(): boolean {
  return (extStatus()?.panels.length ?? 0) > 0;
}

const failed = (e: unknown) => toast("warn", e instanceof Error ? e.message : t("cmd.failed"));

/** EXT_EVERY is how often an open panel asks for its lines again. */
const EXT_EVERY = 2000;

/** spanClass maps the markup's style names to classes (xm-accent …). */
function spanClass(s: ExtSpan): string | undefined {
  return s.s ? s.s.split(" ").map((n) => "xm-" + n).join(" ") : undefined;
}

const shut = signal<ReadonlySet<string>>(new Set());
function toggle(id: string): void {
  const n = new Set(shut.peek());
  if (n.has(id)) n.delete(id);
  else n.add(id);
  shut.value = n;
}

/** ExtPanel is one panel's section: its lines, asked again while open. */
function ExtPanel({ id, title }: { id: string; title: string }) {
  const [lines, setLines] = useState<ExtSpan[][] | null>(null);
  const open = !shut.value.has(id);
  useEffect(() => {
    if (!open) return;
    let gone = false;
    const load = () =>
      link.request("ext.panel", { id }).then(
        (r) => !gone && setLines((r as ExtPanelView).lines ?? []),
        () => !gone && setLines((l) => l ?? []),
      );
    void load();
    const every = setInterval(() => void load(), EXT_EVERY);
    return () => {
      gone = true;
      clearInterval(every);
    };
  }, [id, open]);
  const pick = (row: number) => {
    if (!live.peek()) return;
    link.request("ext.select", { id, row }).catch(failed);
  };
  return (
    <PaneSection id={"ext-" + id} title={title} open={open} onToggle={() => toggle(id)}>
      {lines === null ? (
        <p class="pv-msg">{t("loading")}</p>
      ) : lines.length === 0 ? (
        <p class="pv-msg">{t("extv.empty")}</p>
      ) : (
        <Tree label={title}>
          {lines.map((l, i) => (
            <Row
              key={i}
              level={0}
              label={
                <bdi dir="auto">
                  {l.map((s, j) => (
                    <span key={j} class={spanClass(s)}>
                      {s.t}
                    </span>
                  ))}
                </bdi>
              }
              onClick={() => pick(i)}
            />
          ))}
        </Tree>
      )}
    </PaneSection>
  );
}

export function DeskExtensions() {
  const x = extStatus();
  return (
    <PaneView id="extensions" title={t("act.extensions")}>
      <div class="pv-scroll">
        {!x || x.panels.length === 0 ? <p class="pv-msg">{t("extv.none")}</p> : x.panels.map((p) => <ExtPanel key={p.id} id={p.id} title={p.title} />)}
      </div>
    </PaneView>
  );
}

registerSide("extensions", () => <DeskExtensions />);

/** ExtStatusItems are the extensions' status items for the status bar. */
export function ExtStatusItems() {
  const x = extStatus();
  if (!x) return null;
  return (
    <>
      {x.items.map((it) =>
        it.run ? (
          <button
            type="button"
            key={it.id}
            class="strip-item sb-ext"
            data-ext={it.id}
            title={t("extv.run", { id: it.id })}
            onClick={() => live.peek() && link.request("ext.status_run", { id: it.id }).catch(failed)}
          >
            {it.text}
          </button>
        ) : (
          <span key={it.id} class="strip-item sb-ext" data-ext={it.id} title={it.id}>
            {it.text}
          </span>
        ),
      )}
    </>
  );
}
