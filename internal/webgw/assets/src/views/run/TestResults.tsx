// TEST RESULTS, a tab of the desktop's bottom panel (WEBP-4): the last
// run's output, or one test's own output (a failed test in TESTING
// opens it here). It reads the laptop's runner (tests.output) when it
// shows, again when the tree changes, and every second while tests run.
import { h } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { TestOutput } from "../../../../../webproto/proto";
import { link, live, store } from "../../session";
import { readTestsSnap } from "../../state/run";
import { t } from "../../strings";
import { TermRow } from "../../ui/rows";
import { Note } from "./bits";
import { resultsOf } from "./TestingView";

const EVERY_MS = 1000;

export function TestResults() {
  useEffect(() => link.subscribe("tests"), []);
  const s = readTestsSnap(store.topic("tests").value?.data);
  const id = resultsOf.value;
  const running = s?.state === "running";
  const [out, setOut] = useState<TestOutput | null>(null);
  useEffect(() => {
    if (!live.value) return;
    let stop = false;
    const read = () =>
      link.request("tests.output", id ? { id } : {}).then(
        (r) => !stop && setOut(r as TestOutput),
        () => !stop && setOut({ id, lines: [] }),
      );
    read();
    const timer = running ? setInterval(read, EVERY_MS) : undefined;
    return () => {
      stop = true;
      clearInterval(timer);
    };
  }, [id, running, s?.command, s?.counts.passed, s?.counts.failed, live.value]);
  const name = id ? (s?.items.find((x) => x.id === id)?.label ?? id) : "";
  return (
    <div class="tres" data-panel-view="test-results">
      <div class="tres-head">
        <span class="tres-what" dir="auto">
          {name ? t("test.output_of", { name }) : (s?.command ?? t("test.results"))}
        </span>
        {name && (
          <button type="button" class="tx-link" onClick={() => (resultsOf.value = "")}>
            {t("test.all_output")}
          </button>
        )}
      </div>
      {!out ? null : out.lines.length === 0 ? (
        <Note>{t("test.no_output")}</Note>
      ) : (
        <div class="tres-rows" dir="ltr">
          {out.truncated && <Note>{t("tasks.older")}</Note>}
          {out.lines.map((l, i) => (
            <TermRow key={i} text={l} />
          ))}
        </div>
      )}
    </div>
  );
}
