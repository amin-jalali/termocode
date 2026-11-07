// The desktop's Containers view, a tree like the laptop editor's
// container tools: CONTAINERS (grouped by compose project, each with its
// state's icon, image and status), IMAGES, NETWORKS, VOLUMES, CONTEXTS
// and REGISTRIES, each folding, with actions on hover. What only reads
// or starts asks here first (Cancel first and focused) or runs at once:
// logs (the panel's OUTPUT tab), inspect, history, stats, disk usage,
// search and tags (editor tabs), start, restart, pause, tag, create,
// compose up / restart, the session's context. What ends work, removes,
// sends or runs something asks the laptop: stop, remove, a shell or
// attach, pull, push, run, every prune, compose stop and down, the
// default context, sign in and out (the password is typed there), Open
// in Databases. The filter narrows every section's rows.
import { Fragment, h, type ComponentChildren } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import type { ContainersHost, ImageItem } from "../../../../../webproto/proto";
import { panelTab } from "../../lib/lsp";
import { copyText } from "../../lib/clip";
import { toast } from "../../lib/toasts";
import { ctrTab, type CtrWhat } from "../../lib/vtabs";
import { setPanes, showFile } from "../../lib/workbench";
import { link, live } from "../../session";
import { readContainers, type ContainersView, type CtrView } from "../../state/more";
import { t, type StringKey } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { errText } from "../../ui/CardSheet";
import { onMenu, type MenuEntry } from "../../ui/ContextMenu";
import { Cod } from "../../ui/icons";
import { Act, Ask, PaneSection, PaneView, Row, Tree } from "../../ui/pane";
import { Portal } from "../../ui/Portal";
import { gitFailed as failed } from "../git/errors";

type Load<T> = { s: "loading" } | { s: "ok"; v: T } | { s: "err"; msg: string };

const list = signal<Load<ContainersView> | null>(null);
const host = signal<Load<ContainersHost> | null>(null);
const shut = signal<ReadonlySet<string>>(new Set(["#networks", "#volumes", "#contexts", "#registries"]));

/** filter narrows the rows (Containers: Filter Rows…); null: no box. */
export const ctrFilter = signal<string | null>(null);
/** ctrSel is the container clicked last (Open Published Port acts on it). */
export const ctrSel = signal<CtrView | null>(null);

/** Logs is what the OUTPUT panel shows: one container's last lines. */
export interface Logs {
  id: string;
  name: string;
  lines: string[];
  redacted: number;
  truncated: boolean;
  loading: boolean;
  err: string;
}
export const logs = signal<Logs | null>(null);

function reload(): void {
  if (!list.peek()) list.value = { s: "loading" };
  link.request("containers.list", {}).then(
    (r) => (list.value = { s: "ok", v: readContainers(r) }),
    (e: unknown) => (list.value = { s: "err", msg: errText(e) }),
  );
}

function reloadHost(): void {
  if (!host.peek()) host.value = { s: "loading" };
  link.request("containers.host", {}).then(
    (r) => (host.value = { s: "ok", v: r as ContainersHost }),
    (e: unknown) => (host.value = { s: "err", msg: errText(e) }),
  );
}

function reloadAll(): void {
  reload();
  reloadHost();
}

/** showLogs reads a container's log tail into the panel's OUTPUT tab. */
export function showLogs(c: { id: string; name: string }): void {
  logs.value = { id: c.id, name: c.name, lines: [], redacted: 0, truncated: false, loading: true, err: "" };
  panelTab.value = "output";
  setPanes({ panel: true });
  link.request("containers.logs", { id: c.id }).then(
    (r) => {
      const o = typeof r === "object" && r !== null ? (r as Record<string, unknown>) : {};
      if (logs.peek()?.id !== c.id) return;
      logs.value = {
        id: c.id,
        name: c.name,
        lines: Array.isArray(o.lines) ? o.lines.filter((x): x is string => typeof x === "string") : [],
        redacted: typeof o.redacted === "number" ? o.redacted : 0,
        truncated: o.truncated === true,
        loading: false,
        err: "",
      };
    },
    (e: unknown) => {
      if (logs.peek()?.id === c.id) logs.value = { ...c, lines: [], redacted: 0, truncated: false, loading: false, err: errText(e) };
    },
  );
}

/** portURL is a container's first published port on the laptop's host
 * name (the page's), or null. */
export function portURL(c: CtrView | null): string | null {
  const p = c ? /:(\d+)->/.exec(c.ports) : null;
  if (!p) return null;
  const u = new URL(location.href);
  u.protocol = "http:";
  u.port = p[1];
  u.pathname = "/";
  u.search = u.hash = "";
  return u.href;
}

/** openPort names where a container's published port is (the row's link
 * opens it). */
export function openPort(c: CtrView | null): void {
  const url = portURL(c);
  toast("info", url ? t("ctr.port_at", { url }) : t("ctr.no_port"));
}

/** PortLink is a row action that opens the published port in a new tab. */
function PortLink({ c }: { c: CtrView }) {
  const url = portURL(c);
  if (!url) return null;
  return (
    <a class="act" data-act="port" href={url} target="_blank" rel="noopener noreferrer" aria-label={t("ctr.open_port")} title={url} onClick={(e) => e.stopPropagation()}>
      <Cod name="link-external" />
    </a>
  );
}

/** openTab opens a container read in an editor tab. */
const openTab = (what: CtrWhat, arg: string, label: string) => showFile(ctrTab(what, arg, label), true);

const RUNNING: ReadonlySet<string> = new Set(["running", "restarting"]);
const DB_IMAGE = /(^|\/)(postgres|postgis|mysql|mariadb|redis|valkey)([:@]|$)/;

function icon(c: CtrView) {
  if (c.busy) return <Cod name="loading" />;
  if (RUNNING.has(c.state)) return <span class="c-ok"><Cod name="vm-running" /></span>;
  if (c.state === "paused") return <span class="c-warn"><Cod name="vm-active" /></span>;
  return <span class="c-err"><Cod name="vm-outline" /></span>;
}

/** Pending is an in-page question under a row. */
type Pending = { key: string; text: string; verb: string; run: () => void } | null;
/** Laptop is a question for the laptop: its op and args. */
type Laptop = { label: string; text: string; op: string; args: unknown } | null;
/** Form is an inline text box under a section's head or a row. */
type Form = { key: string; place: StringKey; value: string; ok: (v: string) => void } | null;

interface Ctx {
  canAct: boolean;
  pending: Pending;
  setPending: (p: Pending) => void;
  setLaptop: (l: Laptop) => void;
  form: Form;
  setForm: (f: Form) => void;
}

/** run asks the laptop at once (the page asked first); done toasts. */
function act(op: string, args: unknown, done: string, after = reloadAll): void {
  link.request(op, args).then((r) => {
    const note = (r as { note?: unknown } | null)?.note;
    toast("ok", typeof note === "string" && note ? done + " " + note : done);
    after();
  }, failed);
}

function InlineForm({ f, setForm }: { f: NonNullable<Form>; setForm: (f: Form) => void }) {
  const box = useRef<HTMLInputElement | null>(null);
  const [v, setV] = useState(f.value);
  useLayoutEffect(() => box.current?.focus(), []);
  return (
    <div class="pv-input ctr-form" data-form={f.key}>
      <input
        ref={box}
        dir="ltr"
        aria-label={t(f.place)}
        placeholder={t(f.place)}
        value={v}
        onInput={(e) => setV((e.target as HTMLInputElement).value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            e.stopPropagation();
            setForm(null);
          } else if (e.key === "Enter" && v.trim()) {
            e.preventDefault();
            setForm(null);
            f.ok(v.trim());
          }
        }}
      />
    </div>
  );
}

function Under({ k, x }: { k: string; x: Ctx }) {
  return (
    <>
      {x.form?.key === k && <InlineForm f={x.form} setForm={x.setForm} />}
      {x.pending?.key === k && <Ask text={x.pending.text} verb={x.pending.verb} onYes={() => (x.setPending(null), x.pending?.run())} onNo={() => x.setPending(null)} />}
    </>
  );
}

const shown = (s: string) => {
  const f = ctrFilter.value?.toLowerCase().trim();
  return !f || s.toLowerCase().includes(f);
};

/** Item is one action of a row: a hover button and a context menu entry
 * (menuOnly: the menu alone has it). */
type Item = { icon: string; act?: string; label: string; run: () => void; menuOnly?: boolean } | null;

/** buttons draws a row's hover buttons from its items. */
const buttons = (items: readonly Item[]) => items.map((it) => it && !it.menuOnly && <Act key={it.label} icon={it.icon} act={it.act} label={it.label} onClick={it.run} />);

/** menuOf is a row's right-click handler over its items (none: no menu). */
function menuOf(label: string, items: () => readonly Item[]): ((e: MouseEvent) => void) | undefined {
  return onMenu(label, () => {
    const out: (MenuEntry | null)[] = [];
    for (const it of items()) {
      if (!it) {
        if (out.length && out[out.length - 1]) out.push(null);
      } else out.push({ label: it.label, run: it.run });
    }
    if (out.length && !out[out.length - 1]) out.pop();
    return out;
  });
}

function CtrRows({ cs, level, x }: { cs: CtrView[]; level: number; x: Ctx }) {
  const ask = (c: CtrView, op: "start" | "restart" | "pause") => {
    const paused = c.state === "paused";
    const q: StringKey = op === "start" ? "ctr.start_q" : op === "restart" ? "ctr.restart_q" : paused ? "ctr.unpause_q" : "ctr.pause_q";
    const verb: StringKey = op === "start" ? "ctr.start" : op === "restart" ? "ctr.restart" : paused ? "ctr.unpause" : "runs.pause";
    x.setPending({
      key: c.id,
      text: t(q, { name: c.name }),
      verb: t(verb),
      run: () =>
        link.request("containers." + op, { id: c.id }).then((r) => {
          const st = (r as { state?: unknown } | null)?.state;
          toast("ok", t("ctr.done", { name: c.name, state: typeof st === "string" ? st : "" }));
          reload();
        }, failed),
    });
  };
  const laptop = (c: CtrView, label: StringKey, q: StringKey, op: string, args: unknown) => x.setLaptop({ label: t(label), text: t(q, { name: c.name }), op, args });
  // items are the laptop's keys for a container: what only reads runs at
  // once; Start, Restart and Pause ask in the view; the rest asks the laptop.
  const items = (c: CtrView): Item[] => {
    const running = RUNNING.has(c.state) || c.state === "paused";
    const can = x.canAct && !c.busy;
    const url = portURL(c);
    return [
      { icon: "output", label: t("ctr.logs"), run: () => showLogs(c) },
      { icon: "json", act: "inspect", label: t("ctr.inspect"), run: () => openTab("inspect-container", c.id, c.name) },
      RUNNING.has(c.state) ? { icon: "pulse", act: "stats", label: t("ctr.stats"), run: () => openTab("stats", c.id, c.name) } : null,
      url ? { icon: "link-external", label: t("ctr.open_port"), run: () => void window.open(url, "_blank", "noopener,noreferrer"), menuOnly: true } : null,
      null,
      { icon: "copy", label: t("ctr.copy_id"), run: () => copyText(c.id), menuOnly: true },
      { icon: "copy", label: t("ctr.copy_name"), run: () => copyText(c.name), menuOnly: true },
      null,
      can && c.state === "running" ? { icon: "terminal", act: "shell", label: t("ctr.shell"), run: () => laptop(c, "ctr.shell", "ctr.shell_q", "containers.exec", { id: c.id, kind: "shell" }) } : null,
      can && c.state === "running" ? { icon: "plug", act: "attach", label: t("ctr.attach"), run: () => laptop(c, "ctr.attach", "ctr.attach_q", "containers.exec", { id: c.id, kind: "attach" }) } : null,
      can && DB_IMAGE.test(c.image) ? { icon: "database", act: "open-db", label: t("ctr.open_db"), run: () => laptop(c, "ctr.open_db", "ctr.open_db_q", "containers.open_db", { id: c.id }) } : null,
      null,
      can && running ? { icon: c.state === "paused" ? "debug-continue" : "debug-pause", act: "pause", label: t(c.state === "paused" ? "ctr.unpause" : "runs.pause"), run: () => ask(c, "pause") } : null,
      can && !running ? { icon: "play", label: t("ctr.start"), run: () => ask(c, "start") } : null,
      can && running ? { icon: "debug-restart", label: t("ctr.restart"), run: () => ask(c, "restart") } : null,
      can && running ? { icon: "debug-stop", label: t("ctr.stop"), run: () => laptop(c, "ctr.stop", "ctr.stop_q", "containers.stop", { id: c.id }) } : null,
      can && !running ? { icon: "trash", label: t("ctr.remove"), run: () => laptop(c, "ctr.remove", "ctr.remove_q", "containers.remove", { id: c.id }) } : null,
    ];
  };
  return (
    <>
      {cs
        .filter((c) => shown(c.name + " " + c.image))
        .map((c) => {
          const its = items(c);
          return (
            <Fragment key={c.id}>
              <Row
                level={level}
                icon={icon(c)}
                label={c.name}
                desc={c.image}
                selected={ctrSel.value?.id === c.id}
                title={[c.name, c.image, c.status, c.ports].filter(Boolean).join(" — ")}
                data={{ id: c.id, state: c.state }}
                onClick={() => ((ctrSel.value = c), showLogs(c))}
                onContextMenu={menuOf(c.name, () => items(c))}
                end={<span class="tr-age">{c.status}</span>}
                actions={
                  <>
                    {buttons(its.slice(0, 3))}
                    <PortLink c={c} />
                    {buttons(its.slice(3))}
                  </>
                }
              />
              <Under k={c.id} x={x} />
            </Fragment>
          );
        })}
    </>
  );
}

function ProjectRow({ p, cs, x }: { p: string; cs: CtrView[]; x: Ctx }) {
  const up = (verb: "up" | "restart") =>
    x.setPending({ key: "p:" + p, text: t(verb === "up" ? "ctr.compose_up_q" : "ctr.compose_restart_q", { name: p }), verb: t(verb === "up" ? "ctr.start" : "ctr.restart"), run: () => act("containers.compose", { project: p, verb }, t("ctr.compose_done", { name: p })) });
  const ask = (verb: "stop" | "down") =>
    x.setLaptop({ label: t(verb === "stop" ? "ctr.stop" : "ctr.compose_down"), text: t(verb === "stop" ? "ctr.compose_stop_q" : "ctr.compose_down_q", { name: p }), op: "containers.compose_ask", args: { project: p, verb } });
  const items: Item[] = [
    { icon: "play", act: "compose-up", label: t("ctr.compose_up"), run: () => up("up") },
    { icon: "debug-restart", act: "compose-restart", label: t("ctr.compose_restart"), run: () => up("restart") },
    { icon: "debug-stop", act: "compose-stop", label: t("ctr.compose_stop"), run: () => ask("stop") },
    { icon: "trash", act: "compose-down", label: t("ctr.compose_down"), run: () => ask("down") },
  ];
  return (
    <>
      <Row
        level={0}
        twist={shut.value.has(p) ? "closed" : "open"}
        onToggle={() => flip(p)}
        icon={<Cod name="package" />}
        label={p}
        data={{ project: p }}
        end={<span class="count">{cs.length}</span>}
        onContextMenu={x.canAct ? menuOf(p, () => items) : undefined}
        actions={x.canAct && <>{buttons(items)}</>}
      />
      <Under k={"p:" + p} x={x} />
    </>
  );
}

function flip(k: string): void {
  const n = new Set(shut.peek());
  if (n.has(k)) n.delete(k);
  else n.add(k);
  shut.value = n;
}

/** Section is one folding part of the view with its load state. */
function Section({ id, title, count, actions, load, empty, children }: { id: string; title: string; count?: number; actions?: ComponentChildren; load: Load<unknown> | null; empty: boolean; children: ComponentChildren }) {
  let body: ComponentChildren = children;
  if (!load || load.s === "loading") body = <p class="pv-msg">{t("loading")}</p>;
  else if (load.s === "err") body = <p class="pv-msg c-err">{load.msg}</p>;
  else if (empty) body = <p class="pv-msg">{t("ctr.empty")}</p>;
  return (
    <PaneSection id={id} title={title} open={!shut.value.has("#" + id)} onToggle={() => flip("#" + id)} count={count} actions={actions}>
      {body}
    </PaneSection>
  );
}

const imageName = (im: ImageItem) => im.tags[0] ?? im.id;

function HostSections({ x }: { x: Ctx }) {
  const hl = host.value;
  const v = hl?.s === "ok" ? hl.v : null;
  const prob = (sec: string) => v?.problems?.[sec];
  const load = (sec: string): Load<unknown> | null => (prob(sec) ? { s: "err", msg: prob(sec) ?? "" } : hl);
  const lap = (label: StringKey, text: string, op: string, args: unknown) => x.setLaptop({ label: t(label), text, op, args });
  const prune = (kind: string, q: StringKey) => <Act icon="clear-all" act={"prune-" + kind} label={t("ctr.prune")} onClick={() => lap("ctr.prune", t(q), "containers.prune", { kind })} />;
  const create = (kind: "network" | "volume") => (
    <Act
      icon="add"
      act={"create-" + kind}
      label={t(kind === "network" ? "ctr.network_create" : "ctr.volume_create")}
      onClick={() => {
        if (shut.peek().has("#" + kind + "s")) flip("#" + kind + "s");
        x.setForm({ key: "#" + kind + "s", place: "ctr.name_place", value: "", ok: (name) => act("containers.create", { kind, name }, t("ctr.created_item", { name }), reloadHost) });
      }}
    />
  );
  const images = (v?.images ?? []).filter((im) => shown(im.tags.join(" ") + " " + im.id));
  return (
    <>
      <Section
        id="images"
        title={t("ctr.images")}
        count={v?.images.length}
        load={load("images")}
        empty={images.length === 0}
        actions={
          x.canAct && (
            <>
              <Act icon="cloud-download" act="pull" label={t("ctr.pull")} onClick={() => (shut.peek().has("#images") && flip("#images"), x.setForm({ key: "#images", place: "ctr.ref_place", value: "", ok: (ref) => lap("ctr.pull", t("ctr.pull_q", { ref }), "containers.image", { verb: "pull", ref }) }))} />
              {prune("images", "ctr.prune_images_q")}
            </>
          )
        }
      >
        <Under k="#images" x={x} />
        <Tree label={t("ctr.images")}>
          {images.map((im) => {
            const ref = imageName(im);
            const tagged = im.tags.length > 0;
            return (
              <Fragment key={im.id + ref}>
                <Row
                  icon={<Cod name="layers" />}
                  label={tagged ? ref : t("ctr.untagged")}
                  desc={im.id}
                  data={{ image: ref }}
                  title={[...im.tags, im.id, im.size, im.created].filter(Boolean).join(" — ")}
                  end={<span class="tr-age">{im.size}</span>}
                  actions={
                    <>
                      <Act icon="json" act="inspect" label={t("ctr.inspect")} onClick={() => openTab("inspect-image", ref, ref)} />
                      <Act icon="history" act="history" label={t("ctr.history")} onClick={() => openTab("history", ref, ref)} />
                      {tagged && <Act icon="search" act="tags" label={t("ctr.tags")} onClick={() => openTab("tags", ref.replace(/:[^/:]*$/, ""), ref.replace(/:[^/:]*$/, ""))} />}
                      {x.canAct && (
                        <>
                          <Act icon="play" act="run" label={t("ctr.run")} onClick={() => x.setForm({ key: "i:" + ref, place: "ctr.run_place", value: "", ok: (spec) => runImage(x, ref, spec) })} />
                          {tagged && <Act icon="tag" act="tag" label={t("ctr.tag")} onClick={() => x.setForm({ key: "i:" + ref, place: "ctr.ref_place", value: ref + "-copy", ok: (tag) => act("containers.tag", { ref, tag }, t("ctr.tagged", { ref: tag }), reloadHost) })} />}
                          {tagged && <Act icon="cloud-upload" act="push" label={t("ctr.push")} onClick={() => lap("ctr.push", t("ctr.push_q", { ref }), "containers.image", { verb: "push", ref })} />}
                          {!im.in_use && <Act icon="trash" act="remove" label={t("ctr.remove")} onClick={() => lap("ctr.remove", t("ctr.image_remove_q", { ref }), "containers.image", { verb: "remove", ref })} />}
                        </>
                      )}
                    </>
                  }
                />
                <Under k={"i:" + ref} x={x} />
              </Fragment>
            );
          })}
        </Tree>
      </Section>
      <Section id="networks" title={t("ctr.networks")} count={v?.networks.length} load={load("networks")} empty={(v?.networks.length ?? 0) === 0} actions={x.canAct && <>{create("network")}{prune("networks", "ctr.prune_networks_q")}</>}>
        <Under k="#networks" x={x} />
        <Tree label={t("ctr.networks")}>
          {(v?.networks ?? []).filter((n) => shown(n.name)).map((n) => (
            <Row
              key={n.name}
              icon={<Cod name="globe" />}
              label={n.name}
              desc={n.driver}
              data={{ network: n.name }}
              actions={
                <>
                  <Act icon="json" act="inspect" label={t("ctr.inspect")} onClick={() => openTab("inspect-network", n.name, n.name)} />
                  {x.canAct && !n.built_in && <Act icon="trash" act="remove" label={t("ctr.remove")} onClick={() => lap("ctr.remove", t("ctr.network_remove_q", { name: n.name }), "containers.storage_remove", { kind: "network", name: n.name })} />}
                </>
              }
            />
          ))}
        </Tree>
      </Section>
      <Section
        id="volumes"
        title={t("ctr.volumes")}
        count={v?.volumes.length}
        load={load("volumes")}
        empty={(v?.volumes.length ?? 0) === 0}
        actions={
          <>
            <Act icon="graph" act="disk-usage" label={t("ctr.disk_usage")} onClick={() => openTab("df", "", t("ctr.disk_usage"))} />
            {x.canAct && <>{create("volume")}{prune("volumes", "ctr.prune_volumes_q")}</>}
          </>
        }
      >
        <Under k="#volumes" x={x} />
        <Tree label={t("ctr.volumes")}>
          {(v?.volumes ?? []).filter((o) => shown(o.name)).map((o) => (
            <Row
              key={o.name}
              icon={<Cod name="database" />}
              label={o.name}
              desc={o.driver}
              data={{ volume: o.name }}
              actions={
                <>
                  <Act icon="json" act="inspect" label={t("ctr.inspect")} onClick={() => openTab("inspect-volume", o.name, o.name)} />
                  {x.canAct && <Act icon="trash" act="remove" label={t("ctr.remove")} onClick={() => lap("ctr.remove", t("ctr.volume_remove_q", { name: o.name }), "containers.storage_remove", { kind: "volume", name: o.name })} />}
                </>
              }
            />
          ))}
        </Tree>
      </Section>
      <Section id="contexts" title={t("ctr.contexts")} count={v?.contexts.length} load={load("contexts")} empty={(v?.contexts.length ?? 0) === 0}>
        <Tree label={t("ctr.contexts")}>
          {(v?.contexts ?? []).map((c) => (
            <Fragment key={c.name}>
              <Row
                icon={<Cod name={c.session || (c.current && !v?.contexts.some((o) => o.session)) ? "check" : "server"} />}
                label={c.name}
                desc={c.current ? t("ctr.default") : c.description}
                data={{ context: c.name }}
                actions={
                  x.canAct && (
                    <>
                      {!c.session && (
                        <Act
                          icon="arrow-swap"
                          act="use"
                          label={t("ctr.context_use")}
                          onClick={() => x.setPending({ key: "c:" + c.name, text: t("ctr.context_use_q", { name: c.name }), verb: t("ctr.context_use"), run: () => act("containers.context", { name: c.name }, t("ctr.context_now", { name: c.name })) })}
                        />
                      )}
                      {!c.current && <Act icon="pinned" act="default" label={t("ctr.context_default")} onClick={() => lap("ctr.context_default", t("ctr.context_default_q", { name: c.name }), "containers.context_default", { name: c.name })} />}
                    </>
                  )
                }
              />
              <Under k={"c:" + c.name} x={x} />
            </Fragment>
          ))}
        </Tree>
      </Section>
      <Section
        id="registries"
        title={t("ctr.registries")}
        count={v?.registries.length}
        load={load("registries")}
        empty={false}
        actions={x.canAct && <Act icon="add" act="connect" label={t("ctr.sign_in")} onClick={() => lap("ctr.sign_in", t("ctr.sign_in_q", { host: t("ctr.a_registry") }), "containers.registry", { verb: "connect", host: "" })} />}
      >
        <Tree label={t("ctr.registries")}>
          {(v?.registries ?? []).map((r) => (
            <Row
              key={r.host}
              icon={<Cod name={r.signed_in ? "account" : "cloud"} />}
              label={r.host}
              desc={r.signed_in ? (r.user ? t("ctr.signed_in_as", { user: r.user }) : t("ctr.signed_in")) : ""}
              data={{ registry: r.host }}
              actions={
                x.canAct &&
                (r.signed_in ? (
                  <Act icon="sign-out" act="signout" label={t("ctr.sign_out")} onClick={() => lap("ctr.sign_out", t("ctr.sign_out_q", { host: r.host }), "containers.registry", { verb: "signout", host: r.host })} />
                ) : (
                  <Act icon="sign-in" act="signin" label={t("ctr.sign_in")} onClick={() => lap("ctr.sign_in", t("ctr.sign_in_q", { host: r.host }), "containers.registry", { verb: "connect", host: r.host })} />
                ))
              }
            />
          ))}
        </Tree>
        {(v?.registries.length ?? 0) === 0 && <p class="pv-msg">{t("ctr.no_registries")}</p>}
      </Section>
    </>
  );
}

/** runImage reads "name 8080:80 …" (a name, then ports) and asks the laptop. */
function runImage(x: Ctx, ref: string, spec: string): void {
  const parts = spec.split(/\s+/).filter(Boolean);
  const name = parts[0] && !/^[\d[]/.test(parts[0]) ? parts.shift() : undefined;
  x.setLaptop({ label: t("ctr.run"), text: t("ctr.run_q", { ref }), op: "containers.image", args: { verb: "run", ref, ...(name ? { name } : {}), ...(parts.length ? { ports: parts } : {}) } });
}

/** askSearch opens the search box (Containers: Search Docker Hub…). */
export const ctrSearch = signal(false);

export function DeskContainers() {
  const on = live.value;
  useEffect(() => {
    if (on) reloadAll();
  }, [on]);
  const [pending, setPending] = useState<Pending>(null);
  const [laptop, setLaptop] = useState<Laptop>(null);
  const [form, setForm] = useState<Form>(null);
  const canAct = link.meta.value?.role !== "viewer";
  useEffect(() => {
    if (ctrSearch.value) {
      ctrSearch.value = false;
      setForm({ key: "#search", place: "ctr.search_place", value: "", ok: (term) => openTab("search", term, term) });
    }
  }, [ctrSearch.value]);
  const l = list.value;
  const v = l?.s === "ok" ? l.v : null;
  // Compose projects first (as folders), then the containers of none.
  const projects = new Map<string, CtrView[]>();
  const loose: CtrView[] = [];
  for (const c of v?.containers ?? []) {
    if (c.project) projects.set(c.project, [...(projects.get(c.project) ?? []), c]);
    else loose.push(c);
  }
  const x: Ctx = { canAct, pending, setPending, setLaptop, form, setForm };
  const lap = (label: StringKey, q: StringKey, op: string, args: unknown) => setLaptop({ label: t(label), text: t(q), op, args });
  return (
    <PaneView
      id="containers"
      title={t("more.containers")}
      actions={
        <>
          <Act icon="search" act="search" label={t("ctr.search")} disabled={!on || !canAct} onClick={() => (ctrSearch.value = true)} />
          <Act icon="filter" act="filter" label={t("ctr.filter")} on={ctrFilter.value !== null} onClick={() => (ctrFilter.value = ctrFilter.peek() === null ? "" : null)} />
          {canAct && <Act icon="trash" act="prune-system" label={t("ctr.prune_system")} disabled={!on} onClick={() => lap("ctr.prune_system", "ctr.prune_system_q", "containers.prune", { kind: "system" })} />}
          <Act icon="refresh" label={t("ctr.refresh")} disabled={!on} onClick={reloadAll} />
        </>
      }
    >
      {ctrFilter.value !== null && (
        <div class="pv-input ctr-filter">
          <input
            dir="ltr"
            aria-label={t("ctr.filter")}
            placeholder={t("ctr.filter_place")}
            value={ctrFilter.value}
            ref={(el) => {
              if (el && document.activeElement === document.body) el.focus();
            }}
            onInput={(e) => (ctrFilter.value = (e.target as HTMLInputElement).value)}
            onKeyDown={(e) => e.key === "Escape" && (e.stopPropagation(), (ctrFilter.value = null))}
          />
        </div>
      )}
      <Under k="#search" x={x} />
      <PaneSection
        id="containers"
        title={t("more.containers")}
        open={!shut.value.has("#")}
        onToggle={() => flip("#")}
        count={v ? v.containers.length : undefined}
        actions={canAct && <Act icon="clear-all" act="prune-containers" label={t("ctr.prune")} onClick={() => lap("ctr.prune", "ctr.prune_containers_q", "containers.prune", { kind: "containers" })} />}
      >
        {!l || l.s === "loading" ? (
          <p class="pv-msg">{t("loading")}</p>
        ) : l.s === "err" ? (
          <p class="pv-msg c-err">{l.msg}</p>
        ) : v && v.containers.length === 0 ? (
          <p class="pv-msg">{t("ctr.none")}</p>
        ) : (
          <Tree label={t("more.containers")}>
            {[...projects].map(([p, cs]) => (
              <Fragment key={p}>
                <ProjectRow p={p} cs={cs} x={x} />
                {!shut.value.has(p) && <CtrRows cs={cs} level={1} x={x} />}
              </Fragment>
            ))}
            <CtrRows cs={loose} level={0} x={x} />
          </Tree>
        )}
        {v && v.more > 0 && <p class="pv-msg">{t("ctr.more", { n: v.more })}</p>}
        {v && <p class="pv-msg">{t("ctr.engine", { engine: v.engine, context: v.context })}</p>}
      </PaneSection>
      <HostSections x={x} />
      {laptop && (
        <AskLaptop
          label={laptop.label}
          text={laptop.text}
          send={() => link.request(laptop.op, laptop.args)}
          onDone={() => {
            setLaptop(null);
            reloadAll();
          }}
        />
      )}
    </PaneView>
  );
}

/** OutputView is the panel's OUTPUT tab: the chosen container's logs. */
export function OutputView() {
  const l = logs.value;
  return (
    <section class="view outv" data-view="output" aria-label={t("tasks.output")}>
      {!l ? (
        <p class="outv-note">{t("wb.output_none")}</p>
      ) : (
        <>
          <Portal into="panel-view-actions">
            <div class="outv-head">
              <span class="outv-channel" title={l.id}>
                <Cod name="vm-running" /> {l.name}
              </span>
              <Act icon="refresh" label={t("ctr.refresh")} onClick={() => showLogs(l)} />
            </div>
          </Portal>
          {l.redacted > 0 && <p class="outv-note">{t("ctr.redacted", { n: l.redacted })}</p>}
          {l.truncated && <p class="outv-note">{t("ctr.older")}</p>}
          <div class="outv-body" dir="ltr" tabIndex={0}>
            {l.loading ? (
              <p class="outv-note">{t("loading")}</p>
            ) : l.err ? (
              <p class="outv-note c-err">{l.err}</p>
            ) : l.lines.length === 0 ? (
              <p class="outv-note">{t("ctr.no_logs")}</p>
            ) : (
              l.lines.map((x, i) => (
                <div class="outv-line" key={i}>
                  {x}
                </div>
              ))
            )}
          </div>
        </>
      )}
    </section>
  );
}
