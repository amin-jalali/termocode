// A small fake DOM: just what the keyboard and the dock touch, so their
// tests run in node. Elements keep children, classes, a style map,
// attributes and listeners; dispatch calls the listeners of the target
// and bubbles to its parents.
type Listener = (e: FakeEvent) => void;

export interface FakeEvent {
  type: string;
  target?: FakeElement;
  pointerId?: number;
  clientX?: number;
  clientY?: number;
  key?: string;
  isTrusted?: boolean;
  touches?: { length: number };
  accelerationIncludingGravity?: { x: number; y: number; z: number };
  defaultPrevented?: boolean;
  preventDefault?: () => void;
  stopPropagation?: () => void;
}

class Style {
  private m = new Map<string, string>();
  [k: string]: unknown;
  setProperty(k: string, v: string): void {
    this.m.set(k, String(v));
  }
  getPropertyValue(k: string): string {
    return this.m.get(k) ?? "";
  }
  removeProperty(k: string): void {
    this.m.delete(k);
  }
}

class ClassList {
  constructor(private el: FakeElement) {}
  private get set(): Set<string> {
    return new Set(this.el.className.split(/\s+/).filter(Boolean));
  }
  private save(s: Set<string>): void {
    this.el.className = [...s].join(" ");
  }
  add(...c: string[]): void {
    const s = this.set;
    c.forEach((x) => s.add(x));
    this.save(s);
  }
  remove(...c: string[]): void {
    const s = this.set;
    c.forEach((x) => s.delete(x));
    this.save(s);
  }
  contains(c: string): boolean {
    return this.set.has(c);
  }
  toggle(c: string, on?: boolean): boolean {
    const want = on ?? !this.contains(c);
    if (want) this.add(c);
    else this.remove(c);
    return want;
  }
}

export class FakeElement {
  tagName: string;
  className = "";
  classList = new ClassList(this);
  style = new Style();
  dataset: Record<string, string> = {};
  attrs = new Map<string, string>();
  children: FakeElement[] = [];
  parentNode: FakeElement | null = null;
  textContent = "";
  innerHTML = "";
  hidden = false;
  rect = { left: 0, top: 0, width: 0, height: 0 };
  ownerDocument: FakeDocument | null = null;
  namespaceURI = "";
  captured = new Set<number>();
  private ls = new Map<string, Listener[]>();

  constructor(tag: string) {
    this.tagName = tag.toUpperCase();
  }
  setAttribute(k: string, v: string): void {
    this.attrs.set(k, String(v));
  }
  getAttribute(k: string): string | null {
    return this.attrs.get(k) ?? null;
  }
  removeAttribute(k: string): void {
    this.attrs.delete(k);
  }
  appendChild<T extends FakeElement>(c: T): T {
    c.remove();
    c.parentNode = this;
    this.children.push(c);
    return c;
  }
  append(...cs: FakeElement[]): void {
    cs.forEach((c) => this.appendChild(c));
  }
  replaceChildren(...cs: FakeElement[]): void {
    this.children.forEach((c) => (c.parentNode = null));
    this.children = [];
    this.append(...cs);
  }
  remove(): void {
    const p = this.parentNode;
    if (!p) return;
    p.children = p.children.filter((c) => c !== this);
    this.parentNode = null;
  }
  addEventListener(t: string, f: Listener): void {
    this.ls.set(t, [...(this.ls.get(t) ?? []), f]);
  }
  removeEventListener(t: string, f: Listener): void {
    this.ls.set(t, (this.ls.get(t) ?? []).filter((x) => x !== f));
  }
  listeners(t: string): number {
    return (this.ls.get(t) ?? []).length;
  }
  dispatchEvent(e: FakeEvent): boolean {
    e.target ??= this;
    let stop = false;
    e.preventDefault ??= () => (e.defaultPrevented = true);
    e.stopPropagation ??= () => (stop = true);
    for (let n: FakeElement | null = this; n && !stop; n = n.parentNode) for (const f of n.ls.get(e.type) ?? []) f(e);
    return !e.defaultPrevented;
  }
  getBoundingClientRect(): { left: number; top: number; width: number; height: number; right: number; bottom: number } {
    const r = this.rect;
    return { ...r, right: r.left + r.width, bottom: r.top + r.height };
  }
  setPointerCapture(id: number): void {
    this.captured.add(id);
  }
  releasePointerCapture(id: number): void {
    this.captured.delete(id);
  }
  hasPointerCapture(id: number): boolean {
    return this.captured.has(id);
  }
  focus(): void {}
  /** all descendants (self included) that match pred, in tree order */
  all(pred: (e: FakeElement) => boolean): FakeElement[] {
    const out: FakeElement[] = [];
    const walk = (e: FakeElement): void => {
      if (pred(e)) out.push(e);
      e.children.forEach(walk);
    };
    walk(this);
    return out;
  }
  byClass(c: string): FakeElement[] {
    return this.all((e) => e.classList.contains(c));
  }
  text(): string {
    return this.textContent + this.children.map((c) => c.text()).join("");
  }
}

export class FakeDocument {
  createElement(tag: string): FakeElement {
    const e = new FakeElement(tag);
    e.ownerDocument = this;
    return e;
  }
  createElementNS(ns: string, tag: string): FakeElement {
    const e = this.createElement(tag);
    e.namespaceURI = ns;
    return e;
  }
}

export class FakeWindow extends FakeElement {
  innerWidth = 390;
  innerHeight = 844;
  frames: (() => void)[] = [];
  constructor() {
    super("window");
  }
  requestAnimationFrame(f: () => void): number {
    this.frames.push(f);
    return this.frames.length;
  }
  cancelAnimationFrame(): void {}
  /** runs n animation frames */
  frame(n = 1): void {
    for (let i = 0; i < n; i++) {
      const fs = this.frames;
      this.frames = [];
      fs.forEach((f) => f());
    }
  }
}

let nextPointer = 1;
export function pointer(type: string, x: number, y: number, id?: number): FakeEvent {
  return { type, pointerId: id ?? nextPointer++, clientX: x, clientY: y, isTrusted: true };
}
