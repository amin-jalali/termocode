// looksSecret says whether text seems to hold a secret: a key or token
// shape, a private key block, or "password: …"-like pairs. Used to keep
// such drafts out of the browser's storage (the draft still lives in
// memory for the page). Pure; errs on the side of "secret".
const SHAPES = [
  /\bsk-[A-Za-z0-9_-]{8,}/,
  /\b(?:gh[pousr]|github_pat)_[A-Za-z0-9_]{16,}/,
  /\bxox[abprs]-[A-Za-z0-9-]{8,}/,
  /\bAKIA[0-9A-Z]{16}\b/,
  /\bAIza[0-9A-Za-z_-]{20,}/,
  /\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\./,
  /-----BEGIN [A-Z ]*PRIVATE KEY-----/,
  /\b(?:password|passwd|pwd|secret|token|api[_-]?key|bearer)\b\s*[:=]?\s*\S{6,}/i,
  /\b[A-Za-z0-9+/_-]{40,}\b/,
];

export function looksSecret(text: string): boolean {
  return SHAPES.some((re) => re.test(text));
}
