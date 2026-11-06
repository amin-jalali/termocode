// More commands of the workbench's command list (WEBP-9): laptop palette
// commands the page does too, each through its own op or on the page.
// Only the desktop workbench loads this file (lib/commands.ts).
import type { DebugLaunch, MCPItems } from "../../../../webproto/proto";
import { link } from "../session";
import { t } from "../strings";
import { toast } from "./toasts";
import { panes, quick, setPanes, showFile } from "./workbench";
import { getDraft, setDraft } from "./drafts";
import { readChat } from "../state/chat";
import { store } from "../session";
import type { Command } from "./commands";
import { collapseTests } from "../views/run/TestingView";
import { logs } from "../views/desk/DeskContainers";
import { askSide } from "../views/desk/SideChat";

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

/** insertMCP puts an MCP prompt or resource into the chat draft, as the
 * laptop's picker does: a prompt starts the message, a resource is
 * added after what was typed. Nothing is sent. */
export function insertMCP(text: string): void {
  const chat = readChat(store.topic("chat").peek()?.data)?.id ?? "";
  const was = getDraft(chat);
  setDraft(chat, text.startsWith("/") || !was.trim() ? text : was.endsWith(" ") ? was + text : was + " " + text);
  if (!panes.peek().chat) setPanes({ chat: true });
}

/** pickMCP lists the laptop's MCP prompts and resources to pick one. */
export function pickMCP(): void {
  if (!panes.peek().chat) setPanes({ chat: true }); // the chat's topic comes while the list loads
  link.request("ai.mcp_items").then((r) => {
    const items = (r as MCPItems).items ?? [];
    if (!items.length) {
      toast("info", t("mcpi.none"));
      return;
    }
    quick.value = { mode: "pick", pick: { title: t("mcpi.title"), items: items.map((x) => ({ id: x.text, label: x.title, detail: x.hint })), run: insertMCP } };
  }, failed);
}

/** moreCommands are this file's commands. */
export function moreCommands(): Command[] {
  return [
    { id: "debugOpenLaunch", label: "cmd.debug_open_launch", when: canAct, run: openLaunch },
    { id: "testingCollapse", label: "cmd.testing_collapse", run: collapseTests },
    { id: "aiSide", label: "cmd.ai_side", when: canAct, run: askSide },
    { id: "aiMcpInsert", label: "cmd.ai_mcp_insert", when: canAct, run: pickMCP },
    { id: "outputClear", label: "cmd.output_clear", when: () => logs.peek() !== null, run: clearOutput },
  ];
}
