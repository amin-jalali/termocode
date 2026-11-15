// The keyboard's input state machine, free of the DOM: pointers go in by
// id and position, key events come out. One finger types (keys fire on
// touch down, with full multi-touch rollover); modifiers are held while a
// finger is on them and arm as one-shot sticky keys when tapped; a drag
// on the spacebar is a trackpad.
import { charCode, namedKey } from "./keys";
import { hitTest, placeKeys, type Placed } from "./layout";

/** A key press, in DOM KeyboardEvent terms. */
export interface KeyEvent {
  /** The DOM key: "a", "A", "!", " ", "Enter", "ArrowUp", "F5"… */
  key: string;
  /** The DOM code: "KeyA", "Digit1", "Space", "Enter", "F5"… */
  code: string;
  ctrl: boolean;
  alt: boolean;
  shift: boolean;
  /** Always false: the layout has no meta key (the AI key took its place). */
  meta: boolean;
  /** The keyboard's own id (enter, f5, pageUp…) or the main glyph. */
  id: string;
  /** True for an auto-repeat. */
  repeat: boolean;
}

/** The function-row media keys (pressed without fn). */
export type MediaAction = "mute" | "volumeDown" | "volumeUp" | "playPause" | "brightnessDown" | "brightnessUp";

/** Timers, injectable so tests run on a fake clock. */
export interface Clock {
  setTimeout(fn: () => void, ms: number): unknown;
  clearTimeout(h: unknown): void;
  setInterval(fn: () => void, ms: number): unknown;
  clearInterval(h: unknown): void;
}

export interface EngineOptions {
  compact?: boolean;
  onKey?(e: KeyEvent): void;
  onAI?(): void;
  /** Spacebar trackpad motion, already accelerated. */
  onPadMove?(dx: number, dy: number): void;
  /** F9: toggle the key sound. */
  onToggleSound?(): void;
  onMedia?(a: MediaAction): void;
  /** Anything visible changed (press, mods, caps, backlight, pad). */
  onChange?(): void;
  /** A keystroke tick: press (false) or the quieter release (true). */
  tick?(release: boolean): void;
  clock?: Clock;
}

/** Auto-repeat: first repeat after this many ms… */
export const REPEAT_DELAY = 350;
/** …then one every this many ms. */
export const REPEAT_EVERY = 55;
/** Spacebar travel (px) that turns a tap into the trackpad. */
export const PAD_SLOP = 10;

const REPEATABLE = new Set(["backspace", "delete", "left", "right", "up", "down"]);
const MEDIA: Record<string, MediaAction> = { f1: "mute", f2: "volumeDown", f3: "volumeUp", f4: "playPause", f6: "brightnessDown", f7: "brightnessUp" };
const FN_NUMBER: Record<string, string> = { "1": "F1", "2": "F2", "3": "F3", "4": "F4", "5": "F5", "6": "F6", "7": "F7", "8": "F8", "9": "F9", "0": "F10", "-": "F11", "=": "F12" };

const realClock: Clock = {
  setTimeout: (f, ms) => setTimeout(f, ms),
  clearTimeout: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
  setInterval: (f, ms) => setInterval(f, ms),
  clearInterval: (h) => clearInterval(h as ReturnType<typeof setInterval>),
};

export class Engine {
  placed: Placed[] = [];
  width = 0;
  height = 0;
  compact: boolean;
  /** Caps lock (a toggle). */
  caps = false;
  /** Backlight level 0 (off) … 2. */
  backlight = 0;
  /** True while the spacebar trackpad is dragging. */
  pad = false;
  /** Modifiers held by a finger now. */
  held = new Set<string>();
  /** Modifiers armed by a tap, for the next key. */
  sticky = new Set<string>();
  /** Live pointers and where they are (for the touch glow). */
  pointers = new Map<number, { x: number; y: number }>();

  private o: EngineOptions;
  private clock: Clock;
  private pressedBy = new Map<number, number>();
  private repeats = new Map<number, unknown>();
  private modBy = new Map<number, string>();
  private used = new Set<string>();
  private spacePointer: number | undefined;
  private spaceStart = { x: 0, y: 0 };
  private padLast = { x: 0, y: 0 };

  constructor(o: EngineOptions = {}) {
    this.o = o;
    this.compact = !!o.compact;
    this.clock = o.clock ?? realClock;
  }

  /** setOptions swaps the callbacks (and compact, which relayouts). */
  setOptions(o: Partial<EngineOptions>): void {
    this.o = { ...this.o, ...o };
    if (o.compact !== undefined && o.compact !== this.compact) this.layout(this.width, this.height, o.compact);
  }

  layout(width: number, height: number, compact = this.compact): void {
    this.width = width;
    this.height = height;
    this.compact = compact;
    this.placed = placeKeys(width, height, compact);
    this.changed();
  }

  get shiftOn(): boolean {
    return this.held.has("shift") || this.sticky.has("shift");
  }
  get ctrlOn(): boolean {
    return this.held.has("ctrl") || this.sticky.has("ctrl");
  }
  get altOn(): boolean {
    return this.held.has("alt") || this.sticky.has("alt");
  }
  get fnOn(): boolean {
    return this.held.has("fn") || this.sticky.has("fn");
  }

  /** The indexes of the keys drawn pressed. */
  pressedIndexes(): Set<number> {
    return new Set(this.pressedBy.values());
  }

  down(id: number, x: number, y: number): void {
    this.pointers.set(id, { x, y });
    const i = hitTest(this.placed, x, y);
    if (i < 0) return this.changed();
    const p = this.placed[i];
    if (p.def.id === "blank") return this.changed();
    this.tick(false);
    const def = p.def;
    if (def.kind === "space") {
      // a tap types a space on release, a drag becomes the trackpad
      this.spacePointer = id;
      this.spaceStart = { x, y };
      this.padLast = { x, y };
      this.pressedBy.set(id, i);
      return this.changed();
    }
    if (def.kind === "mod" && def.id === "caps") {
      this.caps = !this.caps;
      this.pressedBy.set(id, i);
      return this.changed();
    }
    if (def.kind === "mod") {
      this.modBy.set(id, def.id!);
      this.held.add(def.id!);
      this.pressedBy.set(id, i);
      return this.changed();
    }
    this.pressedBy.set(id, i);
    this.fire(p, false);
    if (REPEATABLE.has(p.id ?? "")) this.startRepeat(id, p);
    this.changed();
  }

  move(id: number, x: number, y: number): void {
    if (!this.pointers.has(id)) return;
    this.pointers.set(id, { x, y });
    if (id !== this.spacePointer) return this.changed();
    if (!this.pad) {
      if (Math.hypot(x - this.spaceStart.x, y - this.spaceStart.y) > PAD_SLOP) {
        this.pad = true;
        this.pressedBy.delete(id);
        this.padLast = { x, y };
      }
      return this.changed();
    }
    const dx = x - this.padLast.x;
    const dy = y - this.padLast.y;
    this.padLast = { x, y };
    const gain = 1 + Math.hypot(dx, dy) * 0.06; // faster flicks travel farther
    this.o.onPadMove?.(dx * gain, dy * gain);
    this.changed();
  }

  up(id: number): void {
    this.pointers.delete(id);
    if (id === this.spacePointer) {
      const wasDrag = this.pad;
      const i = this.pressedBy.get(id);
      this.pressedBy.delete(id);
      this.spacePointer = undefined;
      this.pad = false;
      if (!wasDrag && i !== undefined) {
        this.fire(this.placed[i], false);
        this.tick(true);
      }
      return this.changed();
    }
    const m = this.modBy.get(id);
    if (m !== undefined) {
      // a modifier that modified nothing while held arms (or disarms) as
      // a one-shot sticky key: ctrl, tap, c → ctrl+c
      this.tick(true);
      this.modBy.delete(id);
      const stillHeld = [...this.modBy.values()].includes(m);
      if (!stillHeld) {
        this.held.delete(m);
        if (!this.used.has(m)) {
          if (this.sticky.has(m)) this.sticky.delete(m);
          else this.sticky.add(m);
        }
      }
      this.used.delete(m);
      this.pressedBy.delete(id);
      return this.changed();
    }
    if (this.repeats.has(id)) {
      this.stopRepeat(id);
      this.tick(true);
      this.pressedBy.delete(id);
      return this.changed();
    }
    if (this.pressedBy.has(id)) {
      this.tick(true);
      this.pressedBy.delete(id);
    }
    this.changed();
  }

  cancel(id: number): void {
    this.pointers.delete(id);
    this.stopRepeat(id);
    if (id === this.spacePointer) {
      this.spacePointer = undefined;
      this.pad = false;
    }
    const m = this.modBy.get(id);
    this.modBy.delete(id);
    if (m !== undefined && ![...this.modBy.values()].includes(m)) this.held.delete(m);
    this.pressedBy.delete(id);
    this.changed();
  }

  /** destroy stops every timer. */
  destroy(): void {
    for (const id of [...this.repeats.keys()]) this.stopRepeat(id);
  }

  private tick(release: boolean): void {
    this.o.tick?.(release);
  }

  private changed(): void {
    this.o.onChange?.();
  }

  private startRepeat(id: number, p: Placed): void {
    this.repeats.set(
      id,
      this.clock.setTimeout(() => {
        this.repeats.set(
          id,
          this.clock.setInterval(() => {
            this.tick(false);
            this.fire(p, true);
          }, REPEAT_EVERY),
        );
      }, REPEAT_DELAY),
    );
  }

  private stopRepeat(id: number): void {
    const h = this.repeats.get(id);
    if (h === undefined) return;
    this.clock.clearTimeout(h);
    this.clock.clearInterval(h);
    this.repeats.delete(id);
  }

  private emit(key: string, code: string, id: string, repeat: boolean, mods = true): void {
    this.o.onKey?.({ key, code, id, repeat, ctrl: mods && this.ctrlOn, alt: mods && this.altOn, shift: this.shiftOn, meta: false });
  }

  private fire(p: Placed, repeat: boolean): void {
    const def = p.def;
    const shift = this.shiftOn;
    const chord = this.ctrlOn || this.altOn; // ctrl/alt + a printable key is a shortcut, not text
    switch (def.kind) {
      case "letter": {
        const upper = chord ? shift : shift !== this.caps;
        const ch = upper ? def.main! : def.main!.toLowerCase();
        this.emit(ch, charCode(def.main!), def.main!.toLowerCase(), repeat);
        break;
      }
      case "char": {
        const fnKey = this.fnOn ? (def.main === "`" ? "esc" : FN_NUMBER[def.main!]) : undefined;
        if (fnKey) {
          const [k, c] = namedKey(fnKey);
          this.emit(k, c, fnKey.toLowerCase(), repeat);
        } else {
          this.emit(shift ? (def.shifted ?? def.main!) : def.main!, charCode(def.main!), def.main!, repeat);
        }
        break;
      }
      case "space":
        this.emit(" ", "Space", "space", repeat, false);
        break;
      case "ai":
        this.o.onAI?.();
        return; // the AI key keeps the armed modifiers
      case "mod":
        return;
      default: {
        const fid = def.id;
        if (this.fnOn && fid === "f5") {
          this.backlight = (this.backlight + 1) % 3;
          break;
        }
        if (!this.fnOn && fid === "f9") {
          this.o.onToggleSound?.();
          break;
        }
        if (!this.fnOn && fid && MEDIA[fid]) {
          this.o.onMedia?.(MEDIA[fid]);
          break;
        }
        let id = p.id!;
        if (this.fnOn && id === "up") id = "pageUp";
        else if (this.fnOn && id === "down") id = "pageDown";
        else if (this.fnOn && id === "left") id = "home";
        else if (this.fnOn && id === "right") id = "end";
        else if (this.fnOn && def.fnum) id = def.fnum.toLowerCase();
        const [k, c] = namedKey(id);
        this.emit(k, c, id, repeat);
      }
    }
    this.afterFire();
  }

  /** A key used the modifiers: consume armed ones, mark held ones used. */
  private afterFire(): void {
    for (const m of this.held) this.used.add(m);
    this.sticky.clear();
  }
}
