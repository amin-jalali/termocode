// The desktop's hosts tree, as the laptop draws it in the Containers
// view (CONTEXTS) and the Databases view (SERVERS): each host "name ·
// where" opens to its compose projects and containers, read once when it
// first opens; a host that does not answer shows its reason under its
// own row. The Databases view shows the ssh:// hosts and their database
// containers only; picking one adds it to Databases. + Add Server… asks
// a name, then ssh://user@host, then in the view (Cancel first).
import { Fragment, h, type ComponentChildren } from "preact";
import type { Signal } from "@preact/signals";
import { useLayoutEffect, useRef, useState } from "preact/hooks";
import { toast } from "../../lib/toasts";
import { addServer, groups, hostCtrs, hostLabel, NAME, openHost, SSH_HOST, errLine, type HostCtr, type HostView } from "../../state/hosts";
import { t, type StringKey } from "../../strings";
import { Cod } from "../../ui/icons";
import { Ask, Row } from "../../ui/pane";

const RUNNING: ReadonlySet<string> = new Set(["running", "restarting"]);

function ctrIcon(c: HostCtr) {
  if (c.db) return <span class={RUNNING.has(c.state) ? "c-ok" : "c-err"}><Cod name="database" /></span>;
  if (RUNNING.has(c.state)) return <span class="c-ok"><Cod name="vm-running" /></span>;
  return <span class="c-err"><Cod name="vm-outline" /></span>;
}

/** flipHost opens or closes a host row; the first open reads it. */
export function flipHost(open: Signal<ReadonlySet<string>>, name: string): void {
  const n = new Set(open.peek());
  if (n.has(name)) n.delete(name);
  else {
    n.add(name);
    openHost(name);
  }
  open.value = n;
}

interface Props {
  h: HostView;
  open: Signal<ReadonlySet<string>>;
  /** db: only database containers, each picked with onPick. */
  db?: boolean;
  level?: number;
  icon?: ComponentChildren;
  desc?: string;
  title?: string;
  actions?: ComponentChildren;
  /** under is drawn right under the host row (a question). */
  under?: ComponentChildren;
  ctrActions?: (c: HostCtr) => ComponentChildren;
  onPick?: (c: HostCtr) => void;
}

/** HostRows is one host and, when open, its projects and containers. */
export function HostRows(p: Props) {
  const isOpen = p.open.value.has(p.h.name);
  const l = hostCtrs.value.get(p.h.name);
  const lv = p.level ?? 0;
  const shown = l?.s === "ok" ? (p.db ? l.v.filter((c) => c.db) : l.v) : [];
  return (
    <>
      <Row
        level={lv}
        twist={isOpen ? "open" : "closed"}
        onToggle={() => flipHost(p.open, p.h.name)}
        icon={p.icon ?? <Cod name={p.h.local ? "vm" : "remote"} />}
        label={hostLabel(p.h)}
        desc={p.desc}
        title={p.title ?? hostLabel(p.h)}
        data={{ context: p.h.name, host: p.h.name }}
        actions={p.actions}
      />
      {p.under}
      {isOpen && (!l || l.s === "loading") && <p class="tr-note">{t("loading")}</p>}
      {isOpen && l?.s === "err" && (
        <p class="tr-note is-err" data-host-err={p.h.name}>
          {l.msg}
        </p>
      )}
      {isOpen && l?.s === "ok" && shown.length === 0 && <p class="tr-note">{t(p.db ? "srv.no_db" : "ctr.none")}</p>}
      {isOpen &&
        l?.s === "ok" &&
        groups(shown).map(([proj, cs]) => (
          <Fragment key={proj}>
            <Row level={lv + 1} twist={null} icon={<Cod name="package" />} label={proj} data={{ "host-project": p.h.name + "/" + proj }} end={<span class="count">{cs.length}</span>} />
            {cs.map((c) => (
              <Row
                key={c.id}
                level={lv + 2}
                twist={null}
                icon={ctrIcon(c)}
                label={c.name}
                desc={c.image}
                title={[c.name, c.image, c.status].filter(Boolean).join(" — ")}
                data={{ "host-ctr": c.id, db: c.db }}
                onClick={p.onPick ? () => p.onPick?.(c) : undefined}
                end={<span class="tr-age">{c.status}</span>}
                actions={p.ctrActions?.(c)}
              />
            ))}
          </Fragment>
        ))}
    </>
  );
}

function Field({ place, value, ok, no }: { place: StringKey; value: string; ok: (v: string) => void; no: () => void }) {
  const box = useRef<HTMLInputElement | null>(null);
  const [v, setV] = useState(value);
  useLayoutEffect(() => box.current?.focus(), []);
  return (
    <div class="pv-input ctr-form" data-form="add-server">
      <input
        ref={box}
        dir="ltr"
        aria-label={t(place)}
        placeholder={t(place)}
        value={v}
        spellcheck={false}
        autocapitalize="off"
        onInput={(e) => setV((e.target as HTMLInputElement).value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            e.stopPropagation();
            no();
          } else if (e.key === "Enter") e.preventDefault();
        }}
        // On key up: the question that comes next takes the focus, and
        // the same Enter must not answer it.
        onKeyUp={(e) => e.key === "Enter" && v.trim() && ok(v.trim())}
      />
    </div>
  );
}

/** AddServerRow is + Add Server…: a name, then ssh://user@host, then a
 * question in the view (Cancel first); added calls onAdded. */
export function AddServerRow({ level, canAct, onAdded }: { level?: number; canAct: boolean; onAdded: () => void }) {
  const [step, setStep] = useState<{ s: "" } | { s: "name" } | { s: "host"; name: string } | { s: "ask"; name: string; host: string }>({ s: "" });
  const [err, setErr] = useState("");
  const close = () => (setStep({ s: "" }), setErr(""));
  return (
    <>
      <Row
        level={level ?? 0}
        twist={null}
        icon={<Cod name="add" />}
        label={t("srv.add")}
        labelClass="c-dim"
        title={t("srv.add_title")}
        data={{ "add-server": "1" }}
        onClick={() => canAct && (setErr(""), setStep({ s: "name" }))}
      />
      {step.s === "name" && (
        <Field
          place="srv.add_name"
          value=""
          no={close}
          ok={(v) => (NAME.test(v) ? (setErr(""), setStep({ s: "host", name: v })) : setErr(t("srv.bad_name")))}
        />
      )}
      {step.s === "host" && (
        <Field
          key="host"
          place="srv.add_host"
          value="ssh://"
          no={close}
          ok={(v) => (SSH_HOST.test(v) ? (setErr(""), setStep({ s: "ask", name: step.name, host: v })) : setErr(t("srv.bad_host")))}
        />
      )}
      {err && <p class="tr-note is-err">{err}</p>}
      {step.s === "ask" && (
        <Ask
          text={t("srv.add_q", { name: step.name, host: step.host })}
          verb={t("srv.add_yes")}
          onNo={close}
          onYes={() => {
            const { name, host } = step;
            close();
            addServer(name, host).then(
              () => {
                toast("ok", t("srv.added", { name }));
                onAdded();
              },
              (e: unknown) => toast("warn", errLine(e)),
            );
          }}
        />
      )}
    </>
  );
}
