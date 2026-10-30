// Checks the topic reducers share: a delta that does not have the shape
// its reducer needs throws a DeltaError, and the store marks the topic
// broken and asks for a fresh snap. Pure.

export class DeltaError extends Error {}

export function bad(why: string): never {
  throw new DeltaError(why);
}

export function obj(v: unknown, what: string): Record<string, unknown> {
  if (typeof v !== "object" || v === null || Array.isArray(v)) bad(what + " is not an object");
  return v as Record<string, unknown>;
}

export function arr<T>(v: unknown, what: string): T[] {
  if (v === undefined || v === null) return [];
  if (!Array.isArray(v)) bad(what + " is not a list");
  return v as T[];
}

export function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

export function int(v: unknown, what: string): number {
  if (typeof v !== "number" || !Number.isInteger(v)) bad(what + " is not a whole number");
  return v;
}
