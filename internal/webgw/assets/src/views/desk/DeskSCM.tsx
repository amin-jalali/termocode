// The desktop's Source Control view, as the laptop's editor has it: the
// commit message box (Ctrl/Cmd+Enter commits) over the Commit button and
// its menu; Merge Changes, Staged Changes and Changes with their counts
// and their actions on hover (stage all, unstage all, discard all); one
// row per file with its icon, its folder dim and its status letter in
// its colour, and on hover Open File, Discard Changes and Stage or
// Unstage. A click opens the change in a diff editor tab. Commit asks in
// the view first (Cancel first and focused); discarding, pull, push,
// stash and switching branch ask the laptop. The branch and the sync
// state are in the status bar.
import { Fragment, h, type ComponentChildren } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useRef, useState } from "preact/hooks";
import { diffTab } from "../../lib/difftab";
import { modKey } from "../../lib/platform";
import { route } from "../../lib/router";
import { keep, showFile } from "../../lib/workbench";
import { toast } from "../../lib/toasts";
import { link, live, store } from "../../session";
import { age, commitMessage, groupFiles, readGit, splitPath, type GitFile } from "../../state/git";
import { t, tn } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { openMenu, type MenuItems } from "../../ui/ContextMenu";
import { Cod, FileIcon } from "../../ui/icons";
import { Act, Ask, PaneSection, PaneView, Row, Tree } from "../../ui/pane";
import { DiscardFlow } from "../git/DiscardFlow";
import { gitFailed } from "../git/errors";
import { ShowSheet } from "../git/ShowSheet";

type Group = "merge" | "staged" | "changes";

/** RemoteAsk is a git command that waits for the laptop's card. */
export interface RemoteAsk {
  op: string;
  args: Record<string, string>;
  text: string;
}

/** remoteAsk is the open laptop question of the view or the status bar. */
export const remoteAsk = signal<RemoteAsk | null>(null);

/** syncMenu lists pull, push, stash, pop and the branches (the view's …
 * and the status bar's sync and branch items). */
export async function syncMenu(x: number, y: number): Promise<void> {
  const g = readGit(store.topic("git").peek()?.data);
  const branch = g.branch.name;
  const items: (MenuItems[number])[] = [
    { label: t("git.pull"), run: () => (remoteAsk.value = { op: "git.pull", args: {}, text: t("git.pull_q", { branch }) }) },
    { label: t("git.push"), run: () => (remoteAsk.value = { op: "git.push", args: {}, text: t("git.push_q", { branch }) }) },
    null,
    { label: t("git.stash"), disabled: g.files.length === 0, run: () => (remoteAsk.value = { op: "git.stash", args: {}, text: t("git.stash_q") }) },
    { label: t("git.pop"), run: () => (remoteAsk.value = { op: "git.stash_pop", args: {}, text: t("git.pop_q") }) },
  ];
  let others: string[] = [];
  try {
    const r = (await link.request("git.branches", {})) as { branches?: unknown };
    others = (Array.isArray(r?.branches) ? r.branches : []).filter((b): b is string => typeof b === "string" && b !== branch);
  } catch {
    others = [];
  }
  if (others.length) {
    items.push(null);
    for (const b of others.slice(0, 12)) items.push({ label: t("scm.checkout", { branch: b }), run: () => (remoteAsk.value = { op: "git.checkout", args: { branch: b }, text: t("git.checkout_q", { branch: b }) }) });
  }
  openMenu(x, y, t("git.remote"), items);
}

/** RemoteAskHost shows the laptop question, wherever it was asked. */
export function RemoteAskHost() {
  const a = remoteAsk.value;
  if (!a) return null;
  return <AskLaptop label={t("git.remote")} text={a.text} send={() => link.request(a.op, a.args)} onDone={() => (remoteAsk.value = null)} />;
}

/** stage stages or unstages paths and offers Undo for one file. */
function stage(paths: string[], on: boolean, undo = true): void {
  if (paths.length === 0) return;
  link.request(on ? "git.stage" : "git.unstage", { paths }).then(() => {
    if (!undo || paths.length !== 1) return;
    toast("ok", t(on ? "git.staged_toast" : "git.unstaged_toast", { name: splitPath(paths[0]).name }), undefined, { label: t("undo"), run: () => stage(paths, !on, false) });
  }, gitFailed);
}

/** letter is the status letter a row shows and its colour. */
function letter(f: GitFile, g: Group): { l: string; c: string } {
  if (g === "merge") return { l: "!", c: "c-conf" };
  if (f.untracked) return { l: "U", c: "c-add" };
  const code = g === "staged" ? f.x : f.y;
  if (code === "A") return { l: "A", c: "c-add" };
  if (code === "D") return { l: "D", c: "c-del" };
  if (code === "R") return { l: "R", c: "c-add" };
  return { l: "M", c: "c-mod" };
}

/** open shows a change: a diff tab (a new or conflicted file as itself). */
function open(f: GitFile, g: Group, pin = false): void {
  const tab = g === "merge" || f.untracked ? f.path : diffTab(g === "staged" ? "idx" : "wt", f.path);
  showFile(tab, !pin);
  if (pin) keep(tab);
}

const shut = signal<ReadonlySet<string>>(new Set(["log"]));
function toggle(id: string): void {
  const n = new Set(shut.peek());
  if (n.has(id)) n.delete(id);
  else n.add(id);
  shut.value = n;
}

function FileRow({ f, g, canAct, onDiscard }: { f: GitFile; g: Group; canAct: boolean; onDiscard: (p: string[]) => void }) {
  const { name, dir } = splitPath(f.path);
  const st = letter(f, g);
  const del = st.l === "D";
  return (
    <Row
      level={1}
      icon={<FileIcon path={f.path} />}
      label={name}
      labelClass={`${st.c}${del ? " is-del" : ""}`}
      desc={dir}
      title={`${f.path} • ${t(g === "staged" ? "scm.staged" : g === "merge" ? "scm.merge" : "git.changes")}`}
      data={{ path: f.path, group: g }}
      onClick={() => open(f, g)}
      onDblClick={() => open(f, g, true)}
      end={<span class={`tr-letter ${st.c}`}>{st.l}</span>}
      actions={
        <>
          <Act icon="go-to-file" label={t("dv.open_file")} onClick={() => showFile(f.path)} />
          {canAct && g === "changes" && <Act icon="discard" label={t("scm.discard")} onClick={() => onDiscard([f.path])} />}
          {canAct && g === "changes" && <Act icon="add" label={t("scm.stage")} onClick={() => stage([f.path], true)} />}
          {canAct && g === "staged" && <Act icon="remove" label={t("scm.unstage")} onClick={() => stage([f.path], false)} />}
        </>
      }
    />
  );
}

export function DeskSCM() {
  useEffect(() => link.subscribe("git"), []);
  const st = store.topic("git").value;
  const g = readGit(st?.data);
  const viewer = link.meta.value?.role === "viewer";
  const canAct = !viewer && live.value;
  const [msg, setMsg] = useState("");
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [discard, setDiscard] = useState<string[] | null>(null);
  const [show, setShow] = useState("");
  const box = useRef<HTMLTextAreaElement | null>(null);
  const groups = groupFiles(g.files);
  const changes = [...groups.changes, ...groups.untracked].sort((a, b) => a.path.localeCompare(b.path));
  const staged = groups.staged;
  const branch = g.branch.name;
  useEffect(() => {
    if (staged.length === 0) setAsking(false);
  }, [staged.length]);
  // A link to a change (#/git/<path>, ?side=staged): its diff editor tab.
  const r = route.value;
  const linked = r.tab === "git" ? r.rest : "";
  const linkedStaged = r.tab === "git" && r.query.side === "staged";
  useEffect(() => {
    if (linked) showFile(diffTab(linkedStaged ? "idx" : "wt", linked));
  }, [linked, linkedStaged]);
  // The box grows with the message, up to 10 lines.
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 10 * 18 + 6) + "px";
  }, [msg]);

  const ready = msg.trim() !== "" && staged.length > 0 && !busy && canAct;
  const commit = () => {
    const lines = msg.split("\n");
    setBusy(true);
    link
      .request("git.commit", { message: commitMessage(lines[0] ?? "", lines.slice(1).join("\n").trim()) })
      .then((r) => {
        const d = (r ?? {}) as { hash?: unknown; files?: unknown };
        toast("ok", t("git.committed", { hash: String(d.hash ?? ""), n: Number(d.files ?? staged.length) }));
        setMsg("");
      }, gitFailed)
      .finally(() => {
        setBusy(false);
        setAsking(false);
      });
  };
  const askCommit = () => {
    if (!ready) {
      if (staged.length === 0 && msg.trim()) toast("info", t("scm.nothing_staged"));
      return;
    }
    setAsking(true);
  };

  let body;
  if (!st) body = <p class="pv-msg">{t("git.reading")}</p>;
  else if (g.loading) body = <p class="pv-msg">{t("git.reading")}</p>;
  else if (!g.repo) body = <p class="pv-msg">{t("git.not_repo")}</p>;
  else
    body = (
      <>
        {!viewer && (
          <div class="scm-input">
            <div class="pv-input">
              <textarea
                ref={box}
                rows={1}
                dir="auto"
                spellcheck={false}
                value={msg}
                maxLength={10000}
                disabled={!live.value || !!g.op}
                aria-label={t("git.subject")}
                placeholder={t("scm.placeholder", { keys: `${modKey()}+Enter`, branch })}
                onInput={(e) => setMsg((e.currentTarget as HTMLTextAreaElement).value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
                    e.preventDefault();
                    askCommit();
                  }
                }}
              />
            </div>
            {asking ? (
              <Ask text={t("git.commit_confirm", { n: staged.length, branch })} verb={t("git.commit_do")} onYes={commit} onNo={() => setAsking(false)} />
            ) : (
              <div class="pv-split scm-commit">
                <button type="button" class="pv-btn" data-act="commit" aria-disabled={ready ? undefined : "true"} title={tn("git.commit", staged.length)} onClick={askCommit}>
                  <Cod name="check" /> {t("git.commit_do")}
                </button>
                <button
                  type="button"
                  class="pv-btn"
                  aria-label={t("scm.more_actions")}
                  title={t("scm.more_actions")}
                  aria-disabled={canAct ? undefined : "true"}
                  onClick={(e) => {
                    if (!canAct) return;
                    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
                    void syncMenu(r.left, r.bottom);
                  }}
                >
                  <Cod name="chevron-down" />
                </button>
              </div>
            )}
          </div>
        )}
        {g.op && <p class="pv-msg c-warn">{t("git.op", { op: g.op })}</p>}
        <div class="pv-scroll">
          <Tree label={t("act.scm")}>
            {groups.conflicts.length > 0 && (
              <GroupRow id="merge" title={t("scm.merge")} n={groups.conflicts.length} />
            )}
            {!shut.value.has("merge") && groups.conflicts.map((f) => <FileRow key={"m" + f.path} f={f} g="merge" canAct={false} onDiscard={setDiscard} />)}
            {staged.length > 0 && (
              <GroupRow
                id="staged"
                title={t("scm.staged")}
                n={staged.length}
                actions={canAct && <Act icon="remove" label={t("scm.unstage_all")} onClick={() => stage(staged.map((f) => f.path), false)} />}
              />
            )}
            {staged.length > 0 && !shut.value.has("staged") && staged.map((f) => <FileRow key={"s" + f.path} f={f} g="staged" canAct={canAct} onDiscard={setDiscard} />)}
            {(changes.length > 0 || staged.length === 0) && (
              <GroupRow
                id="changes"
                title={t("git.changes")}
                n={changes.length}
                actions={
                  canAct &&
                  changes.length > 0 && (
                    <>
                      <Act icon="discard" label={t("scm.discard_all")} onClick={() => setDiscard(changes.map((f) => f.path))} />
                      <Act icon="add" label={t("scm.stage_all")} onClick={() => stage(changes.map((f) => f.path), true)} />
                    </>
                  )
                }
              />
            )}
            {!shut.value.has("changes") && changes.map((f) => <FileRow key={"c" + f.path} f={f} g="changes" canAct={canAct} onDiscard={setDiscard} />)}
          </Tree>
          {g.more > 0 && <p class="pv-msg">{t("git.many", { n: g.more })}</p>}
        </div>
        {g.log.length > 0 && (
          <PaneSection id="log" title={t("scm.graph")} open={!shut.value.has("log")} onToggle={() => toggle("log")} fixed>
            <Tree label={t("scm.graph")}>
              {g.log.slice(0, 20).map((c) => (
                <Row
                  key={c.hash}
                  level={0}
                  icon={<Cod name="git-commit" />}
                  label={c.subject}
                  desc={`${c.author} · ${age(c.when, Date.now())}`}
                  title={`${c.hash} ${c.subject}`}
                  data={{ hash: c.hash }}
                  end={c.unpushed ? <Cod name="arrow-up" /> : undefined}
                  onClick={() => setShow(c.hash)}
                />
              ))}
            </Tree>
          </PaneSection>
        )}
      </>
    );

  return (
    <PaneView
      id="git"
      title={t("act.scm")}
      actions={
        g.repo &&
        !viewer && (
          <>
            <Act icon="check" label={t("git.commit_do")} keys={`${modKey()}+Enter`} disabled={!ready} onClick={askCommit} />
            <Act
              icon="ellipsis"
              label={t("scm.more_actions")}
              disabled={!canAct}
              onClick={(e) => {
                const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
                void syncMenu(r.left, r.bottom);
              }}
            />
          </>
        )
      }
    >
      {body}
      {discard && <DiscardFlow paths={discard} onDone={() => setDiscard(null)} />}
      {show && <ShowSheet hash={show} onClose={() => setShow("")} />}
    </PaneView>
  );
}

/** GroupRow is a group's head inside the tree (as the laptop's editor
 * draws them): twistie, name, actions on hover, count. */
function GroupRow({ id, title, n, actions }: { id: string; title: string; n: number; actions?: ComponentChildren }) {
  const open = !shut.value.has(id);
  return (
    <Row
      level={0}
      twist={open ? "open" : "closed"}
      onToggle={() => toggle(id)}
      label={<span class="scm-group">{title}</span>}
      data={{ group: id }}
      actions={actions || undefined}
      end={<span class="count">{n}</span>}
    />
  );
}
