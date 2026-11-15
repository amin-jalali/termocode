// The Branches sheet (page design 07): the branch pill and ⇅ open it.
// ✕ and "Branches"; one field finds a branch or names a new one; Local,
// Remote and Tags; the current branch first with ✓ and what waits to
// push or pull; "+ New branch from <current>"; Pull, Push and Stash as
// three equal buttons; the sign-in note. Every action asks the laptop
// first (git.checkout, git.checkout_tag, git.branch_create, git.pull,
// git.push, git.stash, git.stash_pop): git there signs in, never the page.
import type { GitState } from "../state/git";
import { H, h } from "./jsx";
import { s } from "./strings";
import { Icon } from "./ui";

type Tab = "local" | "remote" | "tags";
type Ask = { op: string; args: Record<string, string>; text: string } | null;

interface Lists {
  current: string;
  branches: string[];
  remotes: string[];
  stashes: number;
}

const names = (v: unknown): string[] => (Array.isArray(v) ? v.filter((b): b is string => typeof b === "string") : []);

function readLists(r: unknown): Lists {
  const o = typeof r === "object" && r !== null ? (r as Record<string, unknown>) : {};
  return { current: typeof o.current === "string" ? o.current : "", branches: names(o.branches), remotes: names(o.remotes), stashes: typeof o.stashes === "number" ? o.stashes : 0 };
}

export function Branches({ g, onClose }: { g: GitState; onClose: () => void }) {
  const [lists, setLists] = H.useState<Lists | null>(null);
  const [tags, setTags] = H.useState<string[] | null>(null);
  const [tab, setTab] = H.useState<Tab>("local");
  const [q, setQ] = H.useState("");
  const [ask, setAsk] = H.useState<Ask>(null);
  const find = H.useRef<HTMLInputElement | null>(null);
  const viewer = H.link.meta.value?.role === "viewer";
  const cur = g.branch.name;
  H.useEffect(() => {
    H.link.request("git.branches", {}).then(
      (r) => setLists(readLists(r)),
      (e: unknown) => {
        H.failed(e);
        onClose();
      },
    );
  }, []);
  H.useEffect(() => {
    if (tab === "tags" && !tags)
      H.link.request("git.tags", {}).then(
        (r) => setTags(names((r as { tags?: unknown } | null)?.tags)),
        (e: unknown) => (H.failed(e), setTags([])),
      );
  }, [tab]);

  if (ask) return <H.AskLaptop label={s("branches")} text={ask.text} send={() => H.link.request(ask.op, ask.args)} onDone={onClose} />;

  const want = q.trim();
  const match = (n: string) => !want || n.toLowerCase().includes(want.toLowerCase());
  const all = tab === "local" ? lists?.branches ?? [] : tab === "remote" ? lists?.remotes ?? [] : tags ?? [];
  const rows = (tab === "local" ? [cur, ...all.filter((n) => n !== cur)] : all).filter((n) => n && match(n));
  const loading = tab === "tags" ? !tags : !lists;
  const fresh = want !== "" && !(lists?.branches ?? []).includes(want) && want !== cur;
  const pick = (n: string) => {
    if (viewer) return;
    if (tab === "local") n !== cur && setAsk({ op: "git.checkout", args: { branch: n }, text: H.t("git.checkout_q", { branch: n }) });
    else if (tab === "remote") setAsk({ op: "git.checkout", args: { remote: n }, text: H.t("git.track_q", { branch: n.slice(n.indexOf("/") + 1), remote: n }) });
    else setAsk({ op: "git.checkout_tag", args: { tag: n }, text: s("tag_q", { tag: n }) });
  };
  const create = () => {
    if (!want) {
      find.current?.focus();
      return;
    }
    setAsk({ op: "git.branch_create", args: { name: want }, text: s("create_q", { name: want, from: cur }) });
  };
  const state = [g.branch.ahead > 0 && s("ahead", { n: g.branch.ahead }), g.branch.behind > 0 && s("behind", { n: g.branch.behind })].filter(Boolean).join(" · ") || s("in_step");

  return (
    <H.Sheet onLater={onClose} label={s("branches")}>
      <div class="gp-br">
        <div class="gp-cs-h">
          <button type="button" class="gp-ib gp-x" data-act="close" aria-label={H.t("cancel")} onClick={onClose}>
            <Icon n="close" size={22} />
          </button>
          <h2 class="gp-cs-t">{s("branches")}</h2>
        </div>
        <label class="gp-find">
          <Icon n="search" size={17} />
          <input
            ref={find}
            dir="ltr"
            value={q}
            aria-label={s("find_branch")}
            placeholder={s("find_branch")}
            onInput={(e) => setQ((e.currentTarget as HTMLInputElement).value)}
            onKeyDown={(e) => {
              if (e.key !== "Enter") return;
              e.preventDefault();
              if (rows.length === 1 && rows[0] !== cur) pick(rows[0]);
              else if (fresh && tab === "local" && !viewer) create();
            }}
          />
        </label>
        <div class="gp-seg gp-br-seg" role="tablist" aria-label={s("branches")}>
          {(["local", "remote", "tags"] as const).map((v) => (
            <button type="button" key={v} role="tab" data-tab={v} aria-selected={tab === v ? "true" : "false"} class={tab === v ? "is-on" : ""} onClick={() => setTab(v)}>
              {s(v === "local" ? "local" : v === "remote" ? "remote" : "tags")}
            </button>
          ))}
        </div>
        <div class="gp-card gp-br-list is-cfg">
          {loading ? (
            <p class="gp-hint">{H.t("git.branches_loading")}</p>
          ) : (
            rows.map((n) => (
              <button type="button" class="gp-row gp-br-row" key={n} data-branch={n} aria-current={tab === "local" && n === cur ? "true" : undefined} onClick={() => pick(n)}>
                <span class="gp-br-tick" aria-hidden="true">
                  {tab === "local" && n === cur && <Icon n="check" size={17} />}
                </span>
                <span class="gp-t">
                  <span class="gp-name gp-mono" dir="ltr">
                    {n}
                  </span>
                  {tab === "local" && n === cur && <span class="gp-dir gp-sans">{s("current", { state })}</span>}
                </span>
              </button>
            ))
          )}
          {!loading && rows.length === 0 && !fresh && <p class="gp-hint">{s("no_match")}</p>}
          {tab === "local" && !viewer && (
            <button type="button" class="gp-row gp-br-new" data-act="new-branch" onClick={create}>
              <span class="gp-br-tick" aria-hidden="true">
                <Icon n="plus" size={18} />
              </span>
              <span class="gp-name">{fresh ? s("create_named", { name: want, from: cur }) : s("new_from", { from: cur })}</span>
            </button>
          )}
        </div>
        {!viewer && (
          <div class="gp-br-acts">
            <button type="button" class="gp-btn" data-act="pull" onClick={() => setAsk({ op: "git.pull", args: {}, text: H.t("git.pull_q", { branch: cur }) })}>
              <Icon n="downarrow" size={16} />
              {H.t("git.pull")}
            </button>
            <button type="button" class="gp-btn" data-act="push" onClick={() => setAsk({ op: "git.push", args: {}, text: H.t("git.push_q", { branch: cur }) })}>
              <Icon n="uparrow" size={16} />
              {H.t("git.push")}
            </button>
            {g.files.length === 0 && (lists?.stashes ?? 0) > 0 ? (
              <button type="button" class="gp-btn" data-act="pop" onClick={() => setAsk({ op: "git.stash_pop", args: {}, text: H.t("git.pop_q") })}>
                {s("pop")}
              </button>
            ) : (
              <button type="button" class="gp-btn" data-act="stash" disabled={g.files.length === 0} onClick={() => setAsk({ op: "git.stash", args: {}, text: H.t("git.stash_q") })}>
                {s("stash")}
              </button>
            )}
          </div>
        )}
        <p class="gp-hint gp-br-note">{H.t("git.signin_laptop")}</p>
      </div>
    </H.Sheet>
  );
}
