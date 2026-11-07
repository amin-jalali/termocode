// The MCP view strings of the phone app (MOBUX-14). The phone app is
// English only (owner 2026-11-07), so these stay out of strings.ts and
// its Persian twin. Same voice as strings.ts: calm, short, present
// tense, "the laptop", no other product named. {name} marks a value.
export const MS = {
  "mcpv.title": "MCP views",
  "mcpv.search": "Search tools",
  "mcpv.none": "No MCP server is set up on the laptop.",
  "mcpv.no_match": "No tool matches.",
  "mcpv.unknown": "Connect this server on the laptop once to see its tools.",
  "mcpv.off": "Turned off on the laptop",
  "mcpv.ask_ai": "Ask the AI",
  "mcpv.ask_text": "Show {tool} from {server} as a view.",
  "mcpv.run": "Show",
  "mcpv.running": "Loading…",
  "mcpv.again": "Show again",
  "mcpv.back": "Back to the tools",
  "mcpv.shaped": "View",
  "mcpv.raw": "{ } Raw",
  "mcpv.raw_label": "Show the data as",
  "mcpv.raw_cut": "The raw data is cut. The full result is on the laptop.",
  "mcpv.required": "{label}, needed",
  "mcpv.pick": "Pick one",
  "mcpv.yes": "Yes",
  "mcpv.no": "No",
  "mcpv.uri": "Resource",
  "mcpv.uri_pick": "Pick a resource or type its address",
  "mcpv.typed_schema": "Typed by the tool",
  "mcpv.typed_learned": "Typed from the last result",
  "mcpv.typed_none": "Shape known after the first run",
  "mcpv.need": "Fill in {label}.",
  "mcpv.number": "{label} is a number.",
  "mcpv.secret": "{label} looks like a secret. Type it on the laptop.",
  "mcpv.failed": "The laptop could not show this view.",
  "mcpv.retry": "Retry",
  "mcpv.viewer": "Your role can look, not run tools.",
  "mcpv.shape.table": "Table",
  "mcpv.shape.list": "List",
  "mcpv.shape.card": "Card",
  "mcpv.shape.detail": "Detail",
  "mcpv.shape.text": "Text",
  "mcpv.shape.json": "Raw data",
  "mcpv.shape.status": "Status",
} as const;

export type McpKey = keyof typeof MS;

/** ms is the text of key with its {name} values filled in. */
export function ms(key: McpKey, vars?: Record<string, string | number>): string {
  const text: string = MS[key];
  if (!vars) return text;
  return text.replace(/\{([a-z_]+)\}/g, (m, name: string) => (name in vars ? String(vars[name]) : m));
}
