// aictx.js's view of the page's Preact (api.ts): JSX here compiles to
// h(…), which calls the page's own, so these elements are the page's. H
// is the host; init sets it before anything runs.
import type { JSX as PJSX } from "preact";
import type { AIHost } from "./api";

export let H!: AIHost;

/** setHost keeps the workbench's host (init). */
export function setHost(host: AIHost): void {
  H = host;
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
