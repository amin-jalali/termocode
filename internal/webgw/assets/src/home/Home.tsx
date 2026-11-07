// The phone's Home (MOB-4, mobile plan 2.1): one stream, a chip row and
// one box. The top bar names the workspace, shows the link (● live, ○
// offline) and opens the Panels drawer (≡: Views, Files, Terminal, Git,
// More, This phone), where the old tabs live. The stream is the laptop's
// chat with the chips' quick blocks between its messages (state/home.ts).
// Chips never spend tokens; nothing goes to the AI until Send.
import { h, H } from "./jsx";
import { hs, type HomeKey } from "./strings";
import { CHIPS, chipOrder, viewChips, type Built, type ChipDef } from "./chips";
import { QuickBlock } from "./QuickBlock";
import { Panels } from "./Panels";
import { HomeStart, type StartCard } from "./Start";
import { PinnedViews, startSaved } from "./saved";
import { addQuick, boardBlock, dropQuick, gitBlock, homeSummary, lastIndex, mergeStream, needsYouBlock, patchQuick, projectsBlock, readBlock, readInbox, readProjects, runsBlock, tasksBlock, type HomeBlock, type Labels, type Quick } from "../state/home";
import { readGit } from "../state/git";
import { readRuns, readTasks } from "../state/tasks";
import type { Signal } from "@preact/signals";

/** PAGE is how many chat messages the Home draws (older ones: the Chat tab). */
const PAGE = 60;
/** NEAR_BOTTOM_PX: closer than this to the end, new text keeps the view at the end. */
const NEAR_BOTTOM_PX = 80;

function labels(): Labels {
  return {
    needsYou: hs("needs.title"),
    nothingNeedsYou: hs("needs.none"),
    board: hs("board.title"),
    boardEmpty: hs("board.empty"),
    states: { needs_you: hs("state.needs_you"), working: hs("state.working"), failed: hs("state.failed"), done: hs("state.done") },
    tasks: hs("taskv.title"),
    noTasks: hs("taskv.none"),
    git: hs("gitv.title"),
    clean: hs("gitv.clean"),
    noRepo: hs("gitv.norepo"),
    runs: hs("runv.title"),
    noRuns: hs("runv.none"),
    projects: hs("projv.title"),
    noProjects: hs("projv.none"),
  };
}

function data(topic: string): unknown {
  return H.store.topic(topic).value?.data;
}

/** buildFor builds a chip's page block from the topics now (live). */
export function buildFor(b: Built): HomeBlock {
  const L = labels();
  switch (b) {
    case "needs":
      return needsYouBlock(readInbox(data("inbox")), H.cards.value, L);
    case "board":
      return boardBlock(readInbox(data("inbox")) ?? [], L);
    case "projects":
      return projectsBlock(readProjects(data("projects")), L);
    case "tasks":
      return tasksBlock(readTasks(data("tasks")).tasks, L);
    case "git":
      return gitBlock(readGit(data("git")), L);
    case "runs":
      return runsBlock(readRuns(data("runs")), L);
  }
}

/** The Home's state lives for the page's life: leaving the Home for a
 * tab and coming back keeps the stream. */
let state: { quick: Signal<Quick[]>; panels: Signal<boolean> } | null = null;
function homeState() {
  if (!state) state = { quick: H.signal<Quick[]>([]), panels: H.signal(false) };
  return state;
}
let seq = 0;

/** openItem opens what a row names ("<kind>:<ref>"): a card's sheet, or
 * the view of its source. */
export function openItem(id: string): void {
  const i = id.indexOf(":");
  const kind = i < 0 ? id : id.slice(0, i);
  const ref = i < 0 ? "" : id.slice(i + 1);
  const more = (rest: string) => H.go({ tab: "more", rest, query: {} });
  switch (kind) {
    case "card":
      if (ref) H.go({ ...H.route.peek(), card: ref });
      return;
    case "run":
      return more("runs");
    case "task":
      return more("tasks");
    case "workflow":
      return more("workflows");
    case "lane":
    case "race":
      return more("agents");
    case "file":
      if (ref) H.go({ tab: "files", rest: ref, query: {} });
      return;
    case "ci":
      return H.goTab("git");
    // "project": the project page comes with MOBUX-7; the row stays put.
  }
}

function focusBox(): void {
  const el = document.querySelector<HTMLTextAreaElement>(".home .composer-box");
  if (!el) return;
  el.focus();
  el.setSelectionRange(el.value.length, el.value.length);
}

function TopBar({ panels }: { panels: Signal<boolean> }) {
  const live = H.live.value;
  const ws = H.status.value.workspace;
  return (
    <header class="home-top">
      <h1 class="home-name trunc">
        {hs("title")}
        {ws && <span class="home-ws"> · {ws}</span>}
      </h1>
      <span class={`home-live${live ? " is-live" : ""}`}>
        <span aria-hidden="true">{live ? "●" : "○"}</span> {hs(live ? "live" : "offline")}
      </span>
      <button type="button" class="home-menu" aria-label={hs("panels.open")} title={hs("panels.open")} aria-expanded={panels.value ? "true" : "false"} onClick={() => (panels.value = true)}>
        <span aria-hidden="true">≡</span>
      </button>
    </header>
  );
}

/** waitingCount is what needs the user: the inbox's count, or the
 * waiting cards while the laptop has no inbox topic. */
function waitingCount(): number {
  const inbox = readInbox(data("inbox"));
  return inbox ? inbox.filter((x) => x.state === "needs_you").length : H.cards.value.length;
}

function Chips({ onChip }: { onChip: (c: ChipDef) => void }) {
  const n = waitingCount();
  return (
    <nav class="home-chips" aria-label={hs("chips")}>
      {chipOrder(CHIPS, n).map((c) => (
        <button key={c.id} type="button" class={`chip home-chip${c.id === "needs" && n > 0 ? " is-needs" : ""}`} onClick={() => onChip(c)}>
          {c.id === "needs" && n > 0 ? hs("chip.needs_n", { n }) : hs(c.label)}
        </button>
      ))}
    </nav>
  );
}

export function Home() {
  const st = homeState();
  const { quick, panels } = st;
  H.useEffect(() => H.link.subscribe("chat", "cards", "inbox"), []);
  H.useEffect(() => startSaved(), []);
  const body = H.useRef<HTMLDivElement | null>(null);
  const stick = H.useRef(true);
  const laptop = H.readChat(data("chat"));
  const chatId = laptop?.id ?? "";
  const msgs = laptop?.messages ?? [];
  const shown = msgs.slice(Math.max(0, msgs.length - PAGE));
  const stream = mergeStream(chatId, shown, quick.value);

  H.useEffect(() => {
    const el = body.current;
    if (!el) return;
    const onScroll = () => (stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < NEAR_BOTTOM_PX);
    el.addEventListener("scroll", onScroll, { passive: true });
    return () => el.removeEventListener("scroll", onScroll);
  }, []);
  H.useLayoutEffect(() => {
    const el = body.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  });

  const fill = (key: HomeKey) => {
    H.setDraft(chatId, hs(key));
    requestAnimationFrame(focusBox);
  };

  const run = (c: ChipDef, retry?: Quick) => {
    if (c.topics) H.link.subscribe(...c.topics);
    const id = retry?.id ?? "q" + ++seq;
    if (retry) quick.value = patchQuick(quick.peek(), id, { state: "loading", error: undefined });
    else quick.value = addQuick(quick.peek(), { id, chip: c.id, chat: chatId, after: lastIndex(msgs), at: Date.now(), state: "loading", from: "" });
    stick.current = true;
    const fallback = () => {
      if (c.build) quick.value = patchQuick(quick.peek(), id, { state: "ready", from: "page" });
      else {
        quick.value = dropQuick(quick.peek(), id);
        if (c.fill) fill(c.fill);
      }
    };
    if (!c.view) return fallback();
    H.link.request("view.run", { view: c.view }).then(
      (d) => {
        const b = readBlock(d);
        if (b) quick.value = patchQuick(quick.peek(), id, { state: "ready", from: "view", block: b });
        else fallback();
      },
      (e: unknown) => {
        // The laptop has no such view yet (bad_request, not_found): the
        // page builds the block itself or fills the box.
        const code = H.code(e);
        if (!H.isOffline(e) && (code === "bad_request" || code === "not_found")) return fallback();
        if (H.isOffline(e) && c.build) return fallback(); // the topic's last copy, marked stale by the bars
        quick.value = patchQuick(quick.peek(), id, { state: "error", error: H.errText(e) });
      },
    );
  };

  const onChip = (c: ChipDef) => {
    if (c.panels) {
      panels.value = true;
      return;
    }
    if (!c.view && c.fill) return fill(c.fill);
    run(c);
  };

  const chipOf = (id: string) => CHIPS.find((c) => c.id === id);
  // A Home card opens the list behind it through its chip (no tokens):
  // needs you → the needs-you list, working and done today → the board.
  const onCard = (card: StartCard) => {
    const c = chipOf(card === "needs" ? "needs" : "board");
    if (c) onChip(c);
  };
  const busy = laptop?.busy ?? false;
  return (
    <section class="home" data-view="home" aria-label={hs("title")}>
      <TopBar panels={panels} />
      <div class="home-stream" ref={body} role="log" aria-live="polite">
        {stream.length === 0 && <HomeStart sum={homeSummary(readInbox(data("inbox")), H.cards.value, Date.now())} onCard={onCard} />}
        {stream.length > 0 && <PinnedViews />}
        {stream.map((it) => {
          if (it.kind === "msg") return <H.Message key={"m" + it.m.i} m={it.m} chat={chatId} canFetch />;
          const q = it.q;
          const c = chipOf(q.chip);
          return (
            <QuickBlock
              key={q.id}
              q={q}
              title={c ? hs(c.label) : ""}
              built={q.from === "page" && c?.build ? buildFor(c.build) : null}
              onOpen={openItem}
              onClose={() => (quick.value = dropQuick(quick.peek(), q.id))}
              onRetry={() => c && run(c, q)}
            />
          );
        })}
      </div>
      <footer class="home-foot">
        <Chips onChip={onChip} />
        {laptop ? (
          <H.Composer chat={chatId} busy={busy} profile={laptop.profile ?? ""} model={laptop.model ?? ""} placeholder={hs("ask")} />
        ) : (
          <p class="home-status">{hs("loading")}</p>
        )}
      </footer>
      {panels.value && <Panels views={viewChips(CHIPS)} onView={(c) => run(c)} onClose={() => (panels.value = false)} />}
    </section>
  );
}

