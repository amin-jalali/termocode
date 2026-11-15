// History (#/git?view=history): the branch's recent commits, newest
// first; a tap shows one (git.show).
import type { GitState } from "../state/git";
import { age } from "../state/git";
import { H, h } from "./jsx";
import { s } from "./strings";

export function History({ g }: { g: GitState }) {
  const back = () => H.go({ tab: "git", rest: "", query: {} });
  return (
    <H.View tab="git" name="git-history" title={s("history")} sub={<span class="gp-sub" dir="ltr">{g.branch.name}</span>} back={{ label: H.t("back"), onClick: back }} bodyClass="gp-body">
      <div class="gp">
        <div class="gp-card gp-log">
          {g.log.map((c) => (
            <button type="button" class="gp-row gp-commit-row" key={c.hash} data-hash={c.hash} onClick={() => H.go({ tab: "git", rest: "", query: { view: "history", show: c.hash } })}>
              <span class="gp-t">
                <span class="gp-name" dir="auto">
                  {c.subject}
                </span>
                <span class="gp-dir" dir="ltr">
                  {c.hash} · {c.author} · {age(c.when, Date.now())}
                </span>
              </span>
            </button>
          ))}
        </div>
      </div>
    </H.View>
  );
}
