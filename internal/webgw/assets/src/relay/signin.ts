// What the relay sign-in gave (NEW-10), pure: a paired phone signs in
// with its relay key alone (/login/relay). The reply is a token for the
// panel (net/auth.ts's key), or, with web_require_totp, a ticket for the
// panel's code step (ui/SignInScreen.tsx reads termocode.totp). Nothing
// is kept from a refused reply.

export const TOKEN = "termocode.session";
export const TICKET = "termocode.totp";

/** keepRelaySignIn keeps the token or the ticket of a reply. */
export function keepRelaySignIn(ok: boolean, d: { token?: unknown; totp?: unknown; ticket?: unknown }, s: Pick<Storage, "setItem">): void {
  if (!ok) return;
  if (typeof d.token === "string" && d.token) s.setItem(TOKEN, d.token);
  else if (d.totp === true && typeof d.ticket === "string" && d.ticket) s.setItem(TICKET, d.ticket);
}
