// A tiny test runner with no dependencies: test(name, fn) registers a
// case, run() runs them all in order, prints one line each and sets a
// non-zero exit code when any fails.
type Fn = () => void | Promise<void>;
const cases: [string, Fn][] = [];

export function test(name: string, fn: Fn): void {
  cases.push([name, fn]);
}

function show(v: unknown): string {
  return typeof v === "string" ? JSON.stringify(v) : JSON.stringify(v, (_k, x) => (x instanceof Set ? [...x] : x));
}

export function eq(got: unknown, want: unknown, msg = ""): void {
  if (show(got) !== show(want)) throw new Error(`${msg ? msg + ": " : ""}got ${show(got)}, want ${show(want)}`);
}

export function ok(cond: unknown, msg = "expected true"): void {
  if (!cond) throw new Error(msg);
}

export function near(got: number, want: number, eps = 1e-6, msg = ""): void {
  if (Math.abs(got - want) > eps) throw new Error(`${msg ? msg + ": " : ""}got ${got}, want ${want} ±${eps}`);
}

export async function run(): Promise<void> {
  let failed = 0;
  for (const [name, fn] of cases) {
    try {
      await fn();
      console.log(`ok   ${name}`);
    } catch (e) {
      failed++;
      console.log(`FAIL ${name}\n     ${(e as Error).message}`);
    }
  }
  console.log(`${cases.length - failed}/${cases.length} passed`);
  if (failed) (globalThis as { process?: { exitCode?: number } }).process!.exitCode = 1;
}
