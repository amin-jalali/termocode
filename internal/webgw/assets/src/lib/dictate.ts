// Voice input on the page (CMP-48): the browser's own speech input turns
// speech into text in the chat box of the open chat. Nothing is sent: the
// words join the draft and the user presses Send. The laptop's
// microphone is never used from here (the laptop has AI: Dictate into
// Chat… for that). Where the browser has no speech input, the button and
// the command stay hidden.
import { signal } from "@preact/signals";
import { t } from "../strings";
import { drafts, setDraft } from "./drafts";
import { toast } from "./toasts";

/** The browser's speech recognizer, as much of it as we use. */
interface Recognizer {
  lang: string;
  continuous: boolean;
  interimResults: boolean;
  start(): void;
  stop(): void;
  abort(): void;
  onresult: ((e: { resultIndex: number; results: ArrayLike<ArrayLike<{ transcript: string }> & { isFinal: boolean }> }) => void) | null;
  onerror: ((e: { error: string }) => void) | null;
  onend: (() => void) | null;
}

type RecognizerCtor = new () => Recognizer;

function ctor(): RecognizerCtor | null {
  const w = window as unknown as { SpeechRecognition?: RecognizerCtor; webkitSpeechRecognition?: RecognizerCtor };
  return w.SpeechRecognition ?? w.webkitSpeechRecognition ?? null;
}

/** canDictate: this browser has speech input. */
export const canDictate = (): boolean => typeof window !== "undefined" && ctor() !== null;

/** dictateChat is the chat the shown composer writes to ("" = a new chat). */
export const dictateChat = signal<string | null>(null);

/** listening is the chat being dictated into, or null. */
export const listening = signal<string | null>(null);

let rec: Recognizer | null = null;
let noted = false;

/** joinWords adds words to a draft, with a space after a word. */
export function joinWords(draft: string, words: string): string {
  const w = words.replace(/\s+/g, " ").trim();
  if (!w) return draft;
  if (!draft || /\s$/.test(draft)) return draft + w;
  return draft + " " + w;
}

/** toggleDictate starts or stops listening for the shown composer's chat. */
export function toggleDictate(): void {
  if (rec) {
    rec.stop(); // the last words still arrive, then onend
    return;
  }
  const chat = dictateChat.peek();
  const C = ctor();
  if (chat === null) return;
  if (!C) {
    toast("info", t("dictate.unsupported"));
    return;
  }
  const r = new C();
  r.lang = document.documentElement.lang || navigator.language || "en";
  r.continuous = true;
  r.interimResults = false;
  r.onresult = (e) => {
    let words = "";
    for (let i = e.resultIndex; i < e.results.length; i++) {
      const res = e.results[i];
      if (res.isFinal && res.length > 0) words += " " + res[0].transcript;
    }
    if (words.trim()) setDraft(chat, joinWords(drafts.peek()[chat] ?? "", words));
  };
  r.onerror = (e) => {
    if (e.error === "aborted") return;
    if (e.error === "not-allowed" || e.error === "service-not-allowed") toast("warn", t("dictate.denied"));
    else if (e.error === "no-speech") toast("info", t("dictate.none"));
    else toast("warn", t("dictate.stopped", { why: e.error }));
  };
  r.onend = () => {
    rec = null;
    listening.value = null;
  };
  try {
    r.start();
  } catch (err) {
    toast("warn", t("dictate.stopped", { why: String(err) }));
    return;
  }
  rec = r;
  listening.value = chat;
  if (!noted) {
    noted = true;
    toast("info", t("dictate.listening"), t("dictate.note_body"));
  } else {
    toast("info", t("dictate.listening"));
  }
}

/** stopDictate ends listening at once (the composer went away). */
export function stopDictate(): void {
  rec?.abort();
}
