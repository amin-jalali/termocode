// now.js: the phone's Now, built alone (cmd/webbuild nowChunk) with
// now.css. The Home loads it once the laptop's inbox carries the Now info
// and gives it its host; Now then is the Home's top: one summary line,
// the first-run rules card (once), Your turn (the user's 3/5/10 items:
// ▶, age, why chip, dashed rule tag, ✦ Fix on a red CI), See all and
// what runs; with nothing on the user's turn, "All clear.". A tap opens
// Why this? with the item's one fix or Open, Done, Later and Not
// important; a swipe left shows Later and Done. Each step is now.act and
// a toast with Undo: it only hides; nothing acts on the item's source.
// Routes over the Home: #/home/now (See all), #/home/now/rules,
// #/home/now/setup (the 4 first-run questions).
import "./now.css";
import type { InboxItem, NowInfo, NowRule } from "../../../../webproto/proto";
import { Fragment, H, h, setHost, type NowHost } from "./jsx";
import { age, ruleName, sections, summary, type Sections } from "./state";
import { DEFAULTS, s, SOURCES, type NowKey } from "./strings";

type El = h.JSX.Element;
type Open = (id: string) => void;

/** init takes the Home's host, once, before the first draw, and gives
 * back Now. */
export function init(host: NowHost): typeof Now {
  setHost(host);
  note = host.signal<Note | null>(null);
  const l = document.createElement("link");
  l.rel = "stylesheet";
  l.href = "/assets/now.css";
  document.head.append(l);
  return Now;
}

const req = (op: string, args: unknown) => H.link.request(op as never, args as never) as Promise<unknown>;

/** Note is the one toast Now shows: what was done, with Undo (5 s). */
interface Note {
  text: string;
  bad?: boolean;
  undo?: () => void;
}
let note: { value: Note | null } = { value: null };
let noteTimer: ReturnType<typeof setTimeout> | undefined;
function say(text: string, undo?: () => void, bad?: boolean): void {
  clearTimeout(noteTimer);
  note.value = { text, undo, bad };
  noteTimer = setTimeout(() => (note.value = null), 5000);
}
const fail = (e: unknown) => say(H.errText(e), undefined, true);

function Toast() {
  const n = note.value;
  if (!n) return null;
  return (
    <div class="toasts now-toasts" role="status" aria-live="polite">
      <div class={"toast sev-" + (n.bad ? "error" : "ok")}>
        <div class="toast-body">
          <div class="toast-title" dir="auto">
            {n.text}
          </div>
        </div>
        {n.undo && (
          <button
            type="button"
            class="toast-act"
            onClick={() => {
              n.undo?.();
              note.value = null;
            }}
          >
            {s("undo")}
          </button>
        )}
      </div>
    </div>
  );
}
const go = (rest: string) => H.go({ tab: "home", rest, query: {} });

function read(): { items: InboxItem[]; info: NowInfo | null } {
  const d = H.store.topic("inbox").value?.data as { items?: InboxItem[]; now?: NowInfo } | undefined;
  const v = d?.items;
  return { items: Array.isArray(v) ? v : [], info: d?.now ?? null };
}

const ACTS: Record<string, NowKey> = { done: "done_t", later: "later_t", mute: "mute_t" };

/** act puts x away (done, later, mute) and offers Undo. */
function act(x: InboxItem, what: "done" | "later" | "mute", when?: string): void {
  const args: Record<string, unknown> = { do: what, id: x.id };
  if (x.ws) args.ws = x.ws;
  if (when) args.when = when;
  req("now.act", args).then(() => say(s(ACTS[what], { title: x.title }), () => void req("now.act", { do: "undo" }).catch(fail)), fail);
}

/** fix puts the fix in the Home's box: nothing is sent until the user sends it. */
function fix(x: InboxItem): void {
  const c = H.readChat(H.store.topic("chat").peek()?.data);
  if (c) H.setDraft(c.id, s("fix_text", { name: x.ref }));
  go("");
}

const KIND: Record<string, NowKey> = { card: "agent", run: "agent_run", lane: "agent", workflow: "workflow", task: "task", ci: "ci", race: "race" };

function tile(x: InboxItem): [string, string] {
  if (x.kind === "ci" || x.kind === "task") return x.state === "failed" ? ["r", "✗"] : ["b", "▶"];
  return ["ai", "✦"];
}

function sub(x: InboxItem): string {
  const k = s(KIND[x.kind] ?? "agent");
  return x.chat ? k + " · " + x.chat : k;
}

function clock(ms: number): string {
  const d = new Date(ms);
  return d.getHours() + ":" + String(d.getMinutes()).padStart(2, "0");
}

interface RowCtx {
  now: number;
  rules: readonly NowRule[];
  open: Open;
  opened: string;
  setOpened: (id: string) => void;
  later: (x: InboxItem) => void;
}

function Why({ x, c }: { x: InboxItem; c: RowCtx }) {
  const canFix = x.can?.includes("fix");
  return (
    <div class="now-whyp" role="group" aria-label={s("why")}>
      <p dir="auto">
        <strong>{s("why")}</strong> {x.why}
        {x.why_at ? " · " + clock(x.why_at) : ""}.{" "}
        {x.rule ? (
          <>
            {s("rule_w")} <span class="now-rule">{ruleName(c.rules, x.rule)}</span> {s("puts")}
          </>
        ) : (
          s("why_none")
        )}
      </p>
      <div class="now-acts">
        {canFix ? (
          <button type="button" class="btn btn-ai" onClick={() => fix(x)}>
            {s("fix")}
          </button>
        ) : (
          <button type="button" class="btn btn-neutral" onClick={() => c.open(x.id)}>
            {s("open")}
          </button>
        )}
        <button type="button" class="btn btn-neutral" onClick={() => act(x, "done")}>
          {s("done")}
        </button>
        <button type="button" class="btn btn-neutral" onClick={() => c.later(x)}>
          {s("later")}
        </button>
        <button type="button" class="btn btn-neutral" onClick={() => act(x, "mute")}>
          {s("mute")}
        </button>
        {x.rule && (
          <button type="button" class="btn btn-quiet" onClick={() => go("now/rules")}>
            {s("edit_rule")}
          </button>
        )}
      </div>
    </div>
  );
}

function Row({ x, c }: { x: InboxItem; c: RowCtx }) {
  const [swiped, setSwiped] = H.useState(false);
  const x0 = H.useRef(0);
  const [t, g] = tile(x);
  const open = c.opened === x.id;
  return (
    <li
      class={"now-row" + (swiped ? " is-swiped" : "")}
      data-id={x.id}
      onTouchStart={(e: TouchEvent) => (x0.current = e.touches[0]?.clientX ?? 0)}
      onTouchEnd={(e: TouchEvent) => {
        const dx = (e.changedTouches[0]?.clientX ?? 0) - x0.current;
        if (dx < -50) setSwiped(true);
        else if (dx > 50) setSwiped(false);
      }}
    >
      <div class="now-line">
        <button type="button" class="now-open" aria-expanded={open ? "true" : "false"} onClick={() => c.setOpened(open ? "" : x.id)}>
          <span class="now-mark" aria-hidden="true">
            {x.turn === "me" ? "▶" : ""}
          </span>
          <span class="home-tile now-tile" data-t={t} aria-hidden="true">
            {g}
          </span>
          <span class="now-main">
            <span class="now-title trunc" dir="auto">
              {x.title}
            </span>
            <span class="now-sub trunc" dir="auto">
              {sub(x)}
            </span>
          </span>
          <span class="now-age">{age(x.why_at || x.since, c.now)}</span>
        </button>
        {swiped && (
          <span class="now-swipe">
            <button type="button" class="now-sw-later" onClick={() => c.later(x)}>
              {s("later")}
            </button>
            <button type="button" class="now-sw-done" onClick={() => act(x, "done")}>
              {s("done")}
            </button>
          </span>
        )}
      </div>
      {(x.rule || (x.why && x.turn !== "none")) && (
        <div class="now-chips">
          {x.why && x.turn !== "none" && (
            <span class="now-why" data-k={x.signal ?? ""} dir="auto">
              {x.why}
            </span>
          )}
          {x.rule && (
            <button type="button" class="now-rule" onClick={() => go("now/rules")}>
              rule: {ruleName(c.rules, x.rule)}
            </button>
          )}
          {x.can?.includes("fix") && (
            <button type="button" class="now-fix" onClick={() => fix(x)}>
              {s("fix")}
            </button>
          )}
        </div>
      )}
      {open && <Why x={x} c={c} />}
    </li>
  );
}

function Sec({ id, title, n, items, c, extra }: { id: string; title: string; n: number; items: readonly InboxItem[]; c: RowCtx; extra?: El | false }) {
  return (
    <section class="now-sec" data-sec={id} aria-label={title}>
      <div class="now-sec-h">
        <h2>
          {title} <span class="now-n">{n}</span>
        </h2>
        {extra}
      </div>
      {items.length > 0 && (
        <ul class="now-list">
          {items.map((x) => (
            <Row key={x.id} x={x} c={c} />
          ))}
        </ul>
      )}
    </section>
  );
}

function Later({ x, onClose }: { x: InboxItem; onClose: () => void }) {
  const [v, setV] = H.useState("");
  const pick = (w: string) => {
    onClose();
    act(x, "later", w);
  };
  return (
    <H.Sheet onLater={onClose} label={s("later_q", { title: x.title })}>
      <form
        class="now-later"
        onSubmit={(e: Event) => {
          e.preventDefault();
          if (v.trim()) pick(v.trim());
        }}
      >
        <h2 dir="auto">{s("later_q", { title: x.title })}</h2>
        <input class="now-input" value={v} placeholder={s("later_ph")} aria-label={s("later")} enterkeyhint="done" onInput={(e: Event) => setV((e.currentTarget as HTMLInputElement).value)} />
        <div class="now-picks">
          {(["p_2h", "p_tom", "p_week"] as const).map((k) => (
            <button key={k} type="button" class="chip" onClick={() => pick(s(k))}>
              {s(k)}
            </button>
          ))}
        </div>
        <p class="now-hint">{s("later_hint")}</p>
        <button type="submit" class="btn btn-primary" disabled={!v.trim()}>
          {s("later")}
        </button>
      </form>
    </H.Sheet>
  );
}

/** useRows is the shared state of a list of rows: the open one, the
 * Later sheet, now. */
function useRows(open: Open, info: NowInfo | null): [RowCtx, El | null] {
  const [opened, setOpened] = H.useState("");
  const [later, setLater] = H.useState<InboxItem | null>(null);
  const c: RowCtx = { now: Date.now(), rules: info?.rules ?? [], open, opened, setOpened, later: setLater };
  return [c, later && <Later x={later} onClose={() => setLater(null)} />];
}

function Setup() {
  return (
    <section class="now-setup" aria-label={s("setup")}>
      <h2>{s("setup")}</h2>
      <ul>
        <li>☑ {s("setup_top")}</li>
        <li>☑ {s("setup_from")}</li>
        <li>◉ {s("setup_show", { n: DEFAULTS.show })}</li>
      </ul>
      <div class="now-acts">
        <button type="button" class="btn btn-primary" onClick={() => void req("now.rules", { setup: DEFAULTS }).catch(fail)}>
          {s("use")}
        </button>
        <button type="button" class="btn btn-neutral" onClick={() => go("now/setup")}>
          {s("change")}
        </button>
      </div>
    </section>
  );
}

function Clear({ sec, tab }: { sec: Sections; tab: (t: string) => void }) {
  return (
    <div class="now-clear">
      <span class="now-check" aria-hidden="true">
        ✓
      </span>
      <strong>{s("clear")}</strong>
      <p>{s("clear_sub")}</p>
      <div class="now-picks">
        <button type="button" class="chip" onClick={() => tab("waiting")}>
          {s("waiting_others")} {sec.waiting.length}
        </button>
        <button type="button" class="chip" onClick={() => tab("all")}>
          {s("else")} {sec.other.length}
        </button>
      </div>
    </div>
  );
}

let seen: Set<string> | null = null;

/** badge puts the count on the ✦ button (radial.css draws --now-badge). */
function badge(n: number): void {
  const st = document.documentElement.style;
  if (n > 0) st.setProperty("--now-badge", `"${n > 99 ? "99+" : n}"`);
  else st.removeProperty("--now-badge");
}

function Home({ open }: { open: Open }) {
  const { items, info } = read();
  const [c, sheet] = useRows(open, info);
  const sec = sections(items, c.now);
  const sum = summary(sec);
  H.useEffect(() => badge(sum.need), [sum.need]);
  // A new item on the user's turn: one quiet line, no popup.
  const fresh = seen ? sec.turn.find((x) => !seen?.has(x.id)) : undefined;
  H.useEffect(() => {
    seen = new Set(sec.turn.map((x) => x.id));
  });
  const show = info?.show || 5;
  const mine = [...sec.overdue, ...sec.turn];
  return (
    <div class="now-home">
      <p class="now-sum">
        <strong>{s("sum_need", { n: sum.need })}</strong>
        {sum.running > 0 ? " · " + sum.running + " " + s("running").toLowerCase() : ""}
        <span class="now-sum-sub">{s("sum_sub")}</span>
      </p>
      {info && !info.setup && <Setup />}
      {fresh && (
        <p class="now-note" dir="auto">
          ● {s("new", { title: fresh.title })} · {age(fresh.why_at || fresh.since, c.now)}
        </p>
      )}
      {mine.length === 0 ? (
        <Clear sec={sec} tab={(t) => H.go({ tab: "home", rest: "now", query: { tab: t } })} />
      ) : (
        <Sec
          id="turn"
          title={s("turn")}
          n={mine.length}
          items={mine.slice(0, show)}
          c={c}
          extra={
            <button type="button" class="now-all" onClick={() => go("now")}>
              {s("see_all", { n: items.length })}
            </button>
          }
        />
      )}
      {sec.running.length > 0 && <Sec id="running" title={s("running")} n={sec.running.length} items={sec.running} c={c} />}
      {sheet}
    </div>
  );
}

function Head({ title, back, gear }: { title: string; back: string; gear?: boolean }) {
  return (
    <header class="now-head">
      <button type="button" class="now-back" aria-label={s("back")} onClick={() => go(back)}>
        ‹
      </button>
      <h1>{title}</h1>
      {gear && (
        <button type="button" class="now-gear" aria-label={s("settings")} onClick={() => go("now/rules")}>
          ⚙
        </button>
      )}
    </header>
  );
}

function All({ open, tab }: { open: Open; tab: string }) {
  const { items, info } = read();
  const [c, sheet] = useRows(open, info);
  const sec = sections(items, c.now);
  const setTab = (t: string) => H.go({ tab: "home", rest: "now", query: t === "turn" ? {} : { tab: t } });
  const mine = sec.turn.length + sec.overdue.length;
  const tabs: [string, NowKey, number][] = [
    ["turn", "turn", mine],
    ["waiting", "waiting", sec.waiting.length],
    ["all", "all", items.length],
  ];
  const nav = (t: string, k: NowKey, n: number) => (
    <button type="button" class="now-nav" onClick={() => setTab(t)}>
      <span>{s(k)}</span>
      <span class="now-n">{n} ›</span>
    </button>
  );
  return (
    <div class="now-page" role="region" aria-label={s("now")}>
      <Head title={s("now")} back="" gear />
      <nav class="now-tabs" aria-label={s("now")}>
        {tabs.map(([id, k, n]) => (
          <button key={id} type="button" aria-pressed={tab === id ? "true" : "false"} onClick={() => setTab(id)}>
            {s(k)} <span class="now-n">{n}</span>
          </button>
        ))}
      </nav>
      <div class="now-body">
        {tab !== "waiting" && sec.turn.length > 0 && <Sec id="turn" title={s("turn")} n={sec.turn.length} items={sec.turn} c={c} />}
        {tab !== "waiting" && sec.overdue.length > 0 && (
          <Sec
            id="overdue"
            title={s("overdue")}
            n={sec.overdue.length}
            items={sec.overdue}
            c={c}
            extra={
              <button type="button" class="now-all" onClick={() => sec.overdue.forEach((x) => act(x, "later", "tomorrow"))}>
                {s("tomorrow_all")}
              </button>
            }
          />
        )}
        {tab === "turn" && mine === 0 && <Clear sec={sec} tab={setTab} />}
        {tab !== "turn" && <Sec id="waiting" title={s("waiting_others")} n={sec.waiting.length} items={sec.waiting} c={c} />}
        {tab === "all" && <Sec id="other" title={s("else")} n={sec.other.length} items={sec.other} c={c} />}
        {tab === "turn" && (
          <div class="now-navs">
            {nav("waiting", "waiting_others", sec.waiting.length)}
            {nav("all", "else", sec.other.length)}
          </div>
        )}
      </div>
      {sheet}
    </div>
  );
}

function Toggle({ on, label, onChange }: { on: boolean; label: string; onChange: (v: boolean) => void }) {
  return <button type="button" role="switch" class="now-switch" aria-checked={on ? "true" : "false"} aria-label={label} onClick={() => onChange(!on)} />;
}

function Rules() {
  const { info } = read();
  const rules = info?.rules ?? [];
  const set = (i: number, on: boolean) => void req("now.rules", { rules: rules.map((r, j) => (j === i ? { ...r, on } : r)) }).catch(fail);
  return (
    <div class="now-page" role="region" aria-label={s("rules")}>
      <Head title={s("rules")} back="now" />
      <div class="now-body">
        <p class="now-sum">
          <strong>{s("rules_n", { n: rules.length })}</strong>
          <span class="now-sum-sub">{s("rules_sub")}</span>
        </p>
        <ul class="now-list now-rules">
          {rules.map((r, i) => (
            <li key={r.id} class="now-rulerow">
              <span class="now-main">
                <span class="now-title">
                  {r.name} <span class="now-lvl">{r.level >= 1 && r.level <= 4 ? "P" + r.level : "–"}</span>
                </span>
                <span class="now-sub">{r.sentence}</span>
              </span>
              <span class="now-n">{r.id === "nobots" ? s("hidden", { n: r.count }) : r.count}</span>
              <Toggle on={r.on} label={r.name} onChange={(v) => set(i, v)} />
            </li>
          ))}
        </ul>
        <div class="now-tip">
          <strong>{s("from_item")}</strong>
          <p>{s("from_item_sub")}</p>
        </div>
      </div>
    </div>
  );
}

function Check({ on, label, onChange }: { on: boolean; label: string; onChange: (v: boolean) => void }) {
  return (
    <label class="now-check-row">
      <input type="checkbox" checked={on} onChange={(e: Event) => onChange((e.currentTarget as HTMLInputElement).checked)} /> {label}
    </label>
  );
}

function SetupPage() {
  const { info } = read();
  const [a, setA] = H.useState({ top: info?.top ?? DEFAULTS.top, from: info?.from ?? DEFAULTS.from, show: info?.show || DEFAULTS.show, hide_bots: info?.hide_bots ?? DEFAULTS.hide_bots });
  const flip = (k: "top" | "from", v: string, on: boolean) => setA({ ...a, [k]: on ? [...a[k], v] : a[k].filter((x) => x !== v) });
  const send = (args: unknown) =>
    req("now.rules", args).then(() => go(""), fail);
  return (
    <div class="now-page" role="region" aria-label={s("setup_t")}>
      <Head title={s("setup_t")} back="" />
      <div class="now-body now-qs">
        <p class="now-sum-sub">{s("setup_sub")}</p>
        <fieldset>
          <legend>{s("q1")}</legend>
          {(["agent", "ci_red", "review", "mention"] as const).map((k) => (
            <Check key={k} on={a.top.includes(k)} label={s(("a_" + k) as NowKey)} onChange={(v) => flip("top", k, v)} />
          ))}
        </fieldset>
        <fieldset>
          <legend>{s("q2")}</legend>
          <div class="now-picks">
            {SOURCES.map(([id, name]) => (
              <button key={id} type="button" class="chip-toggle" aria-pressed={a.from.includes(id) ? "true" : "false"} onClick={() => flip("from", id, !a.from.includes(id))}>
                {name}
              </button>
            ))}
          </div>
        </fieldset>
        <fieldset>
          <legend>{s("q3")}</legend>
          <div class="now-picks">
            {[3, 5, 10].map((n) => (
              <button key={n} type="button" class="chip-toggle" aria-pressed={a.show === n ? "true" : "false"} onClick={() => setA({ ...a, show: n })}>
                {n}
              </button>
            ))}
          </div>
        </fieldset>
        <fieldset>
          <legend>{s("q4")}</legend>
          <Check on={a.hide_bots} label={s("a_bots")} onChange={(v) => setA({ ...a, hide_bots: v })} />
        </fieldset>
        <div class="now-acts">
          <button type="button" class="btn btn-primary" onClick={() => send({ setup: a })}>
            {s("use")}
          </button>
          <button type="button" class="btn btn-neutral" onClick={() => send({ skip: true })}>
            {s("skip")}
          </button>
        </div>
      </div>
    </div>
  );
}

/** openItem opens what a row names ("<kind>:<ref>"): a card's sheet, or
 * the view of its source (as the Home's rows do). */
function openItem(id: string): void {
  const i = id.indexOf(":");
  const kind = id.slice(0, i), ref = id.slice(i + 1);
  const more: Record<string, string> = { run: "runs", task: "tasks", workflow: "workflows", lane: "agents", race: "agents" };
  if (kind === "card") H.go({ ...H.route.peek(), rest: "", card: ref });
  else if (kind === "ci") H.goTab("git");
  else if (more[kind]) H.go({ tab: "more", rest: more[kind], query: {} });
}

/** Now is the Home's Now: its top while empty, and its pages over it. */
export function Now({ n }: { n: number }) {
  const open = openItem;
  const r = H.route.value;
  let page: El | null = null;
  if (r.tab === "home") {
    if (r.rest === "now/rules") page = <Rules />;
    else if (r.rest === "now/setup") page = <SetupPage />;
    else if (r.rest === "now") page = <All open={open} tab={r.query.tab ?? "turn"} />;
    else if (!n) page = <Home open={open} />;
  }
  return (
    <>
      {page}
      <Toast />
    </>
  );
}
