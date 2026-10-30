// Dictionaries keyed by names the page does not choose (folder paths,
// topic names, URL query keys): a plain {} would read "constructor" from
// Object.prototype and let "__proto__" swap its prototype. A dict has no
// prototype, and own/has look only at its own keys (T49, lint_test.go).
//
// Pure: no DOM, timers or network.

/** Dict is a string-keyed table with no prototype. */
export type Dict<T> = { [key: string]: T };

const hasOwn = Object.prototype.hasOwnProperty;

/** dict returns a new table without a prototype, holding from's own keys. */
export function dict<T>(from?: Readonly<Record<string, T>> | null): Dict<T> {
  const out = Object.create(null) as Dict<T>;
  if (from) for (const k of Object.keys(from)) out[k] = from[k];
  return out;
}

/** has reports whether d holds k itself (never a prototype's key). */
export function has(d: Readonly<Record<string, unknown>>, k: string): boolean {
  return hasOwn.call(d, k);
}

/** own returns d's own value for k, or undefined. */
export function own<T>(d: Readonly<Record<string, T>>, k: string): T | undefined {
  return hasOwn.call(d, k) ? d[k] : undefined;
}
