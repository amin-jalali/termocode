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
import "./kit.css";
import { toast } from "../lib/toasts";
import type { StatusView } from "../state/status";
import { t, type StringKey } from "../strings";
import { AttentionView } from "../ui/AttentionPill";
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
import "./replay";

installSafety();

type Width = 390 | 768 | 1280;
type Themes = "both" | "dark" | "light";
const width = signal<Width>(390);
const themes = signal<Themes>("both");

const baseStatus: StatusView = {
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
        {["tokens", "strip", "nav", "pill", "bars", "buttons", "cards", "sheets", "toasts", "chips", "term", "code", "diff", "tree", "loading", "empty", "waiting", "connect", "misc"].map((s) => (
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
                <Bar kind="final" text={t("e.idle")} sub={t("e.still_running")} />
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
      </main>
      <Toasts />
    </div>
  );
}

const root = document.getElementById("kit");
if (root) {
  render(<Kit />, root);
}
