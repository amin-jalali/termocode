// The Review page of now.js (#/home/review/<n>): one pull request in one
// fixed order: the risk band with "i of N", the question with its task,
// the solution, the changes with the main change first, one proof row
// per check with the commit it ran on (a stale row says "old code" and
// offers Run again; a red one ✦ Fix and Log), the risks and one short
// suggestion. Open, Skip, Comment and Approve & next sit at the bottom
// (keys o, s, c, a). Approve & next opens the next one at once and sends
// the approval only after a 4 s Undo, for the commit the user saw; when
// new code came in the laptop refuses it and the page says so. Merging
// stays the user's own step: nothing else is sent.
import type { ReviewQueue, ReviewSnap } from "../../../../webproto/proto";
import { draft, go, Head, req } from "./kit";
import { Fragment, H, h } from "./jsx";
import { s, type NowKey } from "./strings";

/** UNDO_MS is how long an approval waits for Undo before it is sent. */
export const UNDO_MS = 4000;

interface Pending {
  n: number;
  i: number;
  head: string;
  snap: ReviewSnap;
  timer: ReturnType<typeof setTimeout>;
}

const short = (sha: string) => sha.slice(0, 7);

function ProofTail({ p, n }: { p: ReviewSnap["proof"][number]; n: number }) {
  switch (p.state) {
    case "fail":
      return (
        <span class="rv-ptail">
          <button type="button" class="rv-link rv-fix" onClick={() => draft(s("rv_fix_text", { name: p.name, n }))}>
            {s("fix")}
          </button>
          {" · "}
          {p.url ? (
            <a class="rv-link" href={p.url} target="_blank" rel="noopener noreferrer">
              {s("rv_log")}
            </a>
          ) : (
            s("rv_log")
          )}
        </span>
      );
    case "stale":
      return (
        <span class="rv-ptail">
          <span class="rv-sha">{s("rv_old", { sha: p.sha ?? "" })}</span>{" "}
          <button type="button" class="rv-link" onClick={() => draft(s("rv_again_text", { name: p.name, n }))}>
            {s("rv_again")}
          </button>
        </span>
      );
    case "notrun":
      return <span class="rv-ptail rv-sha">{s("rv_notrun")}</span>;
    case "running":
      return <span class="rv-ptail rv-sha">{s("rv_running")}</span>;
  }
  return <span class="rv-ptail rv-sha">{p.sha}</span>;
}

/** Task marks the task id in the question (or adds it after). */
function Task({ text, task }: { text: string; task: string }) {
  const at = task ? text.indexOf(task) : -1;
  if (!task) return <>{text}</>;
  if (at < 0)
    return (
      <>
        {text} <span class="rv-task">{task}</span>
      </>
    );
  return (
    <>
      {text.slice(0, at)}
      <span class="rv-task">{task}</span>
      {text.slice(at + task.length)}
    </>
  );
}

const MARK: Record<string, string> = { ok: "✓", fail: "✗", running: "◷", stale: "◷", notrun: "–" };

function Body({ r, i, n }: { r: ReviewSnap; i: number; n: number }) {
  const notes = r.suggestion.notes ?? 0;
  return (
    <div class="rv-body">
      <div class="rv-band" data-risk={r.risk.level}>
        <p class="rv-risk">{s("rv_band", { risk: s(("risk_" + r.risk.level) as NowKey), i, n })}</p>
        <h2 dir="auto">
          #{r.number} {r.title}
        </h2>
        <p class="rv-meta">{s("rv_meta", { who: r.author, files: r.files, add: r.add, del: r.del, sha: short(r.head) })}</p>
      </div>
      {r.question && (
        <section>
          <h3 class="rv-h">{s("rv_q")}</h3>
          <p dir="auto">
            <Task text={r.question} task={r.task ?? ""} />
          </p>
        </section>
      )}
      <section>
        <h3 class="rv-h">{s("rv_s")}</h3>
        <p dir="auto">{r.solution}</p>
        {r.agent && <p class="rv-meta">{s("rv_agent", { name: r.agent })}</p>}
      </section>
      <section>
        <h3 class="rv-h">{s("rv_c")}</h3>
        <ol class="rv-list">
          {r.groups.map((g, k) => (
            <li key={g.title} class={"rv-group" + (g.main ? " is-main" : "")}>
              <span class="rv-num">{k + 1}</span>
              <span class="rv-gt trunc">{g.title}</span>
              <span class="rv-sha">
                {g.main ? (
                  <>
                    <span class="rv-add">+{g.add}</span> <span class="rv-del">-{g.del}</span>
                  </>
                ) : (
                  s("rv_files", { n: g.files.length }) + " ›"
                )}
              </span>
            </li>
          ))}
        </ol>
      </section>
      <section>
        <h3 class="rv-h">{s("rv_p", { sha: short(r.head) })}</h3>
        <ul class="rv-list">
          {r.proof.map((p) => (
            <li key={p.name} class="rv-proof" data-state={p.state}>
              <span class="rv-mark" aria-hidden="true">
                {MARK[p.state]}
              </span>
              <span class="rv-pname trunc">{p.name}</span>
              <ProofTail p={p} n={r.number} />
            </li>
          ))}
        </ul>
      </section>
      <section>
        <h3 class="rv-h">{s("rv_r")}</h3>
        <ul class="rv-reasons">
          {r.risk.reasons.map((x) => (
            <li key={x}>{x}</li>
          ))}
        </ul>
      </section>
      <section>
        <h3 class="rv-h">{s("rv_sg")}</h3>
        <p>
          {notes > 0 && <span class="rv-note">{s(notes === 1 ? "rv_notes" : "rv_notes_n", { n: notes })}</span>} {r.suggestion.text}
        </p>
      </section>
    </div>
  );
}

/** Review is the page; first is the pull request the route named. */
export function Review({ n: first }: { n: number }) {
  const [queue, setQueue] = H.useState<number[]>([first]);
  const [i, setI] = H.useState(0);
  const [snap, setSnap] = H.useState<ReviewSnap | null>(null);
  const [err, setErr] = H.useState("");
  const [pending, setPending] = H.useState<Pending | null>(null);
  const [moved, setMoved] = H.useState<{ n: number; i: number; text: string } | null>(null);
  const [done, setDone] = H.useState(0);
  const [comment, setComment] = H.useState<string | null>(null);
  const live = H.useRef({ queue, i });
  live.current = { queue, i };

  const load = (k: number, q: number[]) => {
    setI(k);
    setSnap(null);
    setErr("");
    if (k >= q.length) return;
    H.go({ tab: "home", rest: "review/" + q[k], query: {} }, true);
    req("review.get", { number: q[k] }).then(
      (d) => live.current.i === k && setSnap(d as ReviewSnap),
      (e) => setErr(H.errText(e)),
    );
  };
  H.useEffect(() => {
    req("review.list", {}).then(
      (d) => {
        const items = (d as ReviewQueue | undefined)?.items ?? [];
        const q = items.filter((x) => !x.bot && !x.draft).map((x) => x.number);
        const all = q.includes(first) ? q.slice(q.indexOf(first)) : [first, ...q];
        setQueue(all);
        load(0, all);
      },
      () => load(0, [first]),
    );
  }, []);

  const send = (p: { n: number; i: number; head: string }) =>
    req("review.act", { number: p.n, head: p.head, do: "approve" }).then(
      () => {
        setDone((x) => x + 1);
        setPending(null);
      },
      (e) => {
        setPending(null);
        // A refusal says why in the laptop's own words (new code came in).
        setMoved({ n: p.n, i: p.i, text: H.code(e) === "conflict" && e instanceof Error ? e.message : H.errText(e) });
      },
    );
  const next = () => load(live.current.i + 1, live.current.queue);
  const approve = () => {
    if (!snap) return;
    if (pending) {
      clearTimeout(pending.timer);
      void send(pending);
    }
    const p = { n: snap.number, i, head: snap.head, snap };
    setPending({ ...p, timer: setTimeout(() => void send(p), UNDO_MS) });
    next();
  };
  const undo = () => {
    if (!pending) return;
    clearTimeout(pending.timer);
    setPending(null);
    // Back to that one as it was: nothing is asked again.
    setI(pending.i);
    setSnap(pending.snap);
    H.go({ tab: "home", rest: "review/" + pending.n, query: {} }, true);
  };
  const sendComment = (text: string) => {
    if (!snap) return;
    const n = snap.number;
    req("review.act", { number: n, head: snap.head, do: "comment", text }).then(
      () => setComment(null),
      (e) => setErr(H.errText(e)),
    );
    setComment(null);
    next();
  };
  H.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null;
      if (e.ctrlKey || e.metaKey || e.altKey || t?.closest("input, textarea")) return;
      const k = e.key.toLowerCase();
      if (k === "a") approve();
      else if (k === "s") next();
      else if (k === "c") setComment("");
      else if (k === "u" || k === "z") undo();
      else if (k === "o") document.querySelector<HTMLAnchorElement>(".rv-open")?.click();
      else return;
      e.preventDefault();
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  });

  const end = i >= queue.length;
  return (
    <div class="now-page rv-page" role="region" aria-label={s("rv")}>
      <Head title={s("rv")} back="" />
      {end ? (
        <div class="rv-done now-clear">
          <span class="now-check" aria-hidden="true">
            ✓
          </span>
          <strong>{s("rv_done")}</strong>
          <p>{s("rv_done_sub", { n: done + (pending ? 1 : 0), w: 0 })}</p>
          <button type="button" class="btn btn-neutral" onClick={() => go("")}>
            {s("rv_back_now")}
          </button>
        </div>
      ) : snap ? (
        <Body r={snap} i={i + 1} n={queue.length} />
      ) : (
        <p class="rv-wait">{err || s("rv_loading")}</p>
      )}
      {moved && (
        <div class="rv-moved" role="alert">
          <strong>{s("rv_moved_h")}</strong>
          <p>{moved.text.replace(s("rv_moved_h"), "").trim()}</p>
          <div class="now-acts">
            <button
              type="button"
              class="btn btn-primary"
              onClick={() => {
                const m = moved;
                setMoved(null);
                load(m.i, live.current.queue);
              }}
            >
              {s("rv_moved_new")}
            </button>
            <button type="button" class="btn btn-neutral" onClick={() => setMoved(null)}>
              {s("rv_skip")}
            </button>
          </div>
        </div>
      )}
      {pending && (
        <div class="rv-undo" role="status">
          <span>{s("rv_approved", { n: pending.n, sha: short(pending.head) })}</span>
          <button type="button" onClick={undo}>
            {s("undo")}
          </button>
          <span class="rv-bar" style={{ animationDuration: UNDO_MS + "ms" }} />
        </div>
      )}
      {comment !== null && snap && (
        <H.Sheet onLater={() => setComment(null)} label={s("rv_comment")}>
          <form
            class="now-later"
            onSubmit={(e: Event) => {
              e.preventDefault();
              if (comment.trim()) sendComment(comment.trim());
            }}
          >
            <h2>
              {s("rv_comment")} · #{snap.number}
            </h2>
            <textarea class="now-input rv-text" value={comment} placeholder={s("rv_comment_ph")} aria-label={s("rv_comment")} onInput={(e: Event) => setComment((e.currentTarget as HTMLTextAreaElement).value)} />
            <button type="submit" class="btn btn-primary" disabled={!comment.trim()}>
              {s("rv_send")}
            </button>
          </form>
        </H.Sheet>
      )}
      {!end && (
        <footer class="rv-foot">
          <div class="rv-acts">
            <a class="btn btn-neutral rv-open" href={snap?.url || undefined} target="_blank" rel="noopener noreferrer" aria-disabled={snap?.url ? undefined : "true"}>
              {s("rv_open")} <kbd>O</kbd>
            </a>
            <button type="button" class="btn btn-neutral" onClick={next}>
              {s("rv_skip")} <kbd>S</kbd>
            </button>
            <button type="button" class="btn btn-neutral" disabled={!snap} onClick={() => setComment("")}>
              {s("rv_comment")} <kbd>C</kbd>
            </button>
            <button type="button" class="btn btn-primary rv-approve" disabled={!snap} onClick={approve}>
              {s("rv_approve")} <kbd>A</kbd>
            </button>
          </div>
          <p class="rv-cap">{snap ? s("rv_foot") : ""}</p>
        </footer>
      )}
    </div>
  );
}
