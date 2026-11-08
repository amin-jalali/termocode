// The hosts tree on the phone (More › Containers and More › Databases),
// as the laptop draws it: Show the hosts, then each host "name · where"
// opens to its compose projects and containers, read once when it first
// opens (Refresh reads them again); a host that does not answer shows its
// reason under its own row. In Databases only the ssh:// hosts and their
// database containers show, and tapping one adds it to Databases. + Add
// Server… takes a name and ssh://user@host, then asks in the card
// (Cancel first and focused).
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { useLayoutEffect, useRef, useState } from "preact/hooks";
import { toast } from "../../lib/toasts";
import { addServer, errLine, forgetHosts, groups, hostCtrs, hostLabel, hosts, loadHosts, NAME, openHost, pickDB, SSH_HOST, type HostCtr } from "../../state/hosts";
import { t } from "../../strings";
import { Button } from "../../ui/Button";
import { SkeletonRows } from "../../ui/Skeleton";

/** shown: the phone's hosts list is open; open: its hosts shown open. */
const shown = signal(false);
const open = signal<ReadonlySet<string>>(new Set());

function flip(name: string): void {
  const n = new Set(open.peek());
  if (n.has(name)) n.delete(name);
  else {
    n.add(name);
    openHost(name);
  }
  open.value = n;
}

function AddServer({ canAct }: { canAct: boolean }) {
  const [step, setStep] = useState<"" | "form" | "ask">("");
  const [name, setName] = useState("");
  const [host, setHost] = useState("ssh://");
  const [err, setErr] = useState("");
  const first = useRef<HTMLInputElement | null>(null);
  const safe = useRef<HTMLButtonElement | null>(null);
  useLayoutEffect(() => {
    if (step === "form") first.current?.focus();
    if (step === "ask") safe.current?.focus();
  }, [step]);
  if (!canAct) return null;
  if (step === "") {
    return (
      <div class="choices">
        <Button small needsLink onClick={() => (setErr(""), setStep("form"))}>
          {t("srv.add")}
        </Button>
      </div>
    );
  }
  const next = () => {
    const n = name.trim();
    const hst = host.trim();
    if (!NAME.test(n)) return setErr(t("srv.bad_name"));
    if (!SSH_HOST.test(hst)) return setErr(t("srv.bad_host"));
    setErr("");
    setStep("ask");
  };
  const done = () => {
    setStep("");
    setName("");
    setHost("ssh://");
  };
  if (step === "ask") {
    const q = t("srv.add_q", { name: name.trim(), host: host.trim() });
    return (
      <div class="row-ask srv-add" role="group" aria-label={q}>
        <p class="row-ask-text" dir="auto">
          {q}
        </p>
        <div class="choices">
          <button type="button" class="btn btn-neutral" ref={safe} onClick={() => setStep("form")}>
            {t("cancel")}
          </button>
          <Button
            kind="primary"
            needsLink
            onClick={() => {
              const n = name.trim();
              addServer(n, host.trim()).then(
                () => {
                  toast("ok", t("srv.added", { name: n }));
                  done();
                  loadHosts();
                },
                (e: unknown) => setErr(errLine(e)),
              );
            }}
          >
            {t("srv.add_yes")}
          </Button>
        </div>
        {err && <p class="db-note">{err}</p>}
      </div>
    );
  }
  return (
    <form
      class="row-ask srv-add"
      onSubmit={(e) => {
        e.preventDefault();
        next();
      }}
    >
      <input ref={first} class="field-box" dir="ltr" aria-label={t("srv.add_name")} placeholder={t("srv.add_name")} value={name} spellcheck={false} autocapitalize="off" onInput={(e) => setName((e.currentTarget as HTMLInputElement).value)} />
      <input class="field-box" dir="ltr" aria-label={t("srv.add_host")} placeholder={t("srv.add_host")} value={host} spellcheck={false} autocapitalize="off" onInput={(e) => setHost((e.currentTarget as HTMLInputElement).value)} />
      {err && <p class="db-note">{err}</p>}
      <div class="choices">
        <button type="button" class="btn btn-neutral" onClick={done}>
          {t("cancel")}
        </button>
        <button type="submit" class="btn btn-primary">
          {t("srv.add_yes")}
        </button>
      </div>
    </form>
  );
}

function Ctr({ host, c, db, canAct, onPicked }: { host: string; c: HostCtr; db: boolean; canAct: boolean; onPicked: (conn: string) => void }) {
  const add = () =>
    pickDB(host, c.id).then(
      (r) => {
        toast("ok", t("srv.db_added", { name: r.name || c.name }));
        onPicked(r.conn);
      },
      (e: unknown) => toast("warn", errLine(e)),
    );
  if (db)
    return (
      <button type="button" class="db-conn" data-host-ctr={c.id} disabled={!canAct} onClick={add}>
        <span class="db-name" dir="ltr">
          {c.name}
        </span>
        <span class="db-meta" dir="ltr">
          {c.db} · {c.status}
        </span>
      </button>
    );
  return (
    <div class="db-conn" data-host-ctr={c.id}>
      <span class="db-name" dir="ltr">
        {c.name}
      </span>
      <span class="db-meta" dir="ltr">
        {c.image} · {c.status}
      </span>
      {c.db && canAct && (
        <Button small needsLink onClick={add}>
          {t("srv.open_db")}
        </Button>
      )}
    </div>
  );
}

/** PhoneHosts is the hosts group of a phone view (db: Databases). */
export function PhoneHosts({ db, canAct, onPicked }: { db: boolean; canAct: boolean; onPicked: (conn: string) => void }) {
  const hl = hosts.value;
  const title = t(db ? "srv.servers" : "srv.hosts");
  const toggle = () => {
    shown.value = !shown.peek();
    if (shown.peek() && (!hosts.peek() || hosts.peek()?.s === "err")) loadHosts();
  };
  const list = hl?.s === "ok" ? hl.v.filter((x) => !db || x.ssh) : [];
  return (
    <section class="group srv-hosts" aria-labelledby={db ? "srv-db" : "srv-ctr"}>
      <div class="ctrs-head">
        <h2 class="group-title" id={db ? "srv-db" : "srv-ctr"}>
          {title}
        </h2>
        <Button small pressed={shown.value} onClick={toggle}>
          {shown.value ? t("srv.hide") : t("srv.show")}
        </Button>
      </div>
      {shown.value && (
        <>
          {(!hl || hl.s === "loading") && <SkeletonRows rows={2} />}
          {hl?.s === "err" && <p class="empty refused">{hl.msg}</p>}
          {hl?.s === "ok" && list.length === 0 && <p class="empty">{t("srv.no_hosts")}</p>}
          {list.length > 0 && (
            <div class="db-list">
              {list.map((x) => {
                const isOpen = open.value.has(x.name);
                const l = hostCtrs.value.get(x.name);
                const cs = l?.s === "ok" ? (db ? l.v.filter((c) => c.db) : l.v) : [];
                return (
                  <Fragment key={x.name}>
                    <button type="button" class="db-conn srv-host" data-host={x.name} aria-expanded={isOpen ? "true" : "false"} onClick={() => flip(x.name)}>
                      <span class="db-name" dir="ltr">
                        {hostLabel(x)}
                      </span>
                    </button>
                    {isOpen && (!l || l.s === "loading") && <p class="db-conn db-meta">{t("loading")}</p>}
                    {isOpen && l?.s === "err" && (
                      <p class="db-conn db-note" data-host-err={x.name} dir="auto">
                        {l.msg}
                      </p>
                    )}
                    {isOpen && l?.s === "ok" && cs.length === 0 && <p class="db-conn db-meta">{t(db ? "srv.no_db" : "ctr.none")}</p>}
                    {isOpen &&
                      groups(cs).map(([proj, items]) => (
                        <Fragment key={proj}>
                          <p class="db-conn srv-project" data-host-project={x.name + "/" + proj} dir="ltr">
                            {proj}
                          </p>
                          {items.map((c) => (
                            <Ctr key={c.id} host={x.name} c={c} db={db} canAct={canAct} onPicked={onPicked} />
                          ))}
                        </Fragment>
                      ))}
                  </Fragment>
                );
              })}
            </div>
          )}
          {hl?.s === "ok" && (
            <div class="choices">
              <Button small needsLink onClick={() => (loadHosts(), forgetHosts(open.peek()))}>
                {t("ctr.refresh")}
              </Button>
            </div>
          )}
          {hl?.s === "ok" && <AddServer canAct={canAct} />}
        </>
      )}
    </section>
  );
}
