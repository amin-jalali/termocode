// Drag and drop to the AI chat (Add to AI Chat): a row of the Databases
// or Containers trees carries its item (lib/aictx.ts aiDrag); while it is
// over the chat, the chat input is marked, and a drop adds its chip.
// What a drop brings is checked: only a tree row's address (never rows
// of data), so a drag from another page adds nothing it should not.
import type { AIItem, ItemKind } from "./api";

/** MIME is the drag data type of an item (lib/aictx.ts sets it). */
export const MIME = "application/x-termocode-item";

const KINDS: ReadonlySet<ItemKind> = new Set(["db_conn", "db_table", "host", "compose_project", "container"]);
const FIELDS = ["host", "project", "container", "conn", "table"] as const;

/** readDrop is the item a drop names, or null. */
function readDrop(text: string): AIItem | null {
  let v: unknown;
  try {
    v = JSON.parse(text);
  } catch {
    return null;
  }
  const o = typeof v === "object" && v !== null ? (v as Record<string, unknown>) : {};
  if (!KINDS.has(o.item as ItemKind)) return null;
  const it: AIItem = { item: o.item as ItemKind };
  for (const f of FIELDS) if (typeof o[f] === "string" && o[f]) it[f] = (o[f] as string).slice(0, 256);
  return it;
}

/** watchDrops takes items dropped on the chat. */
export function watchDrops(add: (it: AIItem) => void): void {
  let on: Element | null = null;
  const mark = (c: Element | null | undefined) => {
    if (on === c) return;
    on?.classList.remove("ai-drop");
    on = c ?? null;
    on?.classList.add("ai-drop");
  };
  const chatOf = (e: DragEvent) => (e.dataTransfer?.types.includes(MIME) ? (e.target as Element).closest?.('[data-view="chat"]') : null);
  document.addEventListener("dragover", (e) => {
    const v = chatOf(e);
    mark(v?.querySelector(".composer"));
    if (!v) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "copy";
  });
  document.addEventListener("dragleave", (e) => {
    const to = e.relatedTarget;
    if (on && !(to instanceof Node && on.closest('[data-view="chat"]')?.contains(to))) mark(null);
  });
  document.addEventListener("dragend", () => mark(null));
  document.addEventListener("drop", (e) => {
    mark(null);
    const v = chatOf(e);
    const it = v && readDrop(e.dataTransfer?.getData(MIME) ?? "");
    if (!v) return;
    e.preventDefault();
    if (it) add(it);
  });
}
