// TESTING (WEBP-4): the laptop's test tree (the tests topic) as the
// laptop's TESTING view lists it: packages, files and tests, each with
// the state of its last run. A row runs or debugs its tests (the hover
// buttons), opens its place in the editor (a failed test: where it
// failed, with its output in TEST RESULTS), and folds. The head runs all,
// reruns the failed ones, stops the run, finds the tests again and
// folds the tree. #/more/testing
import { h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useMemo } from "preact/hooks";
import type { TestItem } from "../../../../../webproto/proto";
import { goTo } from "../../lib/lsp";
import { useNow } from "../../lib/time";
import { setPanes, showFile } from "../../lib/workbench";
import { panelTab } from "../../lib/lsp";
import { link, store } from "../../session";
import { duration, matchTests, readTestsSnap, testTree, type TestNode } from "../../state/run";
import { t, type StringKey } from "../../strings";
import { Cod } from "../../ui/icons";
import { SkeletonRows } from "../../ui/Skeleton";
import { SubView } from "../more/SubView";
import { ask, canAct, IconBtn, Note } from "./bits";

/** The tree's view state (kept while the page lives). */
const folded = signal<ReadonlySet<string>>(new Set());
const filter = signal("");
const picked = signal<string | null>(null);
/** resultsOf is the test whose output TEST RESULTS shows ("": the run's log). */
export const resultsOf = signal("");

const ICON: Readonly<Record<string, string>> = { passed: "pass", failed: "error", skipped: "circle-slash", running: "loading", pending: "circle-outline" };
const STATE: Readonly<Record<string, StringKey>> = { passed: "test.passed", failed: "test.failed", skipped: "test.skipped", running: "test.running", pending: "test.pending" };

/** TestIcon is a test's state as the TESTING view draws it. */
export function TestIcon({ state }: { state: string }) {
  return (
    <span class={`tx-icon tx-${state}`} role="img" aria-label={t(STATE[state] ?? "test.pending")}>
      <Cod name={ICON[state] ?? "circle-outline"} />
    </span>
  );
}

/** showResults shows a test's output (or the run's) in TEST RESULTS. */
export function showResults(id: string): void {
  resultsOf.value = id;
  panelTab.value = "test-results";
  setPanes({ panel: true });
}

/** openTest shows a row's place: a failed test where it failed. */
function openTest(it: TestItem): void {
  if (it.kind === "case" && it.state === "failed" && it.fail_path) goTo(it.fail_path, Math.max(0, (it.fail_line ?? 1) - 1), 0);
  else if (it.kind === "case" && it.path) goTo(it.path, Math.max(0, (it.line ?? 1) - 1), 0);
  else if (it.kind === "file" && it.path) showFile(it.path);
}

function toggle(id: string): void {
  const n = new Set(folded.peek());
  if (n.has(id)) n.delete(id);
  else n.add(id);
  folded.value = n;
}

function Row({ n, depth, act }: { n: TestNode; depth: number; act: boolean }) {
  const it = n.item;
  const open = !folded.value.has(it.id) || filter.value !== "";
  const kids = n.kids.length > 0;
  const click = () => {
    picked.value = it.id;
    if (it.kind === "package") return toggle(it.id);
    openTest(it);
    if (it.kind === "case" && it.state === "failed") showResults(it.id);
  };
  return (
    <div role="treeitem" aria-expanded={kids ? (open ? "true" : "false") : undefined} aria-selected={picked.value === it.id ? "true" : "false"}>
      <div
        class={`tx-row${picked.value === it.id ? " is-picked" : ""}`}
        data-id={it.id}
        style={{ "--d": depth }}
        title={it.message ? `${it.label}\n${it.message}` : it.label}
        tabIndex={0}
        onClick={click}
        onKeyDown={(e) => {
          if (e.key === "Enter") click();
          else if ((e.key === "ArrowRight" && !open) || (e.key === "ArrowLeft" && open && kids)) toggle(it.id);
        }}
      >
        <span class="tx-twist" onClick={(e) => (e.stopPropagation(), kids && toggle(it.id))}>
          {kids && <Cod name={open ? "chevron-down" : "chevron-right"} />}
        </span>
        <TestIcon state={it.state} />
        <span class="tx-label trunc" dir="auto">
          {it.label}
        </span>
        {it.kind === "case" && (it.ms ?? 0) > 0 && (
          <span class="tx-desc" dir="ltr">
            {duration(it.ms ?? 0)}
          </span>
        )}
        {act && (
          <span class="tx-acts">
            <IconBtn icon="run" act="run" label={t("test.run")} onClick={() => ask("tests.run", { id: it.id })} />
            {it.debug && <IconBtn icon="debug-alt" act="debug" label={t("test.debug")} onClick={() => ask("tests.debug", { id: it.id })} />}
            {it.path && <IconBtn icon="go-to-file" act="goto" label={t("test.goto")} onClick={() => openTest(it)} />}
          </span>
        )}
      </div>
      {kids && open && (
        <div role="group">
          {n.kids.map((k) => (
            <Row key={k.item.id} n={k} depth={depth + 1} act={act} />
          ))}
        </div>
      )}
    </div>
  );
}

export function TestingView() {
  useEffect(() => link.subscribe("tests"), []);
  const s = readTestsSnap(store.topic("tests").value?.data);
  const act = canAct();
  const running = s?.state === "running";
  const now = useNow(running && s?.live === true, 1000);
  const got = useMemo(() => Date.now(), [s]);
  const roots = s ? matchTests(testTree(s.items), filter.value) : [];
  const c = s?.counts;
  const took = s ? (s.elapsed_ms ?? 0) + (s.live ? Math.max(0, now - got) : 0) : 0;
  const actions = act && (
    <span class="rt">
      <IconBtn icon="refresh" act="refresh" label={t("test.refresh")} onClick={() => ask("tests.refresh")} off={running} />
      {running ? (
        <IconBtn icon="debug-stop" act="stop" label={t("test.stop")} onClick={() => ask("tests.stop")} />
      ) : (
        <IconBtn icon="run-all" act="run-all" label={t("test.run_all")} onClick={() => ask("tests.run", {})} off={!s || s.items.length === 0} />
      )}
      <IconBtn icon="run-errors" act="run-failed" label={t("test.run_failed")} onClick={() => ask("tests.run", { failed: true })} off={running || !c || c.failed === 0} />
      <IconBtn icon="collapse-all" act="collapse" label={t("test.collapse")} onClick={() => (folded.value = new Set(s?.items.filter((x) => x.kind !== "case").map((x) => x.id)))} />
    </span>
  );
  return (
    <SubView title={t("act.testing")} name="testing" actions={actions}>
      <div class="tx">
        <input class="tx-filter field-box" type="search" placeholder={t("test.filter")} aria-label={t("test.filter")} value={filter.value} onInput={(e) => (filter.value = (e.currentTarget as HTMLInputElement).value)} />
        {s && c && (s.state === "running" || s.command) && (
          <p class="tx-sum" role="status">
            {running && <span class="tx-bar" aria-hidden="true" />}
            <span class="tx-sum-text">{t("test.sum", { passed: c.passed, total: c.total })}</span>
            {took > 0 && <span class="tx-desc" dir="ltr">{duration(took)}</span>}
            <button type="button" class="tx-link" onClick={() => showResults("")}>
              {t("test.results")}
            </button>
          </p>
        )}
        {s?.run_error && <Note>{s.run_error}</Note>}
        {!s || (s.state === "discovering" && s.items.length === 0) ? (
          <SkeletonRows rows={5} />
        ) : s.items.length === 0 ? (
          <Note>{s.error || t("test.none")}</Note>
        ) : (
          <div class="tx-tree" role="tree" aria-label={t("act.testing")}>
            {roots.map((n) => (
              <Row key={n.item.id} n={n} depth={0} act={act} />
            ))}
            {(s.more ?? 0) > 0 && <Note>{t("test.more", { n: s.more ?? 0 })}</Note>}
          </div>
        )}
      </div>
    </SubView>
  );
}
