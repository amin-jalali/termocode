// The v2 More topics as their pages read them (web design 2.9): settings,
// mcp, containers and db. Pure: no DOM, timers or network. Every reader
// takes unknown data and keeps only well-formed rows.

type Obj = Record<string, unknown>;
const obj = (v: unknown): Obj => (typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Obj) : {});
const str = (v: unknown): string => (typeof v === "string" ? v : "");
const list = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);

export interface SettingView {
  key: string;
  group: string;
  type: string;
  value: string;
  def: string;
  description: string;
}

/** readSettings reads the settings topic, grouped in the hub's order. */
export function readSettings(data: unknown): { group: string; items: SettingView[] }[] {
  const out: { group: string; items: SettingView[] }[] = [];
  for (const x of list(obj(data).items).map(obj)) {
    const key = str(x.key);
    if (!key) continue;
    const it: SettingView = {
      key,
      group: str(x.group),
      type: str(x.type),
      value: str(x.value),
      def: str(x.default),
      description: str(x.description),
    };
    const last = out[out.length - 1];
    if (last && last.group === it.group) last.items.push(it);
    else out.push({ group: it.group, items: [it] });
  }
  return out;
}
