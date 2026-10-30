// Pull requests and CI checks (desktop editor tabs "@git:prs" and
// "@git:pr:<n>"), read through the laptop's gh. The list tab shows the
// checked-out branch's CI runs (the laptop's own CI watch) and the open
// pull requests; a pull request's tab shows its description, checks,
// files and reviews. Check Out, Approve, Request Changes, Comment and
// Merge each ask the laptop. Links open only after the confirm sheet.
import { Fragment, h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { CIStatus, PRCheck, PRItem, PRList, PRView } from "../../../../../webproto/proto";
import { prTab } from "../../lib/gittab";
import { askLink } from "../../lib/links";
import { showFile } from "../../lib/workbench";
import { link, live, store } from "../../session";
import { t, type StringKey } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { Cod } from "../../ui/icons";
import { Md } from "../../ui/Markdown";
import { View } from "../View";
import { gitErrText } from "./errors";

type State = PRCheck["state"];
const STATE_ICON: Record<State, string> = { pass: "pass-filled", fail: "error", pending: "loading", none: "circle-slash" };
const STATE_WORD: Record<State, StringKey> = { pass: "pr.checks_pass", fail: "pr.checks_fail", pending: "pr.checks_pending", none: "pr.checks_none" };

/** StateIcon is a check's or a run's state, coloured. */
function StateIcon({ s }: { s: State }) {
  return (
    <span class={`pr-st pr-st-${s}`} title={t(STATE_WORD[s])} role="img" aria-label={t(STATE_WORD[s])}>
      <Cod name={STATE_ICON[s]} />
    </span>
  );
}

/** useRead asks op once (again when again changes) and keeps the answer. */
function useRead<T>(op: string, args: object, again: unknown): { data: T | null; err: string; busy: boolean } {
  const [data, setData] = useState<T | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const on = live.value;
  const key = JSON.stringify(args);
  useEffect(() => {
    if (!on) return;
    let stop = false;
    setBusy(true);
    link
      .request(op, args)
      .then(
        (r) => {
          if (stop) return;
          setData(r as T);
          setErr("");
        },
        (e: unknown) => !stop && setErr(gitErrText(e)),
      )
      .finally(() => !stop && setBusy(false));
    return () => {
      stop = true;
    };
  }, [op, key, again, on]);
  return { data, err, busy };
}

const reviewWord = (r: PRItem["review"]): StringKey | null =>
  r === "APPROVED" ? "pr.approved" : r === "CHANGES_REQUESTED" ? "pr.changes_asked" : r === "REVIEW_REQUIRED" ? "pr.review_needed" : null;

/** PRRow is one pull request in the list. */
function PRRow({ p }: { p: PRItem }) {
  const rw = reviewWord(p.review);
  return (
    <button type="button" class="pr-row" data-pr={p.number} onClick={() => showFile(prTab(p.number), true)}>
      <span class={`pr-icon${p.draft ? " is-draft" : ""}`}>
        <Cod name="git-pull-request" />
      </span>
      <span class="pr-main">
        <span class="pr-title" dir="auto">
          {p.title} <span class="pr-num">#{p.number}</span>
        </span>
        <span class="pr-sub" dir="auto">
          {t("pr.by", { author: p.author, branch: p.branch, base: p.base })}
          {p.draft && <span class="pr-chip">{t("pr.draft")}</span>}
          {rw && <span class={`pr-chip pr-rw-${p.review?.toLowerCase()}`}>{t(rw)}</span>}
        </span>
      </span>
      <StateIcon s={p.checks} />
    </button>
  );
}

/** BranchCI is the checked-out branch's CI, as the laptop watches it. */
function BranchCI({ again }: { again: number }) {
  const git = store.topic("git").value;
  const { data, err } = useRead<CIStatus>("ci.status", {}, `${again}/${git?.seq ?? 0}`);
  if (err) return <p class="pr-note">{err}</p>;
  if (!data) return <p class="pr-note">{t("loading")}</p>;
  if (data.loading) return <p class="pr-note">{t("pr.ci_loading")}</p>;
  if (data.error) return <p class="pr-note">{data.error}</p>;
  return (
    <div class="pr-ci">
      <p class="pr-ci-head">
        <StateIcon s={data.state} />
        <span>{t("pr.ci_of", { branch: data.branch })}</span>
        {data.head && (
          <span class="pr-mono" dir="ltr">
            {data.head}
          </span>
        )}
      </p>
      {data.runs.length === 0 ? (
        <p class="pr-note">{t("pr.no_runs")}</p>
      ) : (
        <ul class="pr-checks">
          {data.runs.map((r, i) => (
            <li key={i} class="pr-check">
              <StateIcon s={r.state} />
              <span class="pr-check-name" dir="auto">
                {r.workflow}
              </span>
              <span class="pr-check-sub" dir="auto">
                {r.title}
              </span>
              {r.url && (
                <button type="button" class="tb tb-icon" aria-label={t("pr.open_link")} title={t("pr.open_link")} onClick={() => askLink(r.url ?? "")}>
                  <Cod name="link-external" />
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {data.pr && <PRRow p={data.pr} />}
    </div>
  );
}

export function PRListView() {
  const [again, setAgain] = useState(0);
  const { data, err, busy } = useRead<PRList>("pr.list", {}, again);
  return (
    <View
      tab="files"
      name="prs"
      title={t("pr.tab")}
      bodyClass="pr-body"
      actions={
        <button type="button" class="tb tb-icon" data-act="refresh" aria-label={t("ctr.refresh")} title={t("ctr.refresh")} disabled={busy} onClick={() => setAgain((x) => x + 1)}>
          <Cod name="refresh" />
        </button>
      }
    >
      <div class="pr-page">
        <section class="pr-sec" aria-label={t("pr.this_branch")}>
          <h2 class="pr-sec-title">{t("pr.this_branch")}</h2>
          <BranchCI again={again} />
        </section>
        <section class="pr-sec" aria-label={t("pr.open_prs")}>
          <h2 class="pr-sec-title">{t("pr.open_prs")}</h2>
          {err ? (
            <p class="pr-note">{err}</p>
          ) : !data ? (
            <p class="pr-note">{t("loading")}</p>
          ) : data.items.length === 0 ? (
            <p class="pr-note">{t("pr.none")}</p>
          ) : (
            <div class="pr-list">
              {data.items.map((p) => (
                <PRRow key={p.number} p={p} />
              ))}
            </div>
          )}
        </section>
      </div>
    </View>
  );
}

type Ask =
  | { k: "checkout" }
  | { k: "merge"; method: "squash" | "merge" | "rebase" }
  | { k: "review"; kind: "approve" | "request_changes" | "comment"; body: string };

const METHODS: readonly { m: "squash" | "merge" | "rebase"; label: StringKey }[] = [
  { m: "squash", label: "pr.squash" },
  { m: "merge", label: "pr.merge_commit" },
  { m: "rebase", label: "pr.rebase_merge" },
];

/** ReviewBox takes a review's message, then asks the laptop. */
function ReviewBox({ kind, onAsk, onCancel }: { kind: "approve" | "request_changes" | "comment"; onAsk: (body: string) => void; onCancel: () => void }) {
  const [body, setBody] = useState("");
  const area = useRef<HTMLTextAreaElement | null>(null);
  useEffect(() => area.current?.focus(), []);
  const need = kind !== "approve";
  return (
    <div class="pr-review-box">
      <textarea
        ref={area}
        class="pr-review-text"
        dir="auto"
        rows={4}
        value={body}
        maxLength={16000}
        aria-label={t(need ? "pr.message" : "pr.message_opt")}
        placeholder={t(need ? "pr.message" : "pr.message_opt")}
        onInput={(e) => setBody((e.currentTarget as HTMLTextAreaElement).value)}
      />
      <div class="pr-review-acts">
        <button type="button" class="gv-btn gv-btn-sec" onClick={onCancel}>
          {t("cancel")}
        </button>
        <button type="button" class="gv-btn" data-act="send-review" disabled={need && body.trim() === ""} onClick={() => onAsk(body)}>
          {t("pr.send_review")}
        </button>
      </div>
    </div>
  );
}

export function PRDetailView({ n }: { n: number }) {
  const [again, setAgain] = useState(0);
  const { data, err, busy } = useRead<PRView>("pr.view", { number: n }, again);
  const [ask, setAsk] = useState<Ask | null>(null);
  const [writing, setWriting] = useState<"approve" | "request_changes" | "comment" | null>(null);
  const [method, setMethod] = useState<"squash" | "merge" | "rebase">("squash");
  const viewer = link.meta.value?.role === "viewer";
  const on = live.value;
  const p = data?.item;
  const open = data?.state === "OPEN";

  const askText = (a: Ask): string => {
    switch (a.k) {
      case "checkout":
        return t("pr.checkout_body", { n, branch: p?.branch ?? "" });
      case "merge":
        return t("pr.merge_body", { n, branch: p?.branch ?? "", base: p?.base ?? "" });
      case "review":
        return t(a.kind === "approve" ? "pr.approve_body" : a.kind === "comment" ? "pr.comment_body" : "pr.changes_body", { n });
    }
  };
  const send = (a: Ask) =>
    a.k === "checkout"
      ? link.request("pr.checkout", { number: n })
      : a.k === "merge"
        ? link.request("pr.merge", { number: n, method: a.method })
        : link.request("pr.review", a.body.trim() ? { number: n, kind: a.kind, body: a.body } : { number: n, kind: a.kind });

  return (
    <View
      tab="files"
      name="pr"
      title={t("pr.tab_one", { n })}
      bodyClass="pr-body"
      actions={
        <>
          {p?.url && (
            <button type="button" class="tb tb-icon" data-act="open" aria-label={t("pr.open_link")} title={t("pr.open_link")} onClick={() => askLink(p.url)}>
              <Cod name="link-external" />
            </button>
          )}
          <button type="button" class="tb tb-icon" data-act="refresh" aria-label={t("ctr.refresh")} title={t("ctr.refresh")} disabled={busy} onClick={() => setAgain((x) => x + 1)}>
            <Cod name="refresh" />
          </button>
        </>
      }
    >
      {err ? (
        <p class="empty refused">{err}</p>
      ) : !data || !p ? (
        <p class="empty">{t("loading")}</p>
      ) : (
        <div class="pr-page pr-detail">
          <h1 class="pr-h" dir="auto">
            {p.title} <span class="pr-num">#{p.number}</span>
          </h1>
          <p class="pr-meta">
            <span class={`pr-state pr-state-${data.state.toLowerCase()}${p.draft ? " is-draft" : ""}`}>
              <Cod name="git-pull-request" /> {t(p.draft ? "pr.draft" : data.state === "MERGED" ? "pr.merged" : data.state === "CLOSED" ? "pr.closed" : "link.open")}
            </span>
            <span dir="auto">{t("pr.wants", { author: p.author, branch: p.branch, base: p.base })}</span>
          </p>
          {!viewer && open && (
            <div class="pr-acts" role="group" aria-label={t("pr.actions")}>
              <button type="button" class="gv-btn gv-btn-sec" data-act="checkout" disabled={!on} onClick={() => setAsk({ k: "checkout" })}>
                {t("pr.checkout")}
              </button>
              <button type="button" class="gv-btn gv-btn-sec" data-act="approve" disabled={!on} onClick={() => setWriting("approve")}>
                {t("pr.approve")}
              </button>
              <button type="button" class="gv-btn gv-btn-sec" data-act="changes" disabled={!on} onClick={() => setWriting("request_changes")}>
                {t("pr.request_changes")}
              </button>
              <button type="button" class="gv-btn gv-btn-sec" data-act="comment" disabled={!on} onClick={() => setWriting("comment")}>
                {t("pr.comment")}
              </button>
              <span class="gm-gap" />
              {!p.draft && (
                <span class="pr-merge">
                  <select class="pr-method" value={method} aria-label={t("pr.method")} onChange={(e) => setMethod((e.currentTarget as HTMLSelectElement).value as typeof method)}>
                    {METHODS.map((x) => (
                      <option key={x.m} value={x.m}>
                        {t(x.label)}
                      </option>
                    ))}
                  </select>
                  <button type="button" class="gv-btn" data-act="merge" disabled={!on} onClick={() => setAsk({ k: "merge", method })}>
                    {t("pr.merge")}
                  </button>
                </span>
              )}
            </div>
          )}
          {writing && (
            <ReviewBox
              key={writing}
              kind={writing}
              onCancel={() => setWriting(null)}
              onAsk={(body) => {
                setWriting(null);
                setAsk({ k: "review", kind: writing, body });
              }}
            />
          )}
          {data.mergeable === "CONFLICTING" && (
            <p class="gr-warn" role="note">
              <Cod name="warning" /> {t("pr.conflicting", { base: p.base })}
            </p>
          )}
          <section class="pr-sec" aria-label={t("gg.col_desc")}>
            <h2 class="pr-sec-title">{t("gg.col_desc")}</h2>
            <div class="pr-md">{data.body && data.body.c?.length ? <Md doc={data.body} /> : <p class="pr-note">{t("pr.no_description")}</p>}</div>
          </section>
          <section class="pr-sec" aria-label={t("pr.checks")}>
            <h2 class="pr-sec-title">
              {t("pr.checks")} <StateIcon s={p.checks} />
            </h2>
            {data.checks.length === 0 ? (
              <p class="pr-note">{t("pr.checks_none")}</p>
            ) : (
              <ul class="pr-checks">
                {data.checks.map((c, i) => (
                  <li key={i} class="pr-check">
                    <StateIcon s={c.state} />
                    <span class="pr-check-name" dir="auto">
                      {c.name}
                    </span>
                    {c.url && (
                      <button type="button" class="tb tb-icon" aria-label={t("pr.open_link")} title={t("pr.open_link")} onClick={() => askLink(c.url ?? "")}>
                        <Cod name="link-external" />
                      </button>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </section>
          <section class="pr-sec" aria-label={t("pr.files")}>
            <h2 class="pr-sec-title">{t("pr.files_n", { n: data.files.length })}</h2>
            <ul class="pr-files">
              {data.files.map((f) => (
                <li key={f.path} class="pr-file">
                  <span class="pr-file-path" dir="auto">
                    {f.path}
                  </span>
                  <span class="gg-file-n" dir="ltr">
                    {f.add > 0 && <span class="gg-add">+{f.add}</span>}
                    {f.del > 0 && <span class="gg-del">−{f.del}</span>}
                  </span>
                </li>
              ))}
            </ul>
          </section>
          {data.reviews.length > 0 && (
            <section class="pr-sec" aria-label={t("pr.reviews")}>
              <h2 class="pr-sec-title">{t("pr.reviews")}</h2>
              {data.reviews.map((r, i) => (
                <article key={i} class="pr-rev">
                  <p class="pr-rev-head">
                    <Cod name={r.state === "APPROVED" ? "pass" : r.state === "CHANGES_REQUESTED" ? "request-changes" : "comment"} />
                    <span class="pr-rev-who" dir="auto">
                      {r.author}
                    </span>
                    {r.when && <span class="pr-rev-when">{new Date(r.when).toLocaleString()}</span>}
                  </p>
                  {r.body && r.body.c?.length ? (
                    <div class="pr-md">
                      <Md doc={r.body} />
                    </div>
                  ) : null}
                </article>
              ))}
            </section>
          )}
        </div>
      )}
      {ask && (
        <AskLaptop
          label={ask.k === "merge" ? t("pr.merge") : ask.k === "checkout" ? t("pr.checkout") : t("pr.send_review")}
          text={askText(ask)}
          send={() => send(ask)}
          onDone={() => setAsk(null)}
          onConfirmed={() => setAgain((x) => x + 1)}
        />
      )}
    </View>
  );
}
