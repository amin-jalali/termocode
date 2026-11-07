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

/** hasBlocks says whether a message carries any block. */
export function hasBlocks(m: ChatMsg): boolean {
  return Array.isArray(m.blocks) && m.blocks.length > 0;
}
