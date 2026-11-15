// A file's diff as a full page (#/git/<path>, ?side=staged; page design
// 08): back, the file name with +N −N, ⋯ (the other side, Open file,
// Discard changes…); the file's header folds and has a round "viewed"
// tick; each part (hunk) starts with its @@ line, the rows carry both
// line numbers; "Show next lines" reads the next page. A floating pill
// moves to the previous or next part (n / N), stages (or unstages) the
// whole file and asks the AI about it. A long press on a part: Ask AI
// about this, Add to chat, Stage this part, Discard this part.
import type { DiffLine, DiffPage as Page, GitFile } from "../state/git";
import { readDiff, splitPath } from "../state/git";
import { H, h } from "./jsx";
import { s } from "./strings";
import { Btn, chatItems, hold, Icon, Menu, type At, type MenuItem } from "./ui";

type Load = { state: "loading" } | { state: "error"; msg: string } | ({ state: "ok" } & Page);

/** Part is one hunk: its @@ row and the rows under it. */
interface Part {
  head: DiffLine;
  rows: DiffLine[];
}

function parts(rows: DiffLine[]): { lead: DiffLine[]; parts: Part[] } {
  const lead: DiffLine[] = [];
  const out: Part[] = [];
  for (const r of rows) {
    if (r.k === "hunk") out.push({ head: r, rows: [] });
    else if (out.length) out[out.length - 1].rows.push(r);
    else if (r.k !== "file") lead.push(r);
  }
  return { lead, parts: out };
}

/** span says which lines a part changes: "lines 27–29 −1 +1". */
function span(p: Part): string {
  const lines = p.rows.filter((r) => r.k !== "ctx").map((r) => r.new || r.old).filter((n) => n > 0);
  const add = p.rows.filter((r) => r.k === "add").length, del = p.rows.filter((r) => r.k === "del").length;
  const from = lines.length ? Math.min(...lines) : p.head.new, to = lines.length ? Math.max(...lines) : from;
  return s("part_lines", { lines: from === to ? String(from) : `${from}–${to}`, del, add });
}

export function DiffPage({
  path,
  file,
  staged,
  canAct,
  onSide,
  onDiscard,
}: {
  path: string;
  file: GitFile | null;
  staged: boolean;
  canAct: boolean;
  onSide: (staged: boolean) => void;
  onDiscard: () => void;
}) {
  const [load, setLoad] = H.useState<Load>({ state: "loading" });
  const [more, setMore] = H.useState(false);
  const [round, setRound] = H.useState(0);
  const [folded, setFolded] = H.useState(false);
  const [viewed, setViewed] = H.useState(false);
  const [at, setAt] = H.useState(0);
  const [menu, setMenu] = H.useState<{ at: At; title: string; items: MenuItem[] } | null>(null);
  const [ask, setAsk] = H.useState<string | null>(null);
  const body = H.useRef<HTMLDivElement | null>(null);
  const status = file ? file.x + file.y : "";
  const { name, dir } = splitPath(path);
  const fetchPage = (from: number) => H.link.request("git.diff", { path, ...(staged ? { staged } : {}), ...(from > 0 ? { from } : {}) }).then(readDiff);

  H.useEffect(() => {
    if (!file || !H.live.value) return;
    let stop = false;
    setLoad({ state: "loading" });
    fetchPage(0).then(
      (p) => !stop && setLoad({ state: "ok", ...p }),
      (e: unknown) => !stop && setLoad({ state: "error", msg: e instanceof Error ? e.message : "" }),
    );
    return () => {
      stop = true;
    };
  }, [path, staged, status, H.live.value, round]);

  const back = () => H.go({ tab: "git", rest: "", query: {} });
  const loadMore = () => {
    if (load.state !== "ok" || !load.next || more) return;
    setMore(true);
    fetchPage(load.next)
      .then((p) => setLoad({ ...load, rows: [...load.rows, ...p.rows], next: p.next }), H.failed)
      .finally(() => setMore(false));
  };
  const hunkOp = (op: string, header: string) =>
    H.link
      .request(op, { path, hunk: header })
      .catch(H.failed)
      .finally(() => setRound((r) => r + 1));
  const { lead, parts: ps } = load.state === "ok" ? parts(load.rows) : { lead: [], parts: [] };
  const hunkAction = canAct && file !== null && !file.untracked && !file.conflict;
  const go = (i: number) => {
    const n = Math.max(0, Math.min(ps.length - 1, i));
    setAt(n);
    body.current?.querySelectorAll<HTMLElement>(".gp-hunk")[n]?.scrollIntoView({ block: "start", behavior: "smooth" });
  };
  const partMenu = (p: Part, where: At) =>
    setMenu({
      at: where,
      title: span(p),
      items: [
        ...chatItems({ kind: "diff", path }, `${name} ${span(p)}`),
        { label: s(staged ? "unstage_part" : "stage_part"), icon: "check", run: () => hunkOp(staged ? "git.unstage_hunk" : "git.stage_hunk", p.head.text), hidden: !hunkAction },
        { label: s("discard_part"), icon: "trash", kind: "danger", run: () => setAsk(p.head.text), hidden: !hunkAction || staged },
      ],
    });
  const pageMenu = (where: At) =>
    setMenu({
      at: where,
      title: path,
      items: [
        ...chatItems({ kind: "diff", path }, name),
        { label: s(staged ? "show_unstaged" : "show_staged"), icon: "plusminus", run: () => onSide(!staged), hidden: !(file?.staged && file.unstaged) },
        { label: s("open_file"), icon: "link", run: () => H.go({ tab: "files", rest: path, query: {} }) },
        { label: H.t("git.discard"), icon: "trash", kind: "danger", run: onDiscard, hidden: !canAct || staged || !(file?.unstaged || file?.untracked) },
      ],
    });

  let content;
  if (!file) content = <p class="gp-hint">{H.t("git.gone")}</p>;
  else if (load.state === "loading") content = <p class="gp-hint">{H.t("loading")}</p>;
  else if (load.state === "error") content = <p class="gp-hint">{load.msg}</p>;
  else if (load.binary) content = <p class="gp-hint">{H.t("file.binary")}</p>;
  else
    content = (
      <div class="gp-diff" dir="ltr">
        {lead.map((r, i) => (
          <Row key={"l" + i} r={r} />
        ))}
        {ps.map((p, i) => (
          <Hunk key={i + p.head.text} p={p} onMenu={(where) => partMenu(p, where)} />
        ))}
        {load.next > 0 && (
          <button type="button" class="gp-more" data-act="more-rows" disabled={more} onClick={loadMore}>
            <Icon n="downarrow" size={15} />
            {s("next_lines")}
          </button>
        )}
        {load.truncated && load.next === 0 && <p class="gp-hint">{H.t("git.long")}</p>}
      </div>
    );

  const counts = load.state === "ok" && (
    <span class="gp-counts" dir="ltr">
      <span class="gp-add">+{load.added}</span> <span class="gp-del">−{load.removed}</span>
    </span>
  );
  return (
    <H.View
      tab="git"
      name="git-diff"
      title={name}
      sub={counts || undefined}
      back={{ label: H.t("back"), onClick: back }}
      actions={
        <Btn
          icon="more"
          act="file-more"
          label={s("more")}
          onClick={(e) => {
            const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
            pageMenu({ x: r.right - 120, y: r.bottom - 6 });
          }}
        />
      }
      bodyClass="gp-body gp-diff-body"
      bodyRef={body}
    >
      <div class={`gp-file${folded ? " is-folded" : ""}`}>
        <div class="gp-file-h">
          <button type="button" class="gp-fold" data-act="fold" aria-expanded={folded ? "false" : "true"} onClick={() => setFolded(!folded)}>
            <Icon n={folded ? "right" : "down"} size={15} />
            <span class="gp-file-p" dir="ltr">
              {dir ? dir + "/" + name : name}
              {staged && <span class="gp-tag gp-side">{H.t("git.staged")}</span>}
            </span>
          </button>
          <button
            type="button"
            role="checkbox"
            class={`gp-circ${viewed ? " is-on" : ""}`}
            data-act="viewed"
            aria-checked={viewed ? "true" : "false"}
            aria-label={s("viewed")}
            onClick={() => (setViewed(!viewed), setFolded(!viewed))}
          >
            {viewed && <Icon n="check" size={14} />}
          </button>
        </div>
        {!folded && content}
      </div>
      {file && load.state === "ok" && (
        <div class="gp-fpill" role="toolbar" aria-label={name}>
          <button type="button" class="gp-fp-b" data-act="prev" aria-label={s("prev_part")} disabled={at <= 0} onClick={() => go(at - 1)}>
            <Icon n="up" size={18} />
          </button>
          <button type="button" class="gp-fp-b" data-act="next" aria-label={s("next_part")} disabled={at >= ps.length - 1} onClick={() => go(at + 1)}>
            <Icon n="down" size={18} />
          </button>
          <span class="gp-fp-n" aria-live="polite">
            {ps.length ? `${at + 1} / ${ps.length}` : "0 / 0"}
          </span>
          {canAct && !file.conflict && (
            <button type="button" class="gp-fp-stage" data-act="stage-file" onClick={() => H.stage(path, !staged)}>
              <Icon n="check" size={15} />
              {s(staged ? "unstage_file" : "stage_file")}
            </button>
          )}
          <button type="button" class="gp-fp-b gp-fp-ai" data-act="ask" aria-label={s("ask")} onClick={() => chatItems({ kind: "diff", path }, name)[0].run()}>
            <Icon n="spark" size={18} />
          </button>
        </div>
      )}
      {menu && <Menu {...menu} onClose={() => setMenu(null)} />}
      {ask && (
        <H.AskLaptop
          cls="discard"
          label={s("discard_part")}
          text={s("discard_part_q", { path })}
          send={() => H.link.request("git.discard_hunk", { path, hunk: ask })}
          onConfirmed={() => setRound((r) => r + 1)}
          onDone={() => setAsk(null)}
        />
      )}
    </H.View>
  );
}

function Row({ r }: { r: DiffLine }) {
  return (
    <div class={`gp-dl is-${r.k}`}>
      <span class="gp-ln">{r.old || ""}</span>
      <span class="gp-ln">{r.new || ""}</span>
      <span class="gp-mk">{r.k === "add" ? "+" : r.k === "del" ? "−" : ""}</span>
      <span class="gp-code">{r.text}</span>
    </div>
  );
}

function Hunk({ p, onMenu }: { p: Part; onMenu: (at: At) => void }) {
  const cb = H.useRef(onMenu);
  cb.current = onMenu;
  const press = H.useMemo(() => hold((at) => cb.current(at)), []);
  return (
    <div class="gp-hunk" {...press}>
      <div class="gp-dl is-hunk">
        <span class="gp-code">{p.head.text}</span>
      </div>
      {p.rows.map((r, i) => (
        <Row key={i} r={r} />
      ))}
    </div>
  );
}
