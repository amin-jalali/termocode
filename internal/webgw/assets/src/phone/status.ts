// Phone status (MOBUX-1): always a colour and a shape, never colour alone.
// The shape carries the meaning for colour-blind users and in grey
// scale; the label is what a screen reader says. phone_test.go reads the
// rows below, so keep one row per line.
import type { StringKey } from "../strings";

export type Status = "needs" | "working" | "review" | "done" | "failed" | "idle" | "asleep";

export interface StatusLook {
  glyph: string;
  /** tone names the colour token --p-st-<tone> and the class ph-st-<tone>. */
  tone: Status;
  label: StringKey;
}

export const STATUS: Readonly<{ [K in Status]: StatusLook }> = {
  needs: { glyph: "●", tone: "needs", label: "ph.st.needs" },
  working: { glyph: "◐", tone: "working", label: "ph.st.working" },
  review: { glyph: "◆", tone: "review", label: "ph.st.review" },
  done: { glyph: "✓", tone: "done", label: "done" },
  failed: { glyph: "✗", tone: "failed", label: "test.failed" },
  idle: { glyph: "○", tone: "idle", label: "ph.st.idle" },
  asleep: { glyph: "◌", tone: "asleep", label: "ph.st.asleep" },
};

/** BOARD_ORDER is the order of the live board's columns and of the Home
 * cards: what needs the user comes first. */
export const BOARD_ORDER: readonly Status[] = ["needs", "working", "review", "done"];
