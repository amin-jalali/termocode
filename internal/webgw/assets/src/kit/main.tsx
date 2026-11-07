// The component kit (WEB-UX-1): every component of the web UI in every
// state of web UX section 5, in dark and light, at 390 / 768 / 1280 px.
// Served only by a dev build (go build -tags webdev) at /dev/kit. The
// texts here are sample data; the components' own words come from
// strings.ts like everywhere else.
import { installSafety } from "../lib/safety";
import { Fragment, h, render, type ComponentChildren } from "preact";
import { signal } from "@preact/signals";
import { useState } from "preact/hooks";
import "../styles/tokens.css";
import "../styles/base.css";
import "../styles/shell.css";
import "../styles/ui.css";
import "../styles/components.css";
import "../styles/phone.css";
import "../styles/home.css";
import "../styles/chat.css";
import "../styles/blocks.css";
import "../styles/customize.css";
import "./kit.css";
import { toast } from "../lib/toasts";
import type { StatusView } from "../state/status";
import { t, tn, type StringKey } from "../strings";
import { AttentionView } from "../ui/AttentionPill";
import { Blocks } from "../ui/Blocks";
import { Button } from "../ui/Button";
import { Card, type CardKind, type CardState } from "../ui/Card";
import { Chip } from "../ui/Chip";
import { ConnectScreen, type ConnectState } from "../ui/ConnectScreen";
import { Bar } from "../ui/OfflineBar";
import { Nav } from "../ui/Nav";
import { CodeRow, DiffRow, TermRow, TreeRow } from "../ui/rows";
import { Segmented } from "../ui/Segmented";
import { Sheet } from "../ui/Sheet";
import { Progress, SkeletonBubbles, SkeletonRows } from "../ui/Skeleton";
import { StripView } from "../ui/Strip";
import { Toasts, ToastView } from "../ui/Toasts";
import { WaitingLaptop } from "../ui/Waiting";
import { Block as PhoneBlock, BoardTile, Composer, PhoneCard, PhoneChip, PhoneChips, PhoneShell, PhoneSheet, Row, Rows, StatusMark, STATUS, StreamMsg, type Status } from "../phone/index";
import { ChipRow } from "../home/ChipRow";
import type { ChipState } from "../home/chips";
import { KitCustomize } from "./customize";
import "./replay";
import type { Block as ProtoBlock } from "../../../../webproto/proto";

/** blockSamples: one block of each kind (MOB-3), plus one kind this page
 * does not know. Sample data only. */
const KIT_NOW = Date.UTC(2026, 10, 7, 12, 0, 0);
const prRef = { kind: "pr", id: "418" };
const blockSamples: ProtoBlock[] = [
  {
    id: "k1", kind: "list", title: "Open pull requests", live: true, total: 5, truncated: true,
    items: [
      { id: "418", title: "fix toast", sub: "main ← fix/toast", tone: "bad", badge: "CI ✗", time: KIT_NOW - 2 * 3600e3, ref: prRef, actions: [{ id: "chat", label: "To chat", kind: "attach", ref: prRef }] },
      { id: "412", title: "login rate limit", sub: "main ← feat/rate", tone: "busy", badge: "CI ◐", time: KIT_NOW - 86400e3 },
      { id: "409", title: "docs: pairing", tone: "ok", badge: "CI ✓", time: KIT_NOW - 3 * 86400e3 },
    ],
  },
  {
    id: "k2", kind: "table", title: "Orders today",
    columns: [{ id: "n", label: "#", kind: "num" }, { id: "who", label: "customer" }, { id: "st", label: "state", kind: "tone" }, { id: "sum", label: "total", kind: "cost" }],
    rows: [["1042", "Ada", "ok", "$12.00"], ["1043", "Grace", "warn", "$7.50"], ["1044", "Linus", "bad", "$0.00"]],
    spec: { view: "db.rows", fields: ["n", "who", "st", "sum"], sort: [{ field: "n", desc: true }], limit: 20 },
  },
  {
    id: "k3", kind: "diff", title: "internal/app/model.go",
    diff: { path: "internal/app/model.go", adds: 2, dels: 1, hunks: [{ header: "@@ -10,3 +10,4 @@ func (m Model) View()", lines: [{ k: "ctx", a: 10, b: 10, text: "\tvar b strings.Builder" }, { k: "del", a: 11, text: "\tb.WriteString(m.title)" }, { k: "add", b: 11, text: "\tb.WriteString(m.header())" }, { k: "add", b: 12, text: "\tb.WriteString(m.body())" }] }] },
    actions: [{ id: "laptop", label: "Show on laptop", kind: "laptop" }],
  },
  { id: "k4", kind: "card", title: "Run 12 · review", fields: [{ label: "state", value: "running", tone: "busy" }, { label: "step", value: "3/5" }, { label: "cost", value: "$0.42", kind: "cost" }, { label: "model", value: "sonnet", kind: "mono" }] },
  { id: "k5", kind: "detail", title: "Container web", fields: [{ label: "image", value: "nginx:1.27", kind: "mono" }, { label: "state", value: "up 3 h", tone: "ok" }, { label: "ports", value: "8080 → 80", kind: "mono" }] },
  { id: "k6", kind: "chart", title: "AI cost today", bars: [{ label: "claude", value: 1.2, text: "$1.20", tone: "info" }, { label: "codex", value: 0.4, text: "$0.40", tone: "info" }, { label: "local", value: 0, text: "$0.00", tone: "idle" }] },
  { id: "k7", kind: "status", title: "CI on main", state: "ok" },
  { id: "k8", kind: "form", title: "New issue", form: [{ id: "title", label: "Title", kind: "text", required: true }, { id: "kind", label: "Kind", kind: "choice", choices: ["bug", "idea"] }, { id: "urgent", label: "Urgent", kind: "bool" }, { id: "body", label: "Details", kind: "long", hint: "Plain text" }] },
  { id: "k9", kind: "json", title: "Raw result", json: '{"ok":true,"items":[1,2,3]}' },
  { id: "k10", kind: "text", title: "Answer", for_you: "Two files changed; tests pass.", md: { k: "doc", c: [{ k: "p", c: [{ k: "text", text: "The toast now waits for the " }, { k: "code", text: "Esc" }, { k: "text", text: " key." }] }] } },
  {
    id: "k11", kind: "board", title: "Board",
    board: { columns: [
      { id: "need", label: "Needs you", tone: "warn", items: [{ id: "c1", title: "Allow go test", sub: "termocode · lane 2", tone: "warn" }] },
      { id: "work", label: "Working", tone: "busy", items: [{ id: "r12", title: "review fix/toast", sub: "step 3/5 · $0.42", tone: "busy" }] },
      { id: "rev", label: "Review", tone: "info", items: [] },
      { id: "done", label: "Done", tone: "ok", items: [{ id: "r9", title: "docs: pairing", tone: "ok" }] },
    ] },
  },
  { id: "k12", kind: "map" as ProtoBlock["kind"], title: "A block from a newer laptop" },
  { id: "k13", kind: "status", title: "Failing CI · shop", state: "bad", stale: true },
];

installSafety();

type Width = 390 | 768 | 1280;
type Themes = "both" | "dark" | "light";
const width = signal<Width>(390);
const themes = signal<Themes>("both");

const baseStatus: StatusView = {
  root: "",
  workspace: "termocode",
  branch: "main",
  dirty: 2,
  aiBusy: false,
  cardsWaiting: 0,
  termCount: 2,
  termBusy: 0,
  runsLive: 0,
  runsNeedYou: 0,
  webClients: 1,
  update: "",
};

/** Pair shows children once per theme, each in a frame of the chosen width. */
function Pair({ children, tall }: { children: () => ComponentChildren; tall?: boolean }) {
  const list: ("dark" | "light")[] = themes.value === "both" ? ["dark", "light"] : [themes.value];
  return (
    <div class="pair">
      {list.map((th) => (
        <div key={th} class={`frame theme-${th}${tall ? " is-tall" : ""}`} data-w={width.value}>
          <span class="frame-tag">{th}</span>
          {children()}
        </div>
      ))}
    </div>
  );
}

// The Home's chip row (MOBUX-3) in two states of the laptop.
const chipStates: Record<"calm" | "waits", ChipState> = {
  calm: { needsYou: 0, working: 0, review: 0, ciRed: 0 },
  waits: { needsYou: 2, working: 1, review: 3, ciRed: 1 },
};
const chipState = signal<"calm" | "waits">("calm");

function KitChips() {
  return (
    <>
      <div class="kit-row">
        <button type="button" class="btn" onClick={() => (chipState.value = "calm")}>
          Calm laptop
        </button>
        <button type="button" class="btn" onClick={() => (chipState.value = "waits")}>
          Something waits
        </button>
      </div>
      <Pair>{() => <ChipRow state={chipStates[chipState.value]} onPick={(id) => toast("info", "Chip " + id)} />}</Pair>
    </>
  );
}

function Section({ id, title, note, children }: { id: string; title: string; note?: string; children: ComponentChildren }) {
  return (
    <section class="kit-section" id={id} aria-labelledby={`k-${id}`}>
      <h2 class="kit-h2" id={`k-${id}`}>
        <span class="kit-hash">#</span>
        {title}
      </h2>
      {note && <p class="kit-note">{note}</p>}
      {children}
    </section>
  );
}

function State({ name, children }: { name: string; children: ComponentChildren }) {
  return (
    <div class="kit-state">
      <div class="kit-state-name">{name}</div>
      {children}
    </div>
  );
}

const colorTokens = [
  "--bg", "--bg-2", "--bg-panel", "--bg-float", "--bg-hover", "--sel", "--strip", "--on-strip", "--text", "--text-2",
  "--muted", "--dim", "--border", "--focus", "--ai", "--on-ai", "--blue", "--green", "--amber", "--red", "--git-mod",
  "--git-add", "--git-del", "--git-conf", "--diff-add-bg", "--diff-del-bg",
];

function Tokens() {
  return (
    <>
      <div class="swatches">
        {colorTokens.map((name) => (
          <div key={name} class="swatch">
            <span class="swatch-chip" style={{ background: `var(${name})` }} />
            <code>{name}</code>
          </div>
        ))}
      </div>
      <div class="type-scale">
        <div class="ts-hero">termocode 24/30</div>
        <div class="ts-view">View title 20/26</div>
        <div class="ts-title">Sheet title 17/24</div>
        <div class="ts-body">Chat and body text 15/22 — the AI changed two files.</div>
        <div class="ts-row">mono 13/20 · internal/app/model.go:88</div>
        <div class="ts-hint">mono 12/18 · Enter  Deny · Esc  Later</div>
      </div>
    </>
  );
}

const diffSample = (
  <div class="code-box">
    <DiffRow kind="hunk" text="@@ -84,7 +84,8 @@ func (m *Model) quit() tea.Cmd" />
    <DiffRow kind="ctx" a={84} b={84} text="	if m.dirty() {" />
    <DiffRow kind="del" a={85} text="		return m.confirmQuit()" />
    <DiffRow kind="add" b={85} text="		return m.confirmQuit(confirm.SafeFirst)" />
    <DiffRow kind="add" b={86} text="		// Cancel stays first and focused." />
    <DiffRow kind="ctx" a={86} b={87} text="	}" />
  </div>
);

function cardBody(kind: CardKind): ComponentChildren {
  switch (kind) {
    case "review":
      return diffSample;
    case "plan":
      return (
        <ol class="plan-steps">
          <li>Read internal/app/quit.go and its tests.</li>
          <li>Make Cancel the first, focused choice.</li>
          <li>Run go test ./internal/app/ -run TestQuit.</li>
        </ol>
      );
    case "mcp":
      return <p>An outside agent asks to read 3 files in internal/app.</p>;
    case "trust":
      return <p>Trusting a new folder changes what the AI may touch. Answer this one on the laptop.</p>;
    default:
      return null;
  }
}

const subjects: Partial<Record<CardKind, string>> = {
  tool: "go test ./internal/app/ -run TestQuit",
  review: "internal/app/quit.go",
  mcp: "files.read  internal/app/*.go",
};

function CardDemo({ kind, state, id }: { kind: CardKind; state: CardState; id: string }) {
  return (
    <Card
      id={id}
      kind={kind}
      state={state}
      mode="inline"
      subject={subjects[kind]}
      where={kind === "tool" ? "terminal 2 · ~/termocode" : undefined}
      onAnswer={(c) => toast(c === "safe" ? "info" : "ok", c === "safe" ? t("toast.denied") : t("toast.allowed"))}
      onLater={() => toast("info", t("card.later"))}
      onClose={() => toast("info", t("close"))}
      onLaptop={() => toast("info", t("laptop.shown"))}
    >
      {cardBody(kind)}
    </Card>
  );
}

/** answerToast is what the kit says after each answer, per kind. */
const answerToast: Record<CardKind, { safe: StringKey; risky: StringKey }> = {
  tool: { safe: "toast.denied", risky: "toast.allowed" },
  mcp: { safe: "toast.denied", risky: "toast.allowed" },
  plan: { safe: "toast.denied", risky: "toast.allowed" },
  review: { safe: "toast.rejected", risky: "toast.accepted" },
  trust: { safe: "card.later", risky: "laptop.shown" },
  workflow: { safe: "toast.denied", risky: "toast.allowed" },
};

const sheetKinds: CardKind[] = ["tool", "review", "plan", "mcp", "trust"];

/** SheetDemo opens a real card dialog of each kind in a sheet: a fresh
 * card each time, so its arming starts over (the e2e card pack drives it). */
function SheetDemo() {
  const [open, setOpen] = useState<"" | CardKind | "wait">("");
  const close = (msg: StringKey) => {
    setOpen("");
    toast("info", t(msg));
  };
  return (
    <div class="kit-row wrap">
      {sheetKinds.map((k) => (
        <Button key={k} kind={k === "tool" ? "ai" : "neutral"} onClick={() => setOpen(k)}>
          {`Open a ${k} card`}
        </Button>
      ))}
      <Button onClick={() => setOpen("wait")}>Open Waiting for the laptop</Button>
      {open !== "" && open !== "wait" && (
        <Sheet onLater={() => close("card.later")}>
          <Card
            id={`sheet-${open}`}
            kind={open}
            subject={subjects[open]}
            where={open === "tool" ? "terminal 2 · ~/termocode" : undefined}
            onAnswer={(c) => close(answerToast[open][c])}
            onLater={() => close("card.later")}
            onCancel={() => close("card.cancelled")}
            onLaptop={() => close("laptop.shown")}
          >
            {cardBody(open)}
          </Card>
        </Sheet>
      )}
      {open === "wait" && (
        <Sheet onLater={() => setOpen("")}>
          <WaitingLaptop since={Date.now()} onCancel={() => (setOpen(""), toast("info", t("wait.cancelled")))} />
        </Sheet>
      )}
    </div>
  );
}

function Connect({ state }: { state: ConnectState }) {
  return (
    <div class="connect-frame">
      <ConnectScreen state={state} focus={false} onOpen={() => toast("info", t("connect.opening"))} />
    </div>
  );
}

const phoneStatuses = Object.keys(STATUS) as Status[];

/** PhoneKit shows the phone app's base components (MOBUX-1). */
function PhoneKit() {
  const [text, setText] = useState("");
  const [attached, setAttached] = useState([{ id: "task:412", label: "#412 login rate limit" }]);
  return (
    <div class="ph-kit">
      <div class="ph-kit-marks">
        {phoneStatuses.map((s) => (
          <span key={s} class="ph-kit-mark">
            <StatusMark s={s} hidden /> {s}
          </span>
        ))}
      </div>
      <div class="ph-kit-cards">
        <PhoneCard status="needs" value={2} detail="run go test · shop" onClick={() => toast("info", "Needs you")} />
        <PhoneCard status="working" value={3} detail="2 lanes · 1 workflow" onClick={() => toast("info", "Working")} />
        <PhoneCard status="done" title="Done today" value={7} detail="$4.20" />
      </div>
      <PhoneChips label="Chips">
        <PhoneChip label="Needs you" status="needs" count={2} onClick={() => undefined} />
        <PhoneChip label="Board" pinned on onClick={() => undefined} />
        <PhoneChip label="Projects" pinned onClick={() => undefined} />
        <PhoneChip label="Review" onClick={() => undefined} />
        <PhoneChip label="Git" onClick={() => undefined} />
      </PhoneChips>
      <PhoneBlock kind="table" title="Pull requests" status="working" live onCustomize={() => toast("info", "Customize")} actions={<Button small>Refresh</Button>}>
        <Rows label="Pull requests">
          <Row status="failed" title="#418 fix toast" meta="CI ✗ · 2 h" end="2 h" onClick={() => undefined} />
          <Row status="working" live title="#412 login rate limit" meta="CI ◐ · 1 d" end="1 d" onClick={() => undefined} onLongPress={() => toast("info", "Row menu")} />
          <Row status="done" title="#409 docs: pairing" meta="CI ✓ · 3 d" end="3 d" />
        </Rows>
      </PhoneBlock>
      <div class="ph-kit-tiles">
        <BoardTile status="needs" title="card: run go test" project="shop" agent="lane fix-login" ci="failed" onClick={() => undefined} />
        <BoardTile status="working" live title="rate limit" project="shop" agent="sonnet" step="3/5" cost="$0.80" ci="working" onClick={() => undefined} />
        <BoardTile status="review" title="race #3" project="api" agent="3 lanes" step="2 done" cost="$1.10" />
        <BoardTile status="done" title="lane fix-login +41 −9" project="shop" ci="done" />
      </div>
      <Composer
        value={text}
        onInput={setText}
        onSend={() => toast("ok", "Sent: " + text)}
        attached={attached}
        onRemove={(id) => setAttached(attached.filter((a) => a.id !== id))}
      />
      <Composer value="" onInput={() => undefined} onSend={() => undefined} offline />
    </div>
  );
}

// The phone app shell (MOBUX-2): one state for both theme frames.
const shellProjects = [
  { id: "shop", name: "shop", where: "~/src/shop" },
  { id: "api", name: "api", where: "~/src/api" },
  { id: "docs", name: "docs", where: "~/notes/docs" },
];
const shellProject = signal("shop");
const shellOffline = signal(false);
const shellMsgs = signal<{ from: "you" | "ai"; text: string }[]>([
  { from: "you", text: "show open PRs as a table, newest first" },
  { from: "ai", text: "5 open pull requests:" },
]);
const shellDraft = signal("");

function ShellControls() {
  return (
    <div class="kit-row">
      <button type="button" class="btn" onClick={() => (shellOffline.value = !shellOffline.value)}>
        Shell offline
      </button>
      <button
        type="button"
        class="btn"
        onClick={() => (shellMsgs.value = [...shellMsgs.value, { from: shellMsgs.value.length % 2 ? "you" : "ai", text: "Message " + (shellMsgs.value.length + 1) + ": the lane fix-login is on step 3 of 5." }])}
      >
        Shell: add a message
      </button>
    </div>
  );
}

function ShellDemo() {
  const p = shellProjects.find((x) => x.id === shellProject.value) ?? shellProjects[0];
  return (
    <div class="ph-kit-shell">
      <PhoneShell
        project={p}
        projects={shellProjects}
        onProject={(id) => (shellProject.value = id)}
        link={shellOffline.value ? "offline" : "live"}
        onMenu={() => toast("info", "All tools")}
        chips={<ChipRow state={chipStates.waits} onPick={(id) => toast("info", "Chip " + id)} />}
        composer={{
          value: shellDraft.value,
          onInput: (v) => (shellDraft.value = v),
          onSend: () => {
            shellMsgs.value = [...shellMsgs.value, { from: "you", text: shellDraft.value }];
            shellDraft.value = "";
          },
          onAttach: () => toast("info", "Attach"),
        }}
      >
        {shellMsgs.value.map((m, i) => (
          <StreamMsg key={i} from={m.from}>
            {m.text}
          </StreamMsg>
        ))}
        <PhoneBlock kind="table" title="Pull requests" status="working" live>
          <Rows label="Pull requests">
            <Row status="failed" title="#418 fix toast" meta="CI ✗ · 2 h" end="2 h" onClick={() => undefined} />
            <Row status="working" title="#412 login rate limit" meta="CI ◐ · 1 d" end="1 d" onClick={() => undefined} />
          </Rows>
        </PhoneBlock>
      </PhoneShell>
    </div>
  );
}

function PhoneSheetDemo() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button onClick={() => setOpen(true)}>Open phone sheet</Button>
      {open && (
        <PhoneSheet
          title="shop · #412 open"
          onLater={() => setOpen(false)}
          actions={[
            { label: "Just start", risky: true, onClick: () => setOpen(false) },
            { label: "Talk about it", primary: true, onClick: () => setOpen(false) },
            { label: "Cancel", safe: true, onClick: () => setOpen(false) },
          ]}
        >
          <p class="kit-note">Cancel comes first and has focus; Just start wakes after 600 ms.</p>
        </PhoneSheet>
      )}
    </>
  );
}

function Kit() {
  const cardKinds: CardKind[] = ["tool", "review", "plan", "mcp", "trust"];
  const cardStates: CardState[] = ["answered", "offline", "gone", "viewonly", "long"];
  return (
    <div class="kit">
      <header class="kit-top">
        <span class="brand">termocode</span>
        <span class="kit-top-title">component kit</span>
        <span class="strip-gap" />
        <div class="kit-ctl">
          <Segmented<string>
            label="Width"
            value={String(width.value)}
            choices={[
              { value: "390", label: "390" },
              { value: "768", label: "768" },
              { value: "1280", label: "1280" },
            ]}
            onChange={(v) => (width.value = Number(v) as Width)}
          />
          <Segmented<Themes>
            label="Themes"
            value={themes.value}
            choices={[
              { value: "both", label: "both" },
              { value: "dark", label: "dark" },
              { value: "light", label: "light" },
            ]}
            onChange={(v) => (themes.value = v)}
          />
        </div>
      </header>
      <nav class="kit-nav" aria-label="Sections">
        {["tokens", "strip", "nav", "pill", "bars", "buttons", "cards", "sheets", "toasts", "chips", "term", "code", "diff", "tree", "loading", "empty", "waiting", "connect", "misc", "phone", "shell"].map((s) => (
          <a key={s} class="chip" href={`#${s}`}>
            {s}
          </a>
        ))}
      </nav>
      <main class="kit-main">
        <Section id="tokens" title="Tokens" note="Colour tokens of web UX 7.1 (AA checked by TestWebTokensContrast) and the type scale.">
          <Pair>{() => <Tokens />}</Pair>
        </Section>

        <Section id="strip" title="Status strip" note="Always LTR, mono facts. The pill tints with the state; the ✦ breathes and the comet runs while the AI works.">
          <Pair>
            {() => (
              <>
                <State name="idle · RTT 40 ms">
                  <StripView s={baseStatus} online rtt={40} />
                </State>
                <State name="AI working · RTT 220 ms (slow)">
                  <StripView s={{ ...baseStatus, aiBusy: true, branch: "feat/web-ux-1" }} online rtt={220} />
                </State>
                <State name="needs you · RTT 640 ms (bad)">
                  <StripView s={{ ...baseStatus, cardsWaiting: 2, dirty: 0 }} online rtt={640} />
                </State>
                <State name="offline (hollow dot)">
                  <StripView s={baseStatus} online={false} rtt={null} />
                </State>
              </>
            )}
          </Pair>
        </Section>

        <Section id="nav" title="Tab bar and rail" note="Labels always visible; badges sit in reserved space; the active tab has a 2 px lavender line.">
          <Pair>
            {() => (
              <>
                <State name="tab bar · badges">
                  <Nav
                    variant="tabs"
                    active="chat"
                    badges={{ chat: { kind: "count", n: 2, tone: "ai" }, term: { kind: "dot", tone: "amber" }, git: { kind: "count", n: 5, tone: "plain" }, more: { kind: "dot", tone: "blue" } }}
                  />
                </State>
                <State name="tab bar · quiet">
                  <Nav variant="tabs" active="git" badges={{}} />
                </State>
                <State name="rail">
                  <div class="rail-box">
                    <Nav variant="rail" active="files" badges={{ chat: { kind: "count", n: 1, tone: "ai" }, term: { kind: "dot", tone: "amber" } }} />
                  </div>
                </State>
              </>
            )}
          </Pair>
        </Section>

        <Section id="pill" title="Attention pill" note="One line above the tab bar, on every tab; the most urgent thing only.">
          <Pair>
            {() => (
              <>
                <AttentionView kind="cards" n={1} />
                <AttentionView kind="cards" n={3} />
                <AttentionView kind="run_done" />
                <AttentionView kind="run_failed" />
                <AttentionView kind="task_done" />
                <AttentionView kind="waiting_laptop" n={1} />
              </>
            )}
          </Pair>
        </Section>

        <Section id="bars" title="Offline bar and hints" note="Lost (amber, countdown, Try now), back (green, 2 s), final (red, no retry).">
          <Pair>
            {() => (
              <>
                <Bar kind="lost" text={t("offline.lost", { s: 4 })} action={<Button small>{t("offline.try")}</Button>} />
                <Bar kind="lost" text={t("offline.trying")} action={<Button small>{t("offline.try")}</Button>} />
                <Bar kind="back" text={t("offline.back", { s: 42 })} />
                <Bar kind="final" text={tn("e.idle", 15)} sub={t("e.still_running")} />
                <Bar kind="final" text={t("e.version")} action={<Button small>{t("e.reload")}</Button>} />
                <Bar kind="final" text={t("e.link_expired")} />
                <div class="hint" role="note">
                  <span class="star" aria-hidden="true">✦</span>
                  <span class="hint-text">{t("firstrun")}</span>
                  <button type="button" class="btn btn-quiet btn-small">
                    {t("firstrun.ok")}
                  </button>
                </div>
              </>
            )}
          </Pair>
        </Section>

        <Section id="buttons" title="Buttons" note="Neutral, primary, AI, danger text, quiet; a button that needs the laptop reads Offline while the link is down (always, on this page).">
          <Pair>
            {() => (
              <div class="kit-row wrap">
                <Button>{t("cancel")}</Button>
                <Button kind="primary">{t("connect.open")}</Button>
                <Button kind="ai">{t("card.allow_once")}</Button>
                <Button kind="danger">{t("git.discard")}</Button>
                <Button kind="quiet">{t("firstrun.ok")}</Button>
                <Button small>{t("offline.try")}</Button>
                <Button disabled>{t("file.save")}</Button>
                <Button kind="primary" needsLink>
                  {t("send")}
                </Button>
              </div>
            )}
          </Pair>
        </Section>

        <Section id="cards" title="Approval cards" note="Deny / Reject / Later first and focused in a dialog; the risky button fills for 600 ms before it wakes (again after scrolling back into view). Enter = the focused button, Esc = Later. No Always allow.">
          <Pair>
            {() => (
              <div class="card-grid">
                {cardKinds.map((k) => (
                  <State key={k} name={`${k} · open`}>
                    <CardDemo id={`${k}-o`} kind={k} state="open" />
                  </State>
                ))}
                {cardStates.map((s) => (
                  <State key={s} name={`tool · ${s}`}>
                    <CardDemo id={`tool-${s}`} kind={s === "long" ? "review" : "tool"} state={s} />
                  </State>
                ))}
              </div>
            )}
          </Pair>
        </Section>

        <Section id="sheets" title="Sheets" note="Bottom sheet on phones (drag the grip down = Later), centred dialog from 720 px. Focus is trapped and restored.">
          <SheetDemo />
        </Section>

        <Section id="toasts" title="Toasts" note="Severity hairline; info/ok/warn hide after 5 s, errors stay; at most 3, older fold into +N.">
          <Pair>
            {() => (
              <div class="toast-col">
                <ToastView x={{ id: 1, severity: "info", title: t("laptop.shown") }} />
                <ToastView x={{ id: 2, severity: "ok", title: t("toast.allowed") }} />
                <ToastView x={{ id: 3, severity: "warn", title: t("e.busy") }} />
                <ToastView x={{ id: 4, severity: "error", title: t("edit.save_err", { msg: "E212" }), detail: "E212: Can't open file for writing" }} />
              </div>
            )}
          </Pair>
          <div class="kit-row">
            <Button onClick={() => toast("info", t("laptop.shown"))}>Push info</Button>
            <Button onClick={() => toast("ok", t("file.saved"))}>Push ok</Button>
            <Button onClick={() => toast("warn", t("e.busy"))}>Push warn</Button>
            <Button onClick={() => toast("error", t("err.internal"))}>Push error</Button>
          </div>
        </Section>

        <Section id="chips" title="Chips and badges">
          <Pair>
            {() => (
              <div class="kit-row wrap">
                <Chip on onClick={() => undefined}>
                  zsh · 1
                </Chip>
                <Chip dot="busy" onClick={() => undefined}>
                  go test · 2
                </Chip>
                <Chip dot="ok">make · 3</Chip>
                <Chip onClick={() => undefined} label="New terminal">
                  +
                </Chip>
                <Chip>▣ htop · 4</Chip>
                <Chip tone="amber">● {t("file.not_saved")}</Chip>
                <Chip tone="ai">📌 {t("chat.pinned")}</Chip>
                <Chip onClick={() => undefined}>↓ {t("term.follow")}</Chip>
                <Chip>{t("offline.input")}</Chip>
              </div>
            )}
          </Pair>
        </Section>

        <Section id="term" title="Terminal rows" note="The laptop's rows, soft-wrapped; ↳ marks a continuation of the same row. Always LTR.">
          <Pair>
            {() => (
              <div class="code-box term-box">
                <TermRow text="~/termocode main ❯ go test ./internal/app/ -run TestQuitConfirmKeepsCancelFirstAndFocused -count=1" />
                <TermRow cont text="-race" />
                <TermRow text="=== RUN   TestQuitConfirmKeepsCancelFirstAndFocused" />
                <TermRow text="--- PASS: TestQuitConfirmKeepsCancelFirstAndFocused (0.04s)" />
                <TermRow text="ok  	internal/app	2.31s" />
                <TermRow text="~/termocode main ❯ echo «تست‌ها سبز شدند»" />
                <TermRow text="تست‌ها سبز شدند" />
              </div>
            )}
          </Pair>
        </Section>

        <Section id="code" title="Code rows" note="Line numbers dim, wrap marker, the 400 ms flash when the laptop changes a row.">
          <Pair>
            {() => (
              <div class="code-box">
                <CodeRow n={86} text="func (m *Model) quit() tea.Cmd {" />
                <CodeRow n={87} text="	if m.dirty() {" flash />
                <CodeRow n={88} text={'\t\treturn m.confirmQuit(confirm.SafeFirst, confirm.WithTitle("Quit termocode?"))'} />
                <CodeRow n={89} text="	}" />
              </div>
            )}
          </Pair>
        </Section>

        <Section id="diff" title="Diff rows" note="Hunk headers stick; + and − rows in the git colours.">
          <Pair>{() => diffSample}</Pair>
        </Section>

        <Section id="tree" title="Tree rows" note="44 px rows; Open on laptop first; git badges; ● not saved; names dir=auto.">
          <Pair>
            {() => (
              <div class="tree-box">
                <div class="tree-group">{t("laptop.open")}</div>
                <TreeRow name="model.go" git="M" dirty active />
                <TreeRow name="quit_test.go" git="A" />
                <div class="tree-group">termocode</div>
                <TreeRow dir open name="internal" />
                <TreeRow depth={1} dir open name="app" />
                <TreeRow depth={2} name="model.go" git="M" />
                <TreeRow depth={2} name="merge_conflict.go" git="U" />
                <TreeRow depth={2} name="old_quit.go" git="D" />
                <TreeRow depth={1} dir name="statusbar" />
                <TreeRow depth={1} dir dim name="node_modules" />
                <TreeRow name="یادداشت‌ها.md" git="?" />
                <TreeRow name="a-very-long-file-name-that-does-not-fit-on-a-phone-screen.go" />
              </div>
            )}
          </Pair>
        </Section>

        <Section id="loading" title="Skeletons and progress" note="Skeleton at once; after 1 s a moving line; after 3 s Still loading.">
          <Pair>
            {() => (
              <div class="kit-split">
                <div>
                  <Progress k="kit" />
                  <SkeletonRows rows={8} />
                </div>
                <SkeletonBubbles />
              </div>
            )}
          </Pair>
        </Section>

        <Section id="empty" title="Empty states">
          <Pair>
            {() => (
              <>
                <div class="empty">
                  <div class="empty-state">
                    <span class="empty-mark" aria-hidden="true">✦</span>
                    <p>{t("chat.empty")}</p>
                  </div>
                </div>
                <div class="empty">
                  <p>{t("term.none")}</p>
                </div>
              </>
            )}
          </Pair>
        </Section>

        <Section id="waiting" title="Waiting for the laptop" note="Destructive actions ask the laptop; the phone waits calmly and can cancel.">
          <Pair>{() => <WaitingLaptop since={Date.now() - 42000} onCancel={() => toast("info", t("wait.cancelled"))} />}</Pair>
        </Section>

        <Section id="connect" title="Connect screen">
          <Pair tall>
            {() => (
              <div class="kit-split">
                <Connect state="ready" />
                <Connect state="expired" />
              </div>
            )}
          </Pair>
        </Section>

        <Section id="chips" title="Home chips" note="Needs you first while something waits; up to 3 pins next; the row scrolls sideways.">
          <KitChips />
        </Section>

        <Section id="blocks" title="Blocks" note="MOB-3: one block of each kind from the laptop; unknown kind = title + Open on laptop; tone = colour + shape; 360 px phone.">
          <Pair tall>
            {() => <Blocks list={blockSamples} now={KIT_NOW} onAction={(a) => toast("info", a.label)} onLaptop={(b) => toast("info", b.title)} onForm={() => toast("ok", t("blk.form_send"))} />}
          </Pair>
        <Section id="customize" title="Customize a view" note="⚙ on a view block opens the sheet: fields, filter, sort, group, rows, layout, density; the preview runs the draft. Cancel is first.">
          <KitCustomize />
        </Section>

        <Section id="misc" title="Choices">
          <Pair>
            {() => (
              <Segmented<string>
                label={t("theme")}
                value="dark"
                choices={[
                  { value: "system", label: t("more.theme.system") },
                  { value: "dark", label: t("more.theme.dark") },
                  { value: "light", label: t("more.theme.light") },
                ]}
                onChange={() => undefined}
              />
            )}
          </Pair>
        </Section>
        <Section id="phone" title="Phone app" note="Phone tokens are web tokens (both themes, AA); status is a colour and a shape; 44 px targets; the safe choice first. Phone layout only (MOBUX-1).">
          <Pair tall>{() => <PhoneKit />}</Pair>
          <Pair>{() => <PhoneSheetDemo />}</Pair>
        </Section>
        <Section id="shell" title="Phone app shell" note="Top bar: project switcher ▾, live mark, ☰. The stream, then the dock in the thumb zone: chips above the composer (+, 🎤, Send). MOBUX-2.">
          <ShellControls />
          <Pair tall>{() => <ShellDemo />}</Pair>
        </Section>
      </main>
      <Toasts />
    </div>
  );
}

const root = document.getElementById("kit");
if (root) {
  render(<Kit />, root);
}
