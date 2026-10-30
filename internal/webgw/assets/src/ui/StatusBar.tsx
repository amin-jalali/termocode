// The desktop status bar (workbench): the TUI's bottom bar. The same
// facts as the phone's strip (pill, branch, changes, AI, web, link), then
// the open file's facts as the laptop's editor has them (line, indent,
// encoding, line ends, language) and the layout buttons.
import { Fragment, h } from "preact";
import type { BufSnap } from "../../../../webproto/proto";
import { selection } from "../lib/edit";
import { file } from "../lib/file";
import { route } from "../lib/router";
import { fileOfTab } from "../lib/difftab";
import { activeFile, cursorAt } from "../lib/workbench";
import { diags, toggleProblems } from "../lib/lsp";
import { link, status, store } from "../session";
import { t } from "../strings";
import { Cod } from "./icons";
import { readGit } from "../state/git";
import { syncMenu } from "../views/desk/DeskSCM";
import { Comet, rttLevel } from "./Strip";

// Filetypes as people name them; others show as nvim names them.
const LANGS: ReadonlyMap<string, string> = new Map([
  ["go", "Go"], ["gomod", "Go Module"], ["typescript", "TypeScript"], ["typescriptreact", "TypeScript JSX"],
  ["javascript", "JavaScript"], ["javascriptreact", "JavaScript JSX"], ["python", "Python"], ["rust", "Rust"],
  ["json", "JSON"], ["jsonc", "JSON with Comments"], ["yaml", "YAML"], ["toml", "TOML"], ["markdown", "Markdown"],
  ["sh", "Shell"], ["bash", "Bash"], ["zsh", "Zsh"], ["lua", "Lua"], ["c", "C"], ["cpp", "C++"], ["java", "Java"],
  ["html", "HTML"], ["css", "CSS"], ["sql", "SQL"], ["make", "Makefile"], ["dockerfile", "Dockerfile"],
  ["text", "Plain Text"], ["", "Plain Text"],
]);

/** langName is a filetype as people name it. */
export function langName(ft: string): string {
  return LANGS.get(ft) ?? ft;
}

const EOLS: Readonly<Record<string, string>> = { unix: "LF", dos: "CRLF", mac: "CR" };

function FileFacts() {
  const f = file.value;
  if (route.value.tab === "more" || !f || f.path !== fileOfTab(activeFile.value ?? "") || f.kind !== "open" || f.buf === undefined) return null;
  const d = store.topic("buf:" + f.buf).value?.data as BufSnap | undefined;
  if (!d || !Array.isArray(d.lines)) return null;
  const sel = selection.value;
  const at = cursorAt.value;
  const line = at ? at.line : sel ? sel.a + 1 : 1;
  const col = at ? at.col : 1;
  const ts = d.ts ?? 0;
  return (
    <>
      <span class="strip-item sb-pos" title={t("sb.line_of", { n: line, m: d.lines.length })}>
        {t("sb.pos", { n: line, c: col })}
      </span>
      {ts > 0 && <span class="strip-item sb-indent">{t(d.et ? "sb.spaces" : "sb.tabs", { n: ts })}</span>}
      <span class="strip-item sb-enc">{(d.enc || "utf-8").toUpperCase()}</span>
      {d.eol && <span class="strip-item sb-eol">{EOLS[d.eol] ?? d.eol}</span>}
      <span class="strip-item sb-lang">{langName(d.ft)}</span>
      {d.readonly && <span class="strip-item">{t("viewonly")}</span>}
    </>
  );
}

/** ProblemCounts are the workspace's problems (to fix, warnings); a
 * click shows the PROBLEMS panel. */
function ProblemCounts() {
  const d = diags();
  const e = d?.errors ?? 0;
  const w = d?.warnings ?? 0;
  const label = t("sb.problems", { e, w });
  return (
    <button type="button" class="strip-item sb-problems" title={label} aria-label={label} onClick={toggleProblems}>
      <Cod name="error" />
      <span class="sb-n">{e}</span>
      <Cod name="warning" />
      <span class="sb-n">{w}</span>
    </button>
  );
}

/** SyncItem is the branch's sync state (↓ behind, ↑ ahead); a click
 * offers pull and push, which ask the laptop. */
function SyncItem() {
  const g = readGit(store.topic("git").value?.data);
  if (!g.repo || !g.branch.name) return null;
  const { ahead, behind } = g.branch;
  return (
    <button
      type="button"
      class="strip-item sb-sync"
      title={t("sb.sync", { ahead, behind })}
      onClick={(e) => {
        const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
        void syncMenu(r.left, r.top - 4);
      }}
    >
      <Cod name="sync" />
      {(ahead > 0 || behind > 0) && (
        <span>
          {behind}↓ {ahead}↑
        </span>
      )}
    </button>
  );
}

export function StatusBar() {
  const s = status.value;
  const online = link.state.value === "online";
  const rtt = link.rtt.value;
  const level = online ? rttLevel(rtt) : "none";
  const ai = s.cardsWaiting > 0 ? "waiting" : s.aiBusy ? "working" : "idle";
  const tint = s.aiBusy ? "ai" : s.dirty > 0 ? "dirty" : "calm";
  return (
    <footer class="strip statusbar" dir="ltr">
      <span class={`brand sb-remote tint-${tint}`} title={s.workspace}>
        <Cod name="remote" />
        <span>termocode</span>
      </span>
      {s.branch && (
        <button
          type="button"
          class="strip-item branch"
          title={t("sb.branch", { branch: s.branch })}
          onClick={(e) => {
            const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
            void syncMenu(r.left, r.top - 4);
          }}
        >
          <Cod name="git-branch" />
          <span class="trunc">{s.branch}</span>
        </button>
      )}
      <SyncItem />
      {s.dirty > 0 && <span class="strip-item dirty" title={t("sb.changed", { n: s.dirty })}>● {s.dirty}</span>}
      <ProblemCounts />
      <span class={`strip-item ai ai-${ai}`}>
        <Cod name="sparkle" />
        <span>{t(ai === "waiting" ? "strip.ai_waiting" : ai === "working" ? "strip.ai_working" : "strip.ai_idle")}</span>
        {ai === "working" && <Comet />}
      </span>
      <span class="strip-gap" />
      <FileFacts />
      {s.webClients > 0 && (
        <span class="strip-item web" title={t("strip.web", { n: s.webClients })}>
          ◉ <span class="web-label">{t("strip.web", { n: s.webClients })}</span>
        </span>
      )}
      {level === "slow" || level === "bad" ? <span class="strip-item rtt-text">{t("rtt.slow", { ms: rtt ?? 0 })}</span> : null}
      <span
        class={`rtt rtt-${level}`}
        role="img"
        aria-label={rtt !== null && online ? t("rtt.label", { ms: rtt }) : t("offline.input")}
        title={rtt !== null && online ? t("rtt.label", { ms: rtt }) : t("offline.input")}
      />
    </footer>
  );
}
