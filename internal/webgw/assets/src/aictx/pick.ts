// @ in the chat input (Add to AI Chat): at the start or after a space,
// it opens the quick input's picker over the items the page knows (the
// database connections and the tables the tree read, the hosts, their
// compose projects and containers), searched as typed. It reads the
// connections and the hosts when nothing read them yet, and the list
// grows as they come. Picking one adds its chip, takes the @ out of the
// draft and gives the focus back to the chat input.
import type { PickItem } from "../lib/workbench";
import type { AIItem, ItemKind } from "./api";
import { H } from "./jsx";

/** got is a load's value (undefined: not read). */
export const got = <T>(l: { s: string; v?: T } | null | undefined): T | undefined => (l?.s === "ok" ? l.v : undefined);

const KIND: Partial<Record<ItemKind, "ai.k_db" | "ai.k_table" | "ai.k_host" | "ai.k_project" | "ai.k_container">> = {
  db_conn: "ai.k_db",
  db_table: "ai.k_table",
  host: "ai.k_host",
  compose_project: "ai.k_project",
  container: "ai.k_container",
};

/** known are the items the page has read, in tree order. */
function known(): AIItem[] {
  const out: AIItem[] = [];
  for (const c of got(H.conns.peek()) ?? []) {
    out.push({ item: "db_conn", conn: c.id });
    const v = got(H.tables.peek().get(c.id));
    for (const x of v && "tables" in v ? v.tables : []) out.push({ item: "db_table", conn: c.id, table: x.name });
  }
  const hosts = got(H.hosts.peek()) ?? [];
  const here = hosts.find((x) => x.session) ?? hosts.find((x) => x.current);
  for (const h of hosts) {
    out.push({ item: "host", host: h.name });
    const cs = [...(got(H.hostCtrs.peek().get(h.name)) ?? []), ...(h === here ? (got(H.ctrs.peek())?.containers ?? []) : [])];
    const seen = new Set<string>();
    for (const c of cs) {
      if (c.project && !seen.has(c.project)) out.push({ item: "compose_project", host: h.name, project: c.project });
      seen.add(c.project);
    }
    for (const c of cs) if (!seen.has("\u0000" + c.name)) (seen.add("\u0000" + c.name), out.push({ item: "container", host: h.name, project: c.project || undefined, container: c.name }));
  }
  return out;
}

/** watchAt opens the picker on an @ typed in the chat input. */
export function watchAt(label: (it: AIItem) => string, add: (it: AIItem) => void): void {
  const check = (b: EventTarget | null) => {
    if (!(b instanceof HTMLTextAreaElement) || !b.matches(".composer-box")) return;
    const at = b.selectionStart;
    if (/(^|\s)@$/.test(b.value.slice(0, at))) open(b, at);
  };
  const open = (b: HTMLTextAreaElement, at: number) => {
    if (!H.conns.peek()) H.loadConns();
    if (!H.hosts.peek()) H.loadHosts();
    const items = (): PickItem[] => {
      const seen = new Set<string>();
      return known().flatMap((it) => {
        const name = label(it);
        if (seen.has(name)) return [];
        seen.add(name);
        const k = KIND[it.item];
        return [{ label: name, detail: k && H.t(k), run: () => (drop(b, at), add(it)) }];
      });
    };
    let cur = { title: H.t("ai.pick"), items: items() };
    H.quick.value = { mode: "pick", pick: cur };
    // The lists the picker asked for fill it as they come.
    let stop: (() => void) | undefined;
    stop = H.effect(() => {
      void [H.conns.value, H.tables.value, H.hosts.value, H.hostCtrs.value, H.ctrs.value];
      if (H.quick.peek()?.pick !== cur) return void stop?.();
      cur = { ...cur, items: items() };
      H.quick.value = { mode: "pick", pick: cur };
    });
  };
  document.addEventListener("input", (e) => check(e.target), true);
  check(document.activeElement); // an @ typed while this file loaded
}

/** drop takes the @ before at out of the chat input, which keeps the focus. */
function drop(b: HTMLTextAreaElement, at: number): void {
  b.value = b.value.slice(0, at - 1) + b.value.slice(at);
  b.dispatchEvent(new Event("input", { bubbles: true }));
  b.focus();
  b.setSelectionRange(at - 1, at - 1);
}
