// The phone's renderer for the blocks of a tool line (MOB-10 slot,
// lib/toolblocks.ts). A block that carries a spec (view_show, MOBUX-17)
// gets a spec chip that opens the customizer. The Home chunk sets
// toolBlocks.value = phoneToolBlocks; the desktop never loads this.
import { signal } from "@preact/signals";
import { h } from "preact";
import type { Block, ChatMsg, ViewSpec } from "../../../../webproto/proto";
import { hasBlocks, type ToolBlocksRenderer } from "../lib/toolblocks";
import { BlockView } from "../ui/Blocks";
import { SpecChip } from "./SpecChip";

/** Customizing is the view the customizer sheet (MOBUX-16) edits. */
export interface Customizing {
  spec: ViewSpec;
  block: Block;
}

/** customizing is the spec a chip opened (null: the sheet is shut). */
export const customizing = signal<Customizing | null>(null);

function open(spec: ViewSpec, block: Block) {
  customizing.value = { spec, block };
}

export const phoneToolBlocks: ToolBlocksRenderer = (m: ChatMsg) => {
  if (!hasBlocks(m)) return null;
  return (
    <div class="ph-tool-blocks">
      {(m.blocks ?? []).map((b, i) => (
        <div class="ph-tool-block" key={b.id || i}>
          <BlockView block={b} />
          {b.spec && <SpecChip block={b} onCustomize={open} />}
        </div>
      ))}
    </div>
  );
};
