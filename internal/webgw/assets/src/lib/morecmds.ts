// More commands of the workbench's command list (WEBP-9): laptop palette
// commands the page does too, each through its own op or on the page.
// Only the desktop workbench loads this file (lib/commands.ts).
import type { DebugLaunch, KeyBindings, KeyBound, LSPServer, LSPServerList, MCPItems, ThemeColor, ThemeColors, ThemeList } from "../../../../webproto/proto";
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
import { askHere, askLaptop } from "../ui/AskHere";

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
    quick.value = { mode: "pick", pick: { title: t("mcpi.title"), items: items.map((x) => ({ label: x.title, detail: x.hint, run: () => insertMCP(x.text) })) } };
  }, failed);
}

/** lspState is a language server's state in the picker's detail. */
function lspState(s: LSPServer): string {
  const parts = [
    s.state === "managed"
      ? t("lspm.managed", { version: s.version ?? "" }).trim()
      : s.state === "system"
        ? t("lspm.system")
        : s.state === "installing"
          ? s.progress
            ? t("lspm.installing_line", { line: s.progress })
            : t("lspm.installing")
          : t("lspm.missing"),
  ];
  if (s.used) parts.push(t("lspm.used"));
  if (s.problem) parts.push(s.problem);
  return parts.join(" · ");
}

/** askLSP asks here, then the laptop card, to install or uninstall s. */
function askLSP(s: LSPServer, action: "install" | "uninstall"): void {
  askLaptop.value = {
    label: t("cmd.lsp_manage"),
    text: t(action === "install" ? "lspm.install_ask" : "lspm.uninstall_ask", { name: s.name }),
    send: () => link.request("lsp.server_ask", { name: s.name, action }),
  };
}

/** pickLSP is a row picked: install a missing one (or one on PATH);
 * termocode's own copy offers Reinstall and Uninstall. */
function pickLSP(s: LSPServer): void {
  if (s.state === "installing") {
    toast("info", t("lspm.busy", { name: s.name }));
    return;
  }
  if (s.state !== "managed") {
    askLSP(s, "install");
    return;
  }
  setTimeout(
    () =>
      (quick.value = {
        mode: "pick",
        pick: {
          title: s.title,
          items: [
            { label: t("lspm.reinstall"), run: () => askLSP(s, "install") },
            { label: t("lspm.uninstall"), run: () => askLSP(s, "uninstall") },
          ],
        },
      }),
    0,
  );
}

/** manageLSP lists the laptop's language servers (LSP: Manage Language
 * Servers…): the workspace's own first, each with its state. */
export function manageLSP(): void {
  link.request("lsp.servers").then((r) => {
    const items = (r as LSPServerList).items ?? [];
    quick.value = { mode: "pick", pick: { title: t("lspm.title"), items: items.map((s) => ({ label: s.title, detail: lspState(s), run: () => pickLSP(s) })) } };
  }, failed);
}

/** pickTheme lists the laptop's color themes (Preferences: Color
 * Theme…); the one picked shows on the laptop, and this page follows
 * the laptop's colors. */
export function pickTheme(): void {
  link.request("theme.list").then((r) => {
    const l = r as ThemeList;
    const detail = (id: string, light?: boolean) => [id === l.active ? t("thm.active") : "", light ? t("thm.light") : ""].filter(Boolean).join(" · ") || undefined;
    quick.value = {
      mode: "pick",
      pick: {
        title: t("thm.title"),
        items: (l.themes ?? []).map((th) => ({
          label: th.name,
          detail: detail(th.id, th.light),
          run: () => link.request("theme.set", { id: th.id }).then(() => toast("ok", t("thm.set", { name: th.name })), failed),
        })),
      },
    };
  }, failed);
}

/** setThemeColor sends one custom color ("" gives it back to the theme). */
function setThemeColor(key: string, color: string): void {
  link
    .request("theme.color_set", { key, color })
    .then(() => toast("ok", color ? t("thm.color_set", { key, color }) : t("thm.color_reset", { key })), failed);
}

/** askThemeColor asks the new color of key here. */
function askThemeColor(key: string): void {
  setTimeout(
    () =>
      (quick.value = {
        mode: "ask",
        ask: {
          title: t("thm.ask", { key }),
          run: (raw) => {
            const color = raw.trim();
            if (!/^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/.test(color)) {
              toast("warn", t("thm.bad"));
              return;
            }
            setThemeColor(key, color);
          },
        },
      }),
    0,
  );
}

/** pickThemeColor is a color picked: a new color, or (a custom one) back
 * to the theme's own. */
function pickThemeColor(c: ThemeColor): void {
  if (!c.custom) {
    askThemeColor(c.key);
    return;
  }
  setTimeout(
    () =>
      (quick.value = {
        mode: "pick",
        pick: {
          title: c.key,
          items: [
            { label: t("thm.change"), run: () => askThemeColor(c.key) },
            { label: t("thm.reset"), run: () => setThemeColor(c.key, "") },
          ],
        },
      }),
    0,
  );
}

/** editTheme lists the custom theme's colors (Preferences: Edit Custom
 * Theme); a change shows the custom theme on the laptop. */
export function editTheme(): void {
  link.request("theme.colors").then((r) => {
    const items = (r as ThemeColors).colors ?? [];
    quick.value = {
      mode: "pick",
      pick: { title: t("thm.colors"), items: items.map((c) => ({ label: c.key, detail: c.custom ? c.color + " · " + t("thm.custom") : c.color, run: () => pickThemeColor(c) })) },
    };
  }, failed);
}

/** bindKey binds key to action on the laptop; a key another action has
 * is asked here first, then taken with replace. */
function bindKey(action: string, key: string, replace = false): void {
  link.request("keys.bind", replace ? { action, key, replace } : { action, key }).then((r) => {
    const b = r as KeyBound;
    if (b.conflict && !replace) {
      askHere.value = {
        label: t("cmd.keybindings"),
        text: t("kb.conflict_body", { key, owner: b.conflict, action }),
        verb: t("kb.replace"),
        run: () => bindKey(action, key, true),
      };
      return;
    }
    toast("ok", t("kb.bound", { action, key }));
  }, failed);
}

/** customizeKeys lists the laptop's actions with their keys (Preferences:
 * Customize Keybindings); one picked takes a key typed here. */
export function customizeKeys(): void {
  link.request("keys.list").then((r) => {
    const items = (r as KeyBindings).items ?? [];
    quick.value = {
      mode: "pick",
      pick: {
        title: t("kb.title"),
        items: items.map((b) => ({
          label: b.action,
          detail: b.keys.length ? b.keys.join(", ") : t("kb.none"),
          run: () =>
            setTimeout(
              () =>
                (quick.value = {
                  mode: "ask",
                  ask: { title: t("kb.ask", { action: b.action }), run: (key) => bindKey(b.action, key.trim()) },
                }),
              0,
            ),
        })),
      },
    };
  }, failed);
}

/** moreCommands are this file's commands. */
export function moreCommands(): Command[] {
  return [
    { id: "debugOpenLaunch", label: "cmd.debug_open_launch", when: canAct, run: openLaunch },
    { id: "testingCollapse", label: "cmd.testing_collapse", run: collapseTests },
    { id: "aiSide", label: "cmd.ai_side", when: canAct, run: askSide },
    { id: "aiMcpInsert", label: "cmd.ai_mcp_insert", when: canAct, run: pickMCP },
    { id: "pickTheme", label: "cmd.pick_theme", when: canAct, run: pickTheme },
    { id: "editTheme", label: "cmd.edit_theme", when: canAct, run: editTheme },
    { id: "customizeKeys", label: "cmd.keybindings", when: canAct, run: customizeKeys },
    { id: "lspManage", label: "cmd.lsp_manage", when: canAct, run: manageLSP },
    { id: "outputClear", label: "cmd.output_clear", when: () => logs.peek() !== null, run: clearOutput },
  ];
}
