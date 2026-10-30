// The module registry of the desktop editor's frame. The vendored editor
// ships as plain scripts that each call define(name, deps, factory); the
// frame loads them in order after this file, so every dependency is
// already defined when a module needs it. No loader, no fetch, no eval:
// only the scripts monaco.html names (with their SRI hashes) ever run.

type Exports = Record<string, unknown>;
type Factory = (...deps: unknown[]) => unknown;

const mods = new Map<string, Exports>();

/** resolve turns a dependency relative to module name into a name. */
function resolve(name: string, dep: string): string {
  if (!dep.startsWith("./")) return dep;
  const i = name.lastIndexOf("/");
  return (i < 0 ? "" : name.slice(0, i + 1)) + dep.slice(2);
}

function need(name: string): Exports {
  const m = mods.get(name);
  if (!m) throw new Error("editor frame: module " + name + " is not loaded");
  return m;
}

function define(name: string, deps: string[], factory: Factory): void {
  const exports: Exports = {};
  const req = Object.assign((dep: string) => need(resolve(name, dep)), {
    // The editor asks where its worker script is; the frame runs none.
    toUrl: (p: string) => p,
  });
  const args = deps.map((d) => (d === "exports" ? exports : d === "require" ? req : need(resolve(name, d))));
  const out = factory(...args);
  mods.set(name, typeof out === "object" && out !== null ? (out as Exports) : exports);
}

Object.assign(define, { amd: {} });
Object.assign(globalThis, { define, termocodeModule: need });
