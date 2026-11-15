// History (#/git?view=history, page design 07): the branch's commits,
// newest first: a dot, the subject, the mono hash · author · time and a
// "not pushed" tag on the commits HEAD has and its remote has not. 🔍
// finds a commit; a saved git.log view (the AI's) shapes the list with
// its banner. A tap shows the commit (git.show); a long press: Ask AI
// about this, Add to chat, Show changes, Copy hash.
import type { GitCommit, GitState } from "../state/git";
import { age } from "../state/git";
import { usePageView, ViewBanner } from "./Changes";
import { H, h } from "./jsx";
import { s } from "./strings";
import { Btn, hold, Menu, type At, type MenuItem } from "./ui";
import { LOG_VIEW, logRows } from "./view";

export function History({ g }: { g: GitState }) {
  const [menu, setMenu] = H.useState<{ at: At; title: string; items: MenuItem[] } | null>(null);
  const [find, setFind] = H.useState<string | null>(null);
  const [view, resetView] = usePageView(!g.loading, LOG_VIEW);
  const back = () => H.go({ tab: "git", rest: "", query: {} });
  const show = (c: GitCommit) => H.go({ tab: "git", rest: "", query: { view: "history", show: c.hash } });
  const { rows, hidden } = logRows(g.log, view?.spec ?? null);
  const q = (find ?? "").trim().toLowerCase();
  const shown = q ? rows.filter((c) => (c.subject + " " + c.hash + " " + c.author).toLowerCase().includes(q)) : rows;
  const about = (c: GitCommit) => s("about_commit", { hash: c.hash, subject: c.subject });

  const rowMenu = (c: GitCommit, at: At) =>
    setMenu({
      at,
      title: `${c.hash} ${c.subject}`,
      items: [
        { label: s("ask"), icon: "spark", kind: "ai", run: () => H.draft(about(c)) },
        { label: s("add_chat") + " ↩", icon: "link", run: () => (H.draft(about(c), true), H.toast("ok", s("added"))) },
        { label: s("show_changes"), icon: "plusminus", run: () => show(c) },
        {
          label: s("copy_hash"),
          icon: "copy",
          run: () => {
            H.copy(c.hash);
            H.toast("ok", s("copied", { hash: c.hash }));
          },
        },
      ],
    });

  return (
    <H.View
      tab="git"
      name="git-history"
      title={s("history")}
      sub={
        <span class="gp-sub" dir="ltr">
          {g.branch.name}
        </span>
      }
      back={{ label: H.t("back"), onClick: back }}
      actions={<Btn icon="search" act="find" label={s("find_commit")} onClick={() => setFind(find === null ? "" : null)} />}
      bodyClass="gp-body"
    >
      <div class="gp">
        {find !== null && (
          <label class="gp-find gp-find-page">
            <input
              dir="auto"
              value={find}
              aria-label={s("find_commit")}
              placeholder={s("find_commit")}
              ref={(el) => void (el && find === "" && el.focus())}
              onInput={(e) => setFind((e.currentTarget as HTMLInputElement).value)}
            />
          </label>
        )}
        {view && <ViewBanner view={view} onReset={resetView} />}
        <div class={`gp-card gp-log${view ? " is-cfg" : ""}`}>
          {shown.map((c) => (
            <Commit key={c.hash} c={c} onShow={() => show(c)} onMenu={(at) => rowMenu(c, at)} />
          ))}
        </div>
        {shown.length === 0 && <p class="gp-hint">{s("no_commits")}</p>}
        {hidden > 0 && <p class="gp-hint">{s("hidden", { n: hidden })}</p>}
      </div>
      {menu && <Menu {...menu} onClose={() => setMenu(null)} />}
    </H.View>
  );
}

function Commit({ c, onShow, onMenu }: { c: GitCommit; onShow: () => void; onMenu: (at: At) => void }) {
  const cb = H.useRef(onMenu);
  cb.current = onMenu;
  const press = H.useMemo(() => hold((at) => cb.current(at)), []);
  return (
    <button type="button" class={`gp-row gp-commit-row${c.unpushed ? " is-unpushed" : ""}`} data-hash={c.hash} onClick={onShow} {...press}>
      <span class="gp-dot" aria-hidden="true" />
      <span class="gp-t">
        <span class="gp-name" dir="auto">
          {c.subject}
        </span>
        <span class="gp-dir" dir="ltr">
          {[c.hash, c.author, age(c.when, Date.now())].filter(Boolean).join(" · ")}
        </span>
      </span>
      {c.unpushed && <span class="gp-tag">{s("not_pushed")}</span>}
    </button>
  );
}
