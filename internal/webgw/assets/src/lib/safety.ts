// Defence in depth for never-do 20 (no HTML from data). The lint test
// keeps our sources clean; this hook makes the rule hold at run time too:
// preact's only HTML sink is the dangerouslySetInnerHTML prop, and a link
// may only go to this page's own paths or http(s). An image may also show
// a blob: URL this page made itself (WEB-57: image bytes from the hub).
import { options, type VNode } from "preact";

const SAFE_HREF = /^(#|\/(?!\/)|https?:)/i;

export class UnsafeVNodeError extends Error {}

export function installSafety(): void {
  const prev = options.vnode;
  options.vnode = (vnode: VNode) => {
    // Text vnodes carry their string as props; only element props matter.
    const props = typeof vnode.props === "object" ? (vnode.props as Record<string, unknown> | null) : null;
    if (props) {
      if ("dangerouslySetInnerHTML" in props) throw new UnsafeVNodeError("HTML from data is not allowed");
      for (const k of ["href", "src", "action", "formaction", "xlink:href"]) {
        const v = props[k];
        if (typeof v !== "string" || SAFE_HREF.test(v.trim())) continue;
        if (k === "src" && vnode.type === "img" && v.startsWith("blob:" + location.origin + "/")) continue;
        throw new UnsafeVNodeError(`unsafe ${k}`);
      }
    }
    if (prev) prev(vnode);
  };
}
