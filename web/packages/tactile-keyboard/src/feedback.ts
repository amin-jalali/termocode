// Keystroke feedback: a soft click (Web Audio) and a short vibration.
// Sound (on/off) and vibration (level 0…3) are independent settings.

/** Vibration length (ms) per level: off, light, medium, strong. The
 * original also set an amplitude (28/65/120); the web has no amplitude. */
export const VIBRATE_MS = [0, 6, 12, 18] as const;

/** Click gain on the press and the quieter release. */
export const CLICK_VOLUME = { press: 0.08, release: 0.04 } as const;

export interface Feedback {
  soundOn: boolean;
  hapticLevel: number;
  /** Plays a click at a gain (0…1). */
  click?: (volume: number) => void;
  /** Vibrates for ms. */
  vibrate?: (ms: number) => void;
}

/** makeTick returns the keyboard's tick: a click on press and release, a
 * vibration on press only. It reads the settings on every tick. */
export function makeTick(get: () => Feedback): (release: boolean) => void {
  return (release) => {
    const f = get();
    if (f.soundOn) f.click?.(release ? CLICK_VOLUME.release : CLICK_VOLUME.press);
    if (release) return;
    const ms = VIBRATE_MS[Math.max(0, Math.min(3, Math.round(f.hapticLevel)))];
    if (ms) f.vibrate?.(ms);
  };
}

type AudioCtor = new () => AudioContext;

/** webClick makes a short mechanical click with Web Audio: a 12ms burst
 * of decaying noise through a band-pass. The context starts on the first
 * key (a user gesture). It never throws. */
export function webClick(): (volume: number) => void {
  let ctx: AudioContext | undefined;
  let noise: AudioBuffer | undefined;
  return (volume) => {
    try {
      const g = globalThis as unknown as { AudioContext?: AudioCtor; webkitAudioContext?: AudioCtor };
      const C = g.AudioContext ?? g.webkitAudioContext;
      if (!C) return;
      ctx ??= new C();
      if (ctx.state === "suspended") void ctx.resume();
      if (!noise) {
        const n = Math.floor(ctx.sampleRate * 0.012);
        noise = ctx.createBuffer(1, n, ctx.sampleRate);
        const d = noise.getChannelData(0);
        for (let i = 0; i < n; i++) d[i] = (Math.random() * 2 - 1) * Math.pow(1 - i / n, 4);
      }
      const src = ctx.createBufferSource();
      src.buffer = noise;
      const bp = ctx.createBiquadFilter();
      bp.type = "bandpass";
      bp.frequency.value = 2400;
      bp.Q.value = 0.8;
      const gain = ctx.createGain();
      gain.gain.value = volume * 4; // the noise burst is quiet; scale to a soft click
      src.connect(bp).connect(gain).connect(ctx.destination);
      src.start();
    } catch {
      // no sound: never break typing
    }
  };
}

/** webVibrate is navigator.vibrate where there is one. */
export function webVibrate(): ((ms: number) => void) | undefined {
  const n = (globalThis as { navigator?: Navigator }).navigator;
  if (!n || typeof n.vibrate !== "function") return undefined;
  return (ms) => {
    try {
      n.vibrate(ms);
    } catch {
      // no vibration
    }
  };
}
