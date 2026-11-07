// The Home's view of the page's Preact (home/api.ts): JSX in home.js
// compiles to h(…) and Fragment, which here call the page's own, so the
// Home's elements, hooks and signals are the page's. H is the host;
// init sets it before the first draw.
import type { Fragment as PFragment, JSX as PJSX } from "preact";
import type { HomeHost } from "./api";

export let H!: HomeHost;

export let Fragment!: typeof PFragment;

/** setHost keeps the page's host (HomeModule.init). */
export function setHost(host: HomeHost): void {
  H = host;
  Fragment = host.Fragment;
}

/** h is the page's createElement. */
export function h(type: unknown, props: unknown, ...children: unknown[]): PJSX.Element {
  return (H.h as unknown as (t: unknown, p: unknown, ...c: unknown[]) => PJSX.Element)(type, props, ...children);
}

// The JSX types of h: Preact's own, so the Home's JSX is checked as the
// page's is (tsconfig.json: jsxFactory h).
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
