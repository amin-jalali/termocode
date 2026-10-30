// The launch secret and the session token (web design 3.1, ADR 0020).
// The laptop opens /#s=<secret>; the page takes the secret out of the URL
// at once (so it never lands in history, a bookmark or a screenshot),
// trades it on POST /auth for a session token, and keeps the token in
// sessionStorage: this tab only, gone when the tab closes. No cookies.

const TOKEN_KEY = "termocode.session";
const SECRET_HASH = /^#s=([A-Za-z0-9_-]{43})$/;
const PAIR_HASH = /^#p=([A-Za-z0-9_-]{43})$/;

export type AuthFailure = "expired" | "busy" | "failed";

let memoryToken: string | null = null;

export class AuthError extends Error {
  constructor(readonly why: AuthFailure) {
    super(why);
  }
}

/** takeSecret reads a launch secret from the hash and removes it from the
 * URL in the same step. A malformed #s= is removed too, and gives null. */
export function takeSecret(): string | null {
  const hash = location.hash;
  if (!hash.startsWith("#s=")) return null;
  history.replaceState(null, "", location.pathname + "#/chat");
  const m = SECRET_HASH.exec(hash);
  return m ? m[1] : null;
}

/** takePair reads a pairing secret (#p=, from the laptop's QR code or a
 * share link) and removes it from the URL in the same step, like
 * takeSecret. */
export function takePair(): string | null {
  const hash = location.hash;
  if (!hash.startsWith("#p=")) return null;
  history.replaceState(null, "", location.pathname + "#/chat");
  const m = PAIR_HASH.exec(hash);
  return m ? m[1] : null;
}

export function savedToken(): string | null {
  try {
    return sessionStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

export function forgetToken(): void {
  memoryToken = null;
  try {
    sessionStorage.removeItem(TOKEN_KEY);
  } catch {
    // Nothing kept, nothing to forget.
  }
}

/** redeem trades the launch secret for a session token and keeps it. */
export async function redeem(secret: string): Promise<string> {
  let res: Response;
  try {
    res = await fetch("/auth", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ secret }),
      credentials: "omit",
      cache: "no-store",
      redirect: "error",
      referrerPolicy: "no-referrer",
    });
  } catch {
    throw new AuthError("failed");
  }
  if (res.status === 401) throw new AuthError("expired");
  if (res.status === 429 || res.status === 503) throw new AuthError("busy");
  if (!res.ok) throw new AuthError("failed");
  let token: unknown;
  try {
    token = ((await res.json()) as { token?: unknown }).token;
  } catch {
    throw new AuthError("failed");
  }
  if (typeof token !== "string" || token === "") throw new AuthError("failed");
  keepToken(token);
  return token;
}

/** keepToken keeps a session token for this tab only. */
function keepToken(token: string): void {
  try {
    sessionStorage.setItem(TOKEN_KEY, token);
  } catch {
    // Without storage the token lives in memory: a reload needs a new link.
  }
  memoryToken = token;
}

/** Reply is a JSON answer of the gateway: its status and object. */
export interface Reply {
  status: number;
  data: Record<string, unknown>;
}

/** postJSON posts body to one of the gateway's own endpoints (pairing and
 * passkey sign-in), the way /auth is posted: no credentials, no cache, no
 * redirect, no referrer. A network failure is status 0. */
export async function postJSON(path: "/pair/start" | "/pair/wait" | "/pair/finish" | "/login/start" | "/login/finish", body: unknown): Promise<Reply> {
  let res: Response;
  try {
    res = await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      credentials: "omit",
      cache: "no-store",
      redirect: "error",
      referrerPolicy: "no-referrer",
    });
  } catch {
    return { status: 0, data: Object.create(null) as Record<string, unknown> };
  }
  let data: unknown = null;
  try {
    data = await res.json();
  } catch {
    // Not JSON: the status alone tells.
  }
  const obj = typeof data === "object" && data !== null && !Array.isArray(data) ? (data as Record<string, unknown>) : (Object.create(null) as Record<string, unknown>);
  return { status: res.status, data: obj };
}

/** keepIssuedToken keeps the token of a finished pairing or sign-in reply;
 * false when the reply has none. */
export function keepIssuedToken(r: Reply): boolean {
  const tok = r.data.token;
  if (r.status !== 200 || typeof tok !== "string" || tok === "") return false;
  keepToken(tok);
  return true;
}

/** token is the session token of this tab, if any. */
export function token(): string | null {
  return memoryToken ?? savedToken();
}
