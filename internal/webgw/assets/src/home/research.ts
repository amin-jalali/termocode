// Research in chat (MOB-15, plan 2.1 and use case 13). The Research chip
// never spends tokens: a tap only puts "Research: " in the composer and
// waits for Send; the AI turn then runs on the laptop with the laptop's
// own web_search / web_fetch. The chip is off, and says why, while both
// web tools are off on the laptop (AISnap.webSearch / webFetch). A
// source row's url opens only after the confirm sheet (lib/links).
//
// Pure: no DOM, timers or network. English only (the phone app).
import type { AISnap } from "../../../../webproto/proto";

/** RESEARCH_PREFIX starts the composer's text after a tap on the chip. */
export const RESEARCH_PREFIX = "Research: ";

/** Words of the chip and the sources (English only, plan rule). */
export const RESEARCH_TEXT = {
  wait: "Waiting for the laptop",
  off: "Web tools are off on the laptop. Turn them on there: AI: Web Tools…",
  fetchOnly: "Web search is off on the laptop: the AI can only read pages you name.",
  on: "Searches with {provider} on the laptop",
  open: "Open source",
  noLink: "This source has no link to open.",
} as const;

/** ResearchState is what the chip shows. */
export interface ResearchState {
  /** on: a tap fills the composer; off: a tap shows note only. */
  on: boolean;
  /** note is one calm line under the chip (always set). */
  note: string;
}

/** researchState reads the ai topic (null: not here yet). */
export function researchState(ai: AISnap | null): ResearchState {
  if (!ai) return { on: false, note: RESEARCH_TEXT.wait };
  const search = typeof ai.webSearch === "string" ? ai.webSearch.trim() : "";
  if (search) return { on: true, note: RESEARCH_TEXT.on.replace("{provider}", search) };
  if (ai.webFetch === true) return { on: true, note: RESEARCH_TEXT.fetchOnly };
  return { on: false, note: RESEARCH_TEXT.off };
}

/** researchDraft is the composer text after a tap: the prefix first,
 * once, and the user's own words kept after it. */
export function researchDraft(cur: string): string {
  const rest = cur.trimStart();
  if (rest.toLowerCase().startsWith(RESEARCH_PREFIX.toLowerCase())) return cur;
  return RESEARCH_PREFIX + rest;
}

/** SourceRef is the ref of a sources row (BlockRef shape). */
export interface SourceRef {
  kind: string;
  id?: string;
}

/** sourceURL is the http(s) url a row stands for, or null. The laptop
 * sends it as a ref of kind "url"; anything else never opens. */
export function sourceURL(ref: SourceRef | null | undefined): string | null {
  if (!ref || ref.kind !== "url" || typeof ref.id !== "string") return null;
  const u = ref.id.trim();
  if (!/^https?:\/\/[^\s"<>]+$/i.test(u)) return null;
  try {
    const p = new URL(u);
    return p.protocol === "https:" || p.protocol === "http:" ? p.href : null;
  } catch {
    return null;
  }
}

/** isResearchBlock says a block came from the laptop's web tools. */
export function isResearchBlock(b: { source?: string } | null | undefined): boolean {
  return !!b && (b.source === "web_search" || b.source === "web_fetch");
}
