// What kind of computer the page runs on, for the key names it shows.

/** isMac: the keyboard has ⌘ for the shortcuts Ctrl does elsewhere. */
export function isMac(): boolean {
  const p = (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData?.platform ?? navigator.platform ?? "";
  return /mac|iphone|ipad/i.test(p);
}

/** modKey is the name of the shortcut key: ⌘ on a Mac, Ctrl elsewhere. */
export function modKey(): string {
  return isMac() ? "⌘" : "Ctrl";
}
