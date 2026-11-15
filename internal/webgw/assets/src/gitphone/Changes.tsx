// The phone's Git page (sheet 05): the head with the branch pill, History
// and ⇅; filter chips with counts; the changed files as grouped cards, a
// round tick on each row stages or unstages it; the commit field and a
// split Commit button docked at the bottom. The groups are a view spec
// (git.status): a saved view the AI or the user made reshapes them, with
// Edit and Reset on its banner; ⚙ on a group edits it. A long press on a
// row opens its menu: Ask AI about this, Add to chat, Open diff, Stage,
// Discard….
import type { GitFile, GitState, Side } from "../state/git";
import { splitPath } from "../state/git";
import type { ViewSpec } from "../home/viewspec";
import { Fragment, H, h } from "./jsx";
import { s } from "./strings";
import { Btn, chatItems, hold, Icon, Menu, type At, type MenuItem } from "./ui";
import { describe, pickView, sections, SIDES, STATUS_VIEW, type PageView } from "./view";

const TITLES: Record<Side, "git.conflicts" | "git.staged" | "git.changes" | "git.untracked"> = {
  conflicts: "git.conflicts",
  staged: "git.staged",
  changes: "git.changes",
  untracked: "git.untracked",
};

/** resetKey is the saved view Reset put aside (its id and save time), for
 * as long as the page is open: a newer save shows again. */
let resetKey = "";

/** usePageView is the page's saved view (view.list, ev views_saved), and
 * a Reset of it on this phone (until a newer save). */
function usePageView(ready: boolean): [PageView | null, () => void] {
  const [v, setV] = H.useState<PageView | null>(null);
  const [reset, setReset] = H.useState(resetKey);
  // Asked once the git topic is here (after its sub), and heard again
  // on every save anywhere.
  H.useEffect(() => {
    if (!ready) return;
    const read = (d: unknown) => setV(pickView(d, STATUS_VIEW));
    H.link.request("view.list").then(read, () => undefined);
    return H.link.onEvent((kind, d) => kind === "views_saved" && read(d));
  }, [ready]);
  const key = v ? `${v.id}@${v.saved}` : "";
  const drop = () => setReset((resetKey = key));
  return [v && reset !== key ? v : null, drop];
}

export function stageAll(paths: string[], on: boolean): void {
  if (paths.length) H.link.request(on ? "git.stage" : "git.unstage", { paths }).then(undefined, H.failed);
}

export function Changes({
  g,
  stale,
  canAct,
  msg,
  setMsg,
  onCommit,
  onAmend,
  onDiscard,
}: {
  g: GitState;
  stale: boolean;
  canAct: boolean;
  msg: string;
  setMsg: (m: string) => void;
  onCommit: (all: boolean) => void;
  onAmend: () => void;
  onDiscard: (path: string) => void;
}) {
  const [chip, setChip] = H.useState<Side | "all">("all");
  const [menu, setMenu] = H.useState<{ at: At; title: string; items: MenuItem[] } | null>(null);
  const [tune, setTune] = H.useState(false);
  const [view, resetView] = usePageView(!g.loading);
  const [own, setOwn] = H.useState<ViewSpec | null>(null);
  const spec = own ?? view?.spec ?? null;
  const titles = { conflicts: H.t(TITLES.conflicts), staged: H.t(TITLES.staged), changes: H.t(TITLES.changes), untracked: H.t(TITLES.untracked) };
  const { list, hidden } = sections(g.files, spec, titles, s("folder_root"));
  const counts: Record<Side, number> = { conflicts: 0, staged: 0, changes: 0, untracked: 0 };
  for (const sec of sections(g.files, spec, titles, "").list) if (sec.side) counts[sec.side] = sec.rows.length;
  const total = list.reduce((n, x) => n + new Set(x.rows.map((r) => r.f.path)).size, 0);
  const chips = SIDES.filter((x) => counts[x] > 0);
  const on = chip !== "all" && counts[chip] === 0 ? "all" : chip;
  const staged = g.files.filter((f) => f.staged).length;
  const viewer = H.link.meta.value?.role === "viewer";

  const rowMenu = (f: GitFile, side: Side, at: At) => {
    const stage = side !== "staged";
    setMenu({
      at,
      title: f.path,
      items: [
        ...chatItems({ kind: "diff", path: f.path }, splitPath(f.path).name),
        { label: s("open_diff"), icon: "plusminus", run: () => open(f, side) },
        { label: H.t(stage ? "git.stage" : "git.unstage"), icon: "check", run: () => H.stage(f.path, stage), hidden: !canAct || side === "conflicts" },
        { label: s("discard"), icon: "trash", kind: "danger", run: () => onDiscard(f.path), hidden: !canAct || side !== "changes" },
      ],
    });
  };
  const open = (f: GitFile, side: Side) => H.go({ tab: "git", rest: f.path, query: side === "staged" ? { side: "staged" } : {} });

  const tuneSpec: ViewSpec = spec ?? { view: STATUS_VIEW };
  let body;
  if (g.loading) body = <div class="empty">{H.t("git.reading")}</div>;
  else if (!g.repo) body = <div class="empty">{H.t("git.not_repo")}</div>;
  else
    body = (
      <>
        {g.op && (
          <div class="git-op" role="status">
            {H.t("git.op", { op: g.op })}
          </div>
        )}
        {view && !own && (
          <div class={`gp-aiv${view.by === "ai" ? " is-ai" : ""}`} role="note">
            {view.by === "ai" && <Icon n="spark" size={14} />}
            <span class="gp-aiv-t">
              <b>{s(view.by === "ai" ? "ai_made" : "view_by")}</b> {[view.name, ...describe(view.spec)].join(" · ")}
            </span>
            <button type="button" class="gp-link" data-act="view-edit" onClick={() => setTune(true)}>
              {s("edit")}
            </button>
            <button type="button" class="gp-link" data-act="view-reset" onClick={resetView}>
              {s("reset")}
            </button>
          </div>
        )}
        {chips.length > 1 && (
          <div class="gp-chips" role="toolbar" aria-label={H.t("tab.git")}>
            {(["all", ...chips] as const).map((c) => (
              <button type="button" key={c} class={`gp-chip${on === c ? " is-on" : ""}`} data-chip={c} aria-pressed={on === c ? "true" : "false"} onClick={() => setChip(c)}>
                {c === "all" ? s("all") : titles[c]} <span class="gp-n">{c === "all" ? total : counts[c]}</span>
              </button>
            ))}
          </div>
        )}
        {g.files.length === 0 && <p class="git-clean">{H.t("git.clean")}</p>}
        {list
          .filter((sec) => on === "all" || sec.side === on || (!sec.side && sec.rows.some((r) => r.side === on)))
          .map((sec) => {
            const rows = sec.side || on === "all" ? sec.rows : sec.rows.filter((r) => r.side === on);
            return (
              <section class={`gp-sec${sec.side ? " gp-" + sec.side : ""}`} key={sec.key} data-sec={sec.key} aria-label={sec.title}>
                <div class="gp-sec-h">
                  <h2 class="gp-sec-t" dir="auto">
                    {sec.title}
                  </h2>
                  <span class="gp-n">{rows.length}</span>
                  <span class="gp-sp" />
                  <button type="button" class="gp-gear" data-act="view" aria-label={s("view", { name: sec.title })} title={s("view", { name: sec.title })} onClick={() => setTune(true)}>
                    <Icon n="gear" size={17} />
                  </button>
                </div>
                <div class={`gp-card${spec ? " is-cfg" : ""}`}>
                  {rows.map(({ f, side }) => (
                    <Row key={side + f.path} f={f} side={side} canAct={canAct} onOpen={() => open(f, side)} onMenu={(at) => rowMenu(f, side, at)} />
                  ))}
                </div>
              </section>
            );
          })}
        {hidden > 0 && <p class="gp-hint">{s("hidden", { n: hidden })}</p>}
        {g.more > 0 && <p class="git-many">{H.t("git.many", { n: g.more })}</p>}
      </>
    );

  const dock = g.repo && !viewer && !g.op && g.files.length > 0 && (
    <form class="gp-dock" onSubmit={(e) => e.preventDefault()}>
      <input
        class="gp-msg"
        dir="auto"
        value={msg.split("\n")[0]}
        maxLength={1000}
        aria-label={H.t("git.subject")}
        placeholder={H.t("git.subject")}
        onInput={(e) => {
          // The bar holds the subject; a description written in the sheet stays.
          const i = msg.indexOf("\n");
          setMsg((e.currentTarget as HTMLInputElement).value.replace(/[\r\n]/g, " ") + (i >= 0 ? msg.slice(i) : ""));
        }}
        onKeyDown={(e) => e.key === "Enter" && (e.preventDefault(), onCommit(false))}
      />
      <span class="gp-split">
        <button type="button" class="gp-commit" data-act="commit" disabled={!canAct} onClick={() => onCommit(false)}>
          {s("commit_n", { n: staged })}
        </button>
        <button
          type="button"
          class="gp-commit gp-split-more"
          data-act="commit-more"
          aria-haspopup="menu"
          aria-label={s("commit_opts")}
          disabled={!canAct}
          onClick={(e) => {
            const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
            setMenu({
              at: { x: r.right - 100, y: r.top - 150 },
              title: s("commit_opts"),
              items: [
                { label: s("stage_commit"), icon: "check", run: () => onCommit(true) },
                { label: s("amend"), icon: "undo", run: onAmend, hidden: g.log.length === 0 },
                { label: s("stage_all"), icon: "plus", run: () => stageAll(g.files.filter((f) => f.unstaged || f.untracked).map((f) => f.path), true) },
                { label: s("unstage_all"), icon: "undo", run: () => stageAll(g.files.filter((f) => f.staged).map((f) => f.path), false), hidden: staged === 0 },
              ],
            });
          }}
        >
          <Icon n="down" size={16} />
        </button>
      </span>
    </form>
  );

  return (
    <H.View tab="git" title={H.t("tab.git")} sub={<BranchPill g={g} />} actions={
        <Head
          g={g}
          viewer={viewer}
          onMore={(at) =>
            setMenu({
              at,
              title: H.t("git.changes"),
              items: [
                ...chatItems({ kind: "diff" }, H.t("git.changes")),
                { label: s("stage_all"), icon: "plus", run: () => stageAll(g.files.filter((f) => f.unstaged || f.untracked).map((f) => f.path), true), hidden: !canAct },
                { label: s("unstage_all"), icon: "undo", run: () => stageAll(g.files.filter((f) => f.staged).map((f) => f.path), false), hidden: !canAct || staged === 0 },
              ],
            })
          }
        />
      } foot={dock || undefined} bodyClass="gp-body">
      <div class={`gp${stale ? " is-stale" : ""}`}>{body}</div>
      {menu && <Menu {...menu} onClose={() => setMenu(null)} />}
      {tune && (
        <Tune
          spec={tuneSpec}
          onClose={() => setTune(false)}
          onApply={(sp) => {
            setOwn(sp);
            setTune(false);
          }}
        />
      )}
    </H.View>
  );
}

/** BranchPill is the branch with ↑/↓, a button that opens the branches. */
export function BranchPill({ g }: { g: GitState }) {
  if (!g.repo) return null;
  const b = g.branch;
  return (
    <button type="button" class="gp-pill" data-act="branch" aria-label={s("branch", { name: b.name })} onClick={() => H.go({ tab: "git", rest: "", query: { sheet: "remote" } })}>
      <Icon n="branch" size={14} />
      <span class="gp-pill-n" dir="ltr">
        {b.name}
      </span>
      {b.ahead > 0 && <span class="git-ahead" aria-label={s("ahead", { n: b.ahead })}>↑{b.ahead}</span>}
      {b.behind > 0 && <span class="git-behind" aria-label={s("behind", { n: b.behind })}>↓{b.behind}</span>}
      <Icon n="down" size={13} />
    </button>
  );
}

function Head({ g, viewer, onMore }: { g: GitState; viewer: boolean; onMore: (at: At) => void }) {
  if (!g.repo) return null;
  return (
    <>
      <Btn icon="history" act="history" label={s("history")} onClick={() => H.go({ tab: "git", rest: "", query: { view: "history" } })} />
      {!viewer && !g.op && <Btn icon="sync" act="sync" label={s("sync")} onClick={() => H.go({ tab: "git", rest: "", query: { sheet: "remote" } })} />}
      <Btn
        icon="more"
        act="more"
        label={s("more")}
        onClick={(e) => {
          const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
          onMore({ x: r.right - 120, y: r.bottom - 6 });
        }}
      />
    </>
  );
}

function Row({ f, side, canAct, onOpen, onMenu }: { f: GitFile; side: Side; canAct: boolean; onOpen: () => void; onMenu: (at: At) => void }) {
  const { name, dir } = splitPath(f.path);
  const code = side === "staged" ? f.x : side === "conflicts" ? "U" : side === "untracked" ? "U" : f.y;
  const st = side === "untracked" ? "add" : side === "conflicts" ? "del" : code === "D" ? "del" : code === "A" ? "add" : "mod";
  const isOn = side === "staged";
  const tick = canAct && side !== "conflicts";
  const cb = H.useRef(onMenu);
  cb.current = onMenu;
  const press = H.useMemo(() => hold((at) => cb.current(at)), []);
  return (
    <div class="gp-row" data-path={f.path} data-side={side} {...press}>
      <button type="button" class="gp-open" onClick={onOpen}>
        <span class={`gp-st is-${st}`} aria-hidden="true">
          {side === "untracked" ? "U" : code.trim() || "M"}
        </span>
        <span class="gp-t">
          <bdi class="gp-name" dir="ltr">
            {name}
          </bdi>
          <bdi class="gp-dir" dir="ltr">
            {dir || "/"}
          </bdi>
        </span>
      </button>
      {tick && (
        <button
          type="button"
          role="checkbox"
          class={`gp-circ${isOn ? " is-on" : ""}`}
          aria-checked={isOn ? "true" : "false"}
          aria-label={s(isOn ? "tick_unstage" : "tick_stage", { name })}
          onClick={() => H.stage(f.path, !isOn)}
        >
          {isOn && <Icon n="check" size={14} />}
        </button>
      )}
      {side === "conflicts" && <span class="git-note">{H.t("git.resolve_laptop")}</span>}
    </div>
  );
}

/** Tune is ⚙'s sheet: group by status or folder, only paths with a
 * text; Save keeps it on the laptop as a view, the AI can change it. */
function Tune({ spec, onClose, onApply }: { spec: ViewSpec; onClose: () => void; onApply: (s: ViewSpec) => void }) {
  const cond = (spec.filter ?? []).find((c) => c.field === "path" && c.op === "contains");
  const [group, setGroup] = H.useState(spec.group === "path" ? "path" : "state");
  const [only, setOnly] = H.useState(cond?.value ?? "");
  const next = (): ViewSpec => {
    const filter = (spec.filter ?? []).filter((c) => c !== cond);
    if (only.trim()) filter.push({ field: "path", op: "contains", value: only.trim() });
    return { ...spec, view: STATUS_VIEW, group, filter: filter.length ? filter : undefined };
  };
  return (
    <H.Sheet onLater={onClose} label={s("view", { name: H.t("tab.git") })}>
      <div class="gp-tune">
        <h2 class="gp-tune-t">{s("view", { name: H.t("git.changes") })}</h2>
        <p class="gp-lbl">{s("group")}</p>
        <div class="gp-seg" role="radiogroup" aria-label={s("group")}>
          {(["state", "path"] as const).map((v) => (
            <button type="button" key={v} role="radio" aria-checked={group === v ? "true" : "false"} class={group === v ? "is-on" : ""} onClick={() => setGroup(v)}>
              {s(v === "state" ? "g_state" : "g_folder")}
            </button>
          ))}
        </div>
        <label class="gp-lbl" for="gp-only">
          {s("only")}
        </label>
        <input id="gp-only" class="gp-field" dir="ltr" value={only} onInput={(e) => setOnly((e.currentTarget as HTMLInputElement).value)} />
        <div class="gp-tune-acts">
          <button type="button" class="btn btn-neutral" onClick={onClose}>
            {H.t("cancel")}
          </button>
          <button
            type="button"
            class="btn btn-primary"
            data-act="view-save"
            onClick={() => {
              const sp = next();
              onApply(sp);
              H.link.request("view.save", { spec: sp, name: s("view_name") }).then(() => H.toast("ok", s("saved")), H.failed);
            }}
          >
            {s("save")}
          </button>
        </div>
        <button type="button" class="gp-ask" data-act="view-ask" onClick={() => (onClose(), H.draft(s("ask_view_text") + JSON.stringify(next())))}>
          <Icon n="spark" size={16} />
          {s("ask_view")}
        </button>
      </div>
    </H.Sheet>
  );
}
