// More commands of the workbench's command list (WEBP-9): laptop palette
// commands the page does too, each through its own op or on the page.
// Only the desktop workbench loads this file (lib/commands.ts).
import type { DebugLaunch } from "../../../../webproto/proto";
import { link } from "../session";
import { t } from "../strings";
import { toast } from "./toasts";
import { showFile } from "./workbench";
import type { Command } from "./commands";
import { collapseTests } from "../views/run/TestingView";
import { logs } from "../views/desk/DeskContainers";

const canAct = () => link.meta.peek()?.role === "controller";

/** failed shows why a request was refused. */
const failed = (e: unknown) => toast("warn", e instanceof Error ? e.message : t("cmd.failed"));

/** openLaunch opens the workspace's launch.json (the laptop writes its
 * template first when there is none). */
export function openLaunch(): void {
  link.request("debug.open_launch").then((r) => showFile((r as DebugLaunch).path), failed);
}

/** clearOutput empties the OUTPUT panel's lines (its channel stays). */
export function clearOutput(): void {
  const l = logs.peek();
  if (l) logs.value = { ...l, lines: [], redacted: 0, truncated: false };
}

/** moreCommands are this file's commands. */
export function moreCommands(): Command[] {
  return [
    { id: "debugOpenLaunch", label: "cmd.debug_open_launch", when: canAct, run: openLaunch },
    { id: "testingCollapse", label: "cmd.testing_collapse", run: collapseTests },
    { id: "outputClear", label: "cmd.output_clear", when: () => logs.peek() !== null, run: clearOutput },
  ];
}
