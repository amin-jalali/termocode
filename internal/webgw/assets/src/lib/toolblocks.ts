// The slot for the blocks of a tool line (MOB-10). The laptop sends a
// tool's block in ChatMsg.blocks; the phone's lazy Home chunk, once
// loaded, puts its block renderer here so the chat draws the blocks
// under the tool line. The desktop never fills the slot, so its chat and
// app.js stay as they are (a tool line shows its text only).
import { signal } from "@preact/signals";
import type { VNode } from "preact";
import type { ChatMsg } from "../../../../webproto/proto";

/** ToolBlocksRenderer draws the blocks of one tool message. */
export type ToolBlocksRenderer = (m: ChatMsg) => VNode | null;

/** toolBlocks is the renderer the phone's Home chunk set (null: none). */
export const toolBlocks = signal<ToolBlocksRenderer | null>(null);

/** ChatLook is the desktop's chat look (aictx.js, aictx/look.tsx): the
 * workbench puts it here once aictx.js loaded. An AI answer without a
 * run card (a line with its model and time, its tool calls as short
 * lines), a tool line, and the composer's profile and model picker.
 * The phone never fills it and keeps its own look. */
export interface ChatLook {
  /** m draws an AI answer around body (its text), or a tool line (body
   * null); null leaves it to the page. */
  m(m: ChatMsg, body: VNode | null): VNode | null;
  pick(): VNode;
}

export const look = signal<ChatLook | null>(null);

/** hasBlocks says whether a message carries any block. */
export function hasBlocks(m: ChatMsg): boolean {
  return Array.isArray(m.blocks) && m.blocks.length > 0;
}
