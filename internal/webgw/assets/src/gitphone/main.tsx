// gitphone.js: the phone's Git pages, built alone (cmd/webbuild
// gitphoneChunk) with gitphone.css. home.js loads it on the phone's first
// Git (lib/gitphone.ts), calls init with its host, then draws GitPage.
// Routes: #/git (the changes), #/git?view=history, #/git/<path> (a
// file's diff; ?side=staged), #/git?show=<hash>, #/git?sheet=remote.
import "./gitphone.css";
import type { GitHost } from "./api";
import { readGit } from "../state/git";
import { Changes } from "./Changes";
import { cleanMessage, CommitSheet } from "./CommitSheet";
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
  const [sheet, setSheet] = H.useState(false);
  const [amend, setAmend] = H.useState(false);
  const [msg, setMsg] = H.useState("");
  const top = { tab: "git" as const, rest: "", query: {} };
  const close = () => H.go(top);

  const path = r.tab === "git" ? r.rest : "";
  const file = path ? g.files.find((f) => f.path === path) ?? null : null;
  const staged = r.query.side === "staged";
  // Commit opens the commit sheet; "Stage all and commit" stages first.
  const commit = (all: boolean) => {
    const rest = g.files.filter((f) => f.unstaged || f.untracked).map((f) => f.path);
    if (all && rest.length) H.link.request("git.stage", { paths: rest }).then(() => setSheet(true), H.failed);
    else if (!g.files.some((f) => f.staged)) H.toast("warn", s("stage_first"));
    else setSheet(true);
  };

  if (!st) return <Changes g={{ ...g, loading: true }} stale={false} canAct={false} msg={msg} setMsg={setMsg} onCommit={commit} onAmend={() => setAmend(true)} onDiscard={setDiscard} />;
  return (
    <>
      {r.query.view === "history" ? <History g={g} /> : <Changes g={g} stale={st.stale === true} canAct={canAct} msg={msg} setMsg={setMsg} onCommit={commit} onAmend={() => setAmend(true)} onDiscard={setDiscard} />}
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
      {sheet && g.repo && <CommitSheet g={g} msg={msg} setMsg={setMsg} onClose={() => setSheet(false)} onDone={() => setSheet(false)} />}
      {amend && (
        <H.AskLaptop
          label={s("amend")}
          text={msg.trim() ? s("amend_msg", { subject: msg.trim().split("\n")[0] }) : s("amend_keep")}
          send={() => H.link.request("git.amend", msg.trim() ? { message: cleanMessage(msg) } : {})}
          onConfirmed={() => setMsg("")}
          onDone={() => setAmend(false)}
        />
      )}
    </>
  );
}
