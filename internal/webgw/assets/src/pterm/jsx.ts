// pterm.js's view of the page's Preact (api.ts): JSX here compiles to
// h(…) and Fragment, which call the page's own. H is the host; init sets
// it first.
import type { Fragment as PFragment, JSX as PJSX } from "preact";
import type { PTermHost } from "./api";

export let H!: PTermHost;

export let Fragment!: typeof PFragment;

/** setHost keeps the page's host (init). */
export function setHost(host: PTermHost): void {
  H = host;
  Fragment = host.Fragment;
}

/** h is the page's createElement. */
export function h(type: unknown, props: unknown, ...children: unknown[]): PJSX.Element {
  return (H.h as unknown as (t: unknown, p: unknown, ...c: unknown[]) => PJSX.Element)(type, props, ...children);
}

// The JSX types of h: Preact's own (tsconfig.json: jsxFactory h).
export namespace h {
  export namespace JSX {
    export type Element = PJSX.Element;
    export type ElementClass = PJSX.ElementClass;
    export type ElementAttributesProperty = PJSX.ElementAttributesProperty;
    export type ElementChildrenAttribute = PJSX.ElementChildrenAttribute;
    export type IntrinsicAttributes = PJSX.IntrinsicAttributes;
    export type IntrinsicElements = PJSX.IntrinsicElements;
    export type LibraryManagedAttributes<C, P> = PJSX.LibraryManagedAttributes<C, P>;
  }
}
