// Branch and remote (WEB-42, web design 2.6): push, pull, stash, pop the
// stash and switch branch. Every one waits for a laptop card: the page
// asks first (Cancel first and focused), then the laptop runs its own git
// command with its own sign-in; no password or token goes through the
// page. The branch list comes from git.branches when the sheet opens.
import { h } from "preact";
import { useEffect, useState } from "preact/hooks";
import { link } from "../../session";
import { t } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { Button } from "../../ui/Button";
import { Sheet } from "../../ui/Sheet";
import { gitFailed } from "./errors";

interface Branches {
  current: string;
  branches: string[];
  stashes: number;
  upstream: boolean;
}

function readBranches(r: unknown): Branches {
  const o = typeof r === "object" && r !== null ? (r as Record<string, unknown>) : {};
  return {
    current: typeof o.current === "string" ? o.current : "",
    branches: Array.isArray(o.branches) ? o.branches.filter((b): b is string => typeof b === "string") : [],
    stashes: typeof o.stashes === "number" ? o.stashes : 0,
    upstream: o.upstream === true,
  };
}

type Ask = { op: string; args: Record<string, string>; text: string } | null;

export function RemoteSheet({ branch, ahead, behind, changed, onDone }: { branch: string; ahead: number; behind: number; changed: number; onDone(): void }) {
  const [b, setB] = useState<Branches | null>(null);
  const [ask, setAsk] = useState<Ask>(null);
  useEffect(() => {
    link.request("git.branches", {}).then((r) => setB(readBranches(r)), (e: unknown) => {
      gitFailed(e);
      onDone();
    });
  }, []);

  if (ask) return <AskLaptop label={t("git.remote")} text={ask.text} send={() => link.request(ask.op, ask.args)} onDone={onDone} />;
  const others = (b?.branches ?? []).filter((x) => x !== branch);
  return (
    <Sheet onLater={onDone} label={t("git.remote")}>
      <div class="remote">
        <p class="remote-branch" dir="ltr">
          {branch}
          {ahead > 0 && <span class="git-ahead"> ↑{ahead}</span>}
          {behind > 0 && <span class="git-behind"> ↓{behind}</span>}
        </p>
        <p class="ask-note">{t("git.signin_laptop")}</p>
        <div class="file-menu">
          <button type="button" class="btn btn-neutral" onClick={onDone}>
            {t("cancel")}
          </button>
          <Button needsLink onClick={() => setAsk({ op: "git.push", args: {}, text: t("git.push_q", { branch }) })}>
            {t("git.push")}
          </Button>
          <Button needsLink onClick={() => setAsk({ op: "git.pull", args: {}, text: t("git.pull_q", { branch }) })}>
            {t("git.pull")}
          </Button>
          {changed > 0 && (
            <Button needsLink onClick={() => setAsk({ op: "git.stash", args: {}, text: t("git.stash_q") })}>
              {t("git.stash")}
            </Button>
          )}
          {(b?.stashes ?? 0) > 0 && (
            <Button needsLink onClick={() => setAsk({ op: "git.stash_pop", args: {}, text: t("git.pop_q") })}>
              {t("git.pop")}
            </Button>
          )}
        </div>
        <h2 class="group-title">{t("git.switch")}</h2>
        {!b ? (
          <p class="empty">{t("git.branches_loading")}</p>
        ) : others.length === 0 ? (
          <p class="empty">{t("git.one_branch")}</p>
        ) : (
          <div class="branch-list">
            {others.map((x) => (
              <button
                type="button"
                class="branch-row"
                key={x}
                data-branch={x}
                dir="ltr"
                onClick={() => setAsk({ op: "git.checkout", args: { branch: x }, text: t("git.checkout_q", { branch: x }) })}
              >
                {x}
              </button>
            ))}
          </div>
        )}
      </div>
    </Sheet>
  );
}
