// The More list on a phone or tablet (RM-3; design sheet 09), in
// radial.js: a search field on top, then grouped cards. Pinned leads:
// the views the user pinned on this device, else the views opened last;
// a pinned row asks for its topic and shows its live count (1 failed).
// Every row has a colour icon tile (the radial menu's icons, so both
// speak one language) and, on a long press, a menu: Ask AI about this
// (the chat gets the view's name as a draft, nothing is sent) and Pin.
// Pinned's ⋯ opens Edit Pinned: ✕ removes, the grip drags, Done keeps.
// Every view a phone has is a row here; a new view that is not fails
// more_coverage_test.go. Drawn with the page's Preact (RadialHost): this
// file imports no value of the page.
import type { VNode as PV } from "preact";
import type { StringKey } from "../strings";
import { ICONS } from "../../../../../web/packages/radial-menu/src/icons";
import type { RadialHost } from "./actions";
import { isIds, read, write } from "../radial";

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- h() of the page gives each element its own props type
type VNode = PV<any>;

/** Row is [sub view, its name (a page string), its icon]. */
type Row = [string, StringKey, string];

/** A tile's colour (g green, a amber, r red, b blue, p the AI's, m
 * muted): its group's (TINTS, in GROUPS' order) unless TINT names it. */
const TINTS = "pbbbm";
const TINT: Record<string, string> = { testing: "g", runs: "g", debug: "r", services: "a", db: "a", mcp: "p" };

/** The list's own words (English, like the phone Home's). */
export const LS = {
  find: "Search the list",
  pinned: "Pinned",
  found: "Found",
  none: "No view has that name.",
  edit: "Edit Pinned",
  done: "Done",
  close: "Close",
  more: "More for Pinned",
  ask: "✦ Ask AI about this",
  pin: "Pin",
  unpin: "Unpin",
  drag: "Drag to sort · or ask: “pin what I use for releases”",
  failed: " failed",
  about: "About the {v} view (#/more/{s}): ",
} as const;

/** GROUPS are the More list's groups, in order. */
export const GROUPS: [string, Row[]][] = [
  ["AI", [["new-task", "nt.title", "list"], ["agents", "ai.wt_title", "branch"], ["races", "more.races", "flag"], ["review", "pill.review", "diff"], ["itermoc", "im.title", "set"]]],
  ["Code", [["search", "more.search", "find"], ["projects", "ph.switch_title", "grid"], ["tasks", "more.tasks", "list"], ["testing", "act.testing", "flask"], ["debug", "act.run", "bug"]]],
  ["Runs and workflows", [["runs", "more.runs", "play"], ["workflows", "more.workflows", "flow"], ["board", "more.board", "board"], ["services", "act.services", "set"]]],
  ["Data and infrastructure", [["db", "more.db", "db"], ["containers", "more.containers", "box"], ["kube", "act.kube", "cloud"], ["mcp", "more.mcp", "plug"]]],
  ["This device and the app", [["phone", "more.this_phone", "phone"], ["settings", "more.settings", "gear"], ["tools", "tools.title", "grid"]]],
];

/** RECENT is how many recent views lead the list. */
const RECENT = 4;
/** PINS is where this device keeps the pinned views (no secret). */
const PINS = "termocode.more.pins";
const MAX_PINS = 8;
/** from is the row being dragged in Edit Pinned (-1: none). */
let from = -1;

/** COUNT: a view's topic and its list's key; its count is the list's
 * length (Testing: its failed tests, else all its tests). */
const COUNT: Record<string, [string, string]> = {
  testing: ["tests", "items"],
  tasks: ["tasks", "tasks"],
  runs: ["runs", "runs"],
  workflows: ["workflows", "workflows"],
  agents: ["worktrees", "worktrees"],
  races: ["races", "races"],
  services: ["services", "services"],
  mcp: ["mcp", "agents"],
};

/** count is a row's live count from its topic now ("" while none). */
function count(host: RadialHost, sub: string): [string, boolean] {
  const c = COUNT[sub];
  const d = c && (host.topic(c[0]) as Record<string, unknown> | undefined);
  const list = d?.[c[1]];
  if (!Array.isArray(list)) return ["", false];
  const n = d?.counts as { failed: number; total: number } | undefined;
  return n?.failed ? [n.failed + LS.failed, true] : [String(n?.total ?? list.length), false];
}

/** MoreList draws the list with the page's Preact. */
export function MoreList({ host }: { host: RadialHost }): VNode {
  const { h } = host;
  const [q, setQ] = host.useState("");
  const [own, setOwn] = host.useState<string[] | null>(() => read(PINS, isIds));
  const [menu, setMenu] = host.useState<Row | null>(null);
  const [edit, setEdit] = host.useState<string[] | null>(null);
  const all = GROUPS.flatMap(([, rs]) => rs);
  const rowOf = (s: string) => all.find((r) => r[0] === s);
  const pins = (own ?? host.prefs.value.moreRecent).map(rowOf).filter((r): r is Row => !!r);
  const subs = pins.map((r) => COUNT[r[0]]?.[0]).filter(Boolean).join();
  host.useEffect(() => {
    if (subs) host.link.subscribe(...subs.split(","));
  }, [subs]);
  const keep = (v: string[]) => (write(PINS, v), setOwn(v));

  // An icon as SVG elements (no markup: the page's Trusted Types).
  const icon = (name: string, size = 18): VNode =>
    h(
      "svg",
      { class: "more-ic", viewBox: "-10 -10 20 20", width: size, height: size, "aria-hidden": "true" },
      [...(ICONS[name] ?? "").matchAll(/<(\w+)([^>]*?)\/>/g)].map((m) => {
        const a = new Map<string, string>();
        for (const x of m[2].matchAll(/([\w-]+)="([^"]*)"/g)) a.set(x[1], x[2] === "{c}" ? "currentColor" : x[2]);
        return h(m[1], Object.fromEntries(a));
      }),
    );
  const tile = (r: Row): VNode => h("span", { class: "more-tile t-" + (TINT[r[0]] ?? TINTS[GROUPS.findIndex((g) => g[1].includes(r))]) }, icon(r[2], 16));
  const link = (r: Row): VNode => {
    const [sub, name] = r;
    const [n, bad] = count(host, sub);
    return h(
      "button",
      {
        type: "button",
        class: "more-link",
        "data-sub": sub,
        key: sub,
        onClick: () => {
          host.setPref("moreRecent", [sub, ...host.prefs.peek().moreRecent.filter((s) => s !== sub)].slice(0, RECENT));
          host.go({ tab: "more", rest: sub, query: {} });
        },
        onContextMenu: (e: Event) => (e.preventDefault(), setMenu(r)),
      },
      tile(r),
      h("span", { class: "more-lb" }, host.t(name)),
      n && h("span", { class: "more-n" + (bad ? " is-bad" : "") }, n),
    );
  };
  const group = (title: string, rows: Row[], cls = "", tool?: VNode): VNode =>
    h(
      "section",
      { class: "more-group" + cls, "aria-label": title, key: title },
      h("div", { class: "more-gh" }, h("h2", { class: "group-title" }, title), tool),
      h("nav", { class: "more-links more-card" }, rows.map(link)),
    );
  const find = h(
    "label",
    { class: "more-findbox" },
    icon("find", 17),
    h("input", { class: "more-find", type: "search", dir: "auto", value: q, "aria-label": LS.find, placeholder: LS.find, onInput: (e: Event) => setQ((e.currentTarget as HTMLInputElement).value) }),
  );
  const btn = (label: string, on: () => void, cls = "", aria?: string): VNode => h("button", { type: "button", class: cls, "aria-label": aria, onClick: on }, label);

  // The row menu (a long press): Ask AI about this, Pin or Unpin.
  const rowMenu =
    menu &&
    h(
      "div",
      { class: "sheet-layer more-scrim", onClick: () => setMenu(null) },
      h(
        "div",
        { class: "more-menu", role: "menu", "aria-label": host.t(menu[1]), onClick: (e: Event) => e.stopPropagation() },
        h("p", { class: "more-mt" }, host.t(menu[1])),
        btn(LS.ask, () => (setMenu(null), host.askAI(LS.about.replace("{v}", host.t(menu[1])).replace("{s}", menu[0]))), "is-ai"),
        pins.includes(menu)
          ? btn(LS.unpin, () => (setMenu(null), keep(pins.filter((r) => r !== menu).map((r) => r[0]))))
          : btn(LS.pin, () => (setMenu(null), keep([...pins.map((r) => r[0]).filter((s) => s !== menu[0]), menu[0]].slice(-MAX_PINS)))),
      ),
    );

  // Edit Pinned: ✕ removes a row, its grip drags it, Done keeps the order.
  const drag = (e: PointerEvent) => {
    if (!edit || from < 0) return;
    const list = (e.currentTarget as HTMLElement).closest(".more-edit") as HTMLElement;
    const rows = [...list.children] as HTMLElement[];
    const to = rows.findIndex((el) => e.clientY < el.getBoundingClientRect().bottom);
    const at = to < 0 ? rows.length - 1 : to;
    if (at !== from) {
      const v = [...edit];
      v.splice(at, 0, ...v.splice(from, 1));
      from = at;
      setEdit(v);
    }
  };
  const sheet =
    edit &&
    h(
      "div",
      { class: "sheet-layer more-scrim" },
      h(
        "div",
        { class: "more-sheet", role: "dialog", "aria-label": LS.edit },
        h("div", { class: "more-sh" }, btn("✕", () => setEdit(null), "more-shx", LS.close), h("b", null, LS.edit), btn(LS.done, () => (keep(edit), setEdit(null)), "more-done")),
        h(
          "div",
          { class: "more-edit" },
          edit.map(rowOf).map(
            (r, i) =>
              r &&
              h(
                "div",
                { class: "more-edit-row", "data-sub": r[0], key: r[0] },
                btn("✕", () => setEdit(edit.filter((s) => s !== r[0])), "more-x", LS.unpin + " " + host.t(r[1])),
                tile(r),
                h("span", { class: "more-lb" }, host.t(r[1])),
                h(
                  "span",
                  {
                    class: "more-grip",
                    "aria-hidden": "true",
                    onPointerDown: (e: PointerEvent) => ((from = i), (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)),
                    onPointerMove: drag,
                    onPointerUp: () => (from = -1),
                  },
                  icon("format", 18),
                ),
              ),
          ),
        ),
        h("p", { class: "more-hint" }, LS.drag),
      ),
    );

  const words = q.trim().toLowerCase();
  if (words) {
    const hits = all.filter(([sub, name]) => (host.t(name) + " " + sub).toLowerCase().includes(words));
    return h("div", { class: "more-list" }, find, hits.length ? group(LS.found, hits) : h("p", { class: "empty", role: "status" }, LS.none), rowMenu);
  }
  const dots = h("button", { type: "button", class: "more-gmenu", "aria-label": LS.more, onClick: () => setEdit(pins.map((r) => r[0])) }, "⋯");
  return h("div", { class: "more-list" }, find, pins.length > 0 && group(LS.pinned, pins, " is-pinned", dots), GROUPS.map(([g, rs]) => group(g, rs)), rowMenu, sheet);
}
