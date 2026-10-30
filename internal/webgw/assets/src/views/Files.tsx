// The Files tab (WEB-16, web UX 5.4): the laptop's open files first, so
// the file the AI just changed is one tap away, then the workspace tree.
// Folders list lazily when opened; names keep their own direction while
// the indent stays on the start side; git letters use the git colours;
// ignored names are dim; dotfiles follow the laptop and can be hidden here.
import { Fragment, h, type ComponentChildren } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { BufItem, TreeDir, TreeEntry } from "../../../../webproto/proto";
import { go } from "../lib/router";
import { expanded, failed, hideDots, reconcile, retry, toggle } from "../lib/tree";
import { link, listOf, live, store } from "../session";
import { t } from "../strings";
import { Chip } from "../ui/Chip";
import { gitBadge, TreeRow } from "../ui/rows";
import { toast } from "../lib/toasts";
import { TopicBody } from "./Placeholder";
import { View } from "./View";

/** join is a workspace path under dir. */
export function join(dir: string, name: string): string {
  return dir ? dir + "/" + name : name;
}

/** openFile shows a file in the file view (the back button returns here). */
export function openFile(path: string): void {
  go({ tab: "files", rest: path, query: {} });
}

interface TreeData {
  label: string;
  hidden: boolean;
  dirs: Record<string, TreeDir>;
}

function readTree(data: unknown): TreeData | null {
  if (typeof data !== "object" || data === null) return null;
  const d = data as Record<string, unknown>;
  const roots = Array.isArray(d.roots) ? d.roots : [];
  const root = roots[0] as { label?: unknown } | undefined;
  const dirs = typeof d.dirs === "object" && d.dirs !== null ? (d.dirs as Record<string, TreeDir>) : {};
  return { label: typeof root?.label === "string" ? root.label : "", hidden: d.hidden === true, dirs };
}

/** StatusRow is a one-line row inside a folder: loading, offline, empty, failed. */
function StatusRow({ depth, children }: { depth: number; children: ComponentChildren }) {
  return (
    <div class={`tree-note depth-${Math.min(depth, 8)}`} dir="auto">
      {children}
    </div>
  );
}

function Folder({ path, depth, tree }: { path: string; depth: number; tree: TreeData }) {
  const [filter, setFilter] = useState("");
  const dir = tree.dirs[path];
  const why = failed.value.get(path);
  if (!dir) {
    if (why !== undefined) {
      return (
        <StatusRow depth={depth}>
          <span class="tree-err">{t("files.err", { reason: why })}</span>
          <button type="button" class="link tree-retry" onClick={() => retry(path)}>
            {t("retry")}
          </button>
        </StatusRow>
      );
    }
    if (!live.value) return <StatusRow depth={depth}>{t("offline.input")}</StatusRow>;
    return (
      <div class={`tree-note depth-${Math.min(depth, 8)}`} aria-busy="true">
        <span class="sk-row" aria-hidden="true" />
      </div>
    );
  }
  const hide = hideDots.value;
  const needle = filter.trim().toLowerCase();
  const entries = (dir.entries ?? []).filter((e: TreeEntry) => !(hide && e.hidden) && (!needle || e.name.toLowerCase().includes(needle)));
  return (
    <>
      {dir.truncated && (
        <StatusRow depth={depth}>
          <span>{t("files.truncated")}</span>
          <input
            class="tree-filter"
            type="search"
            dir="auto"
            value={filter}
            placeholder={t("files.filter")}
            aria-label={t("files.filter")}
            onInput={(e) => setFilter((e.currentTarget as HTMLInputElement).value)}
          />
        </StatusRow>
      )}
      {entries.length === 0 && !needle && <StatusRow depth={depth}>{t("files.empty_dir")}</StatusRow>}
      {entries.map((e) => {
        const p = join(path, e.name);
        const open = e.dir && expanded.value.has(p);
        return (
          <Fragment key={e.name}>
            <TreeRow
              depth={depth}
              dir={e.dir}
              open={open}
              name={e.name}
              git={gitBadge(e.git)}
              dim={e.ignored === true}
              path={p}
              onClick={() => (e.dir ? toggle(p) : openFile(p))}
            />
            {open && <Folder path={p} depth={depth + 1} tree={tree} />}
          </Fragment>
        );
      })}
    </>
  );
}

/** OpenOnLaptop is the group of the laptop's open files. */
function OpenOnLaptop() {
  const bufs = listOf<BufItem>(store.topic("bufs").value?.data, "bufs").filter((b) => typeof b.path === "string" && b.path);
  if (bufs.length === 0) return null;
  return (
    <section class="tree-group" aria-labelledby="g-bufs">
      <h2 class="group-title" id="g-bufs">
        {t("laptop.open")}
      </h2>
      {bufs.map((b) => (
        <TreeRow key={b.buf} name={b.path} dirty={b.dirty} active={b.active} path={b.path} onClick={() => openFile(b.path)} />
      ))}
    </section>
  );
}

function Tree() {
  const st = store.topic("tree").value;
  const tree = readTree(st?.data);
  // Ask for every open folder the topic does not have (start, new connection).
  useEffect(() => {
    if (tree) reconcile(tree.dirs, st?.stale === true);
  }, [st?.data, st?.stale, live.value, expanded.value]);
  if (!tree) return null;
  return (
    <div class="tree">
      <OpenOnLaptop />
      <section class="tree-group" aria-labelledby="g-tree">
        <h2 class="group-title trunc" id="g-tree" dir="auto">
          {tree.label || t("tab.files")}
        </h2>
        <Folder path="" depth={0} tree={tree} />
      </section>
    </div>
  );
}

/** DotfilesChip hides dotfiles here; when the laptop hides them there is nothing to show. */
function DotfilesChip() {
  const tree = readTree(store.topic("tree").value?.data);
  if (!tree) return null;
  const shown = tree.hidden && !hideDots.value;
  return (
    <Chip
      on={shown}
      onClick={() => {
        if (!tree.hidden) toast("info", t("files.dots_laptop"));
        else hideDots.value = !hideDots.value;
      }}
    >
      {t("files.dots")}
    </Chip>
  );
}

export function TreeView() {
  useEffect(() => link.subscribe("tree", "bufs"), []);
  return (
    <View tab="files" title={t("tab.files")} actions={<DotfilesChip />}>
      <TopicBody topic="tree" empty={<p>{t("files.no_ws")}</p>} refused={(r) => (r.code === "not_found" ? t("files.no_ws") : r.msg)}>
        <Tree />
      </TopicBody>
    </View>
  );
}

