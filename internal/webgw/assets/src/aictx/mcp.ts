// AI: Insert MCP Prompt or Resource…: the laptop's MCP prompts and
// resources in a picker; the one picked goes into the chat draft, as the
// laptop's picker does: a prompt starts the message, a resource is added
// after what was typed. Nothing is sent. In aictx.js with the chat's
// other extras, loaded on first use.
import type { MCPItems } from "../../../../webproto/proto";
import { readChat } from "../state/chat";
import { H } from "./jsx";

function insertMCP(text: string): void {
  const chat = readChat(H.store.topic("chat").peek()?.data)?.id ?? "";
  const was = H.getDraft(chat);
  H.setDraft(chat, text.startsWith("/") || !was.trim() ? text : was.endsWith(" ") ? was + text : was + " " + text);
  H.showChat();
}

/** pickMCP lists the laptop's MCP prompts and resources to pick one. */
export function pickMCP(): void {
  H.showChat(); // the chat's topic comes while the list loads
  H.link.request("ai.mcp_items").then(
    (r) => {
      const items = (r as MCPItems).items ?? [];
      if (!items.length) return void H.toast("info", H.t("mcpi.none"));
      H.quick.value = { mode: "pick", pick: { title: H.t("mcpi.title"), items: items.map((x) => ({ label: x.title, detail: x.hint, run: () => insertMCP(x.text) })) } };
    },
    (e: unknown) => H.toast("warn", e instanceof Error ? e.message : H.t("cmd.failed")),
  );
}
