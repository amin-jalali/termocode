// gitphone.js: the phone's Git pages, built alone (cmd/webbuild
// gitphoneChunk) with gitphone.css. home.js loads it on the phone's first
// Git (lib/gitphone.ts), calls init with its host, then draws GitPage.
// Routes: #/git (the changes), #/git?view=history, #/git/<path> (a
// file's diff; ?side=staged), #/git?show=<hash>, #/git?sheet=remote.
import "./gitphone.css";
import type { GitHost } from "./api";
import { readGit, type GitState } from "../state/git";
import { Changes } from "./Changes";
import { History } from "./History";
import { H, h, Fragment, setHost } from "./jsx";
import { s } from "./strings";

/** init takes the page's host, once, before the first draw. */
export function init(host: GitHost): void {
  setHost(host);
}

export function GitPage() {
  H.useEffect(() => H.link.subscribe("git"), []);
  const st = H.store.topic("git").value;
  const g = readGit(st?.data);
  const r = H.route.value;
  const viewer = H.link.meta.value?.role === "viewer";
  const canAct = !viewer && H.live.value;
  const [discard, setDiscard] = H.useState<string | null>(null);
  const [ask, setAsk] = H.useState<{ msg: string; all: boolean } | null>(null);
  const [msg, setMsg] = H.useState("");
  const top = { tab: "git" as const, rest: "", query: {} };
  const close = () => H.go(top);

  const path = r.tab === "git" ? r.rest : "";
  const file = path ? g.files.find((f) => f.path === path) ?? null : null;
  const staged = r.query.side === "staged";
  const commit = (all: boolean) => {
    if (!msg.trim()) {
      H.toast("warn", s("msg_first"));
      document.querySelector<HTMLInputElement>(".gp-msg")?.focus();
      return;
    }
    if (!all && !g.files.some((f) => f.staged)) return void H.toast("warn", s("stage_first"));
    setAsk({ msg, all });
  };

  if (!st) return <Changes g={{ ...g, loading: true }} stale={false} canAct={false} msg={msg} setMsg={setMsg} onCommit={commit} onDiscard={setDiscard} />;
  return (
    <>
      {r.query.view === "history" ? <History g={g} /> : <Changes g={g} stale={st.stale === true} canAct={canAct} msg={msg} setMsg={setMsg} onCommit={commit} onDiscard={setDiscard} />}
      {path && (
        <H.DiffSheet
          file={file}
          path={path}
          staged={file ? (staged ? file.staged : !(file.unstaged || file.untracked)) : staged}
          canAct={canAct}
          onSide={(on) => H.go({ tab: "git", rest: path, query: on ? { side: "staged" } : {} }, true)}
          onClose={close}
          onStage={(on) => H.stage(path, on)}
          onDiscard={() => setDiscard(path)}
        />
      )}
      {r.query.show && <H.ShowSheet hash={r.query.show} onClose={() => H.go({ tab: "git", rest: "", query: r.query.view ? { view: r.query.view } : {} })} />}
      {r.query.sheet === "remote" && g.repo && <H.RemoteSheet branch={g.branch.name} ahead={g.branch.ahead} behind={g.branch.behind} changed={g.files.length} onDone={close} />}
      {discard && (
        <H.AskLaptop cls="discard" label={H.t("git.discard")} text={H.t("git.discard_body", { path: discard })} send={() => H.link.request("git.discard", { paths: [discard] })} onDone={() => setDiscard(null)} />
      )}
      {ask && <CommitAsk g={g} {...ask} onDone={(ok) => (setAsk(null), ok && setMsg(""))} />}
    </>
  );
}

/** CommitAsk asks before a commit: "Commit 2 staged files to main?",
 * Cancel first and focused. */
function CommitAsk({ g, msg, all, onDone }: { g: GitState; msg: string; all: boolean; onDone: (ok?: boolean) => void }) {
  const [busy, setBusy] = H.useState(false);
  const n = all ? g.files.length : g.files.filter((f) => f.staged).length;
  const run = async () => {
    setBusy(true);
    try {
      if (all) await H.link.request("git.stage", { paths: g.files.filter((f) => f.unstaged || f.untracked).map((f) => f.path) });
      const r = (await H.link.request("git.commit", { message: msg.trim() })) as { hash?: unknown; files?: unknown } | null;
      H.toast("ok", H.t("git.committed", { hash: String(r?.hash ?? ""), n: Number(r?.files ?? n) }));
      setBusy(false);
      onDone(true);
    } catch (e) {
      H.failed(e);
      setBusy(false);
      onDone();
    }
  };
  return (
    <H.Sheet onLater={() => onDone()} label={H.t("git.commit_do")}>
      <div class="gp-ask-commit commit-ask" role="group" aria-label={H.t("git.commit_do")}>
        <p class="commit-ask-text">{H.tn("git.commit_confirm", n, { branch: g.branch.name })}</p>
        <div class="choices">
          <button type="button" class="btn btn-neutral" onClick={() => onDone()}>
            {H.t("cancel")}
          </button>
          <button type="button" class="btn btn-primary" disabled={busy} onClick={run}>
            {H.t("git.commit_do")}
          </button>
        </div>
      </div>
    </H.Sheet>
  );
}
