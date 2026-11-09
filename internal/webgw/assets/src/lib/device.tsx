// This device, Pair a Device… and Devices… (desktop only) live in
// device.js: the workbench's one dynamic import loads it on first use
// and gives it the page's Preact, sheet, link, fields and toasts (one
// copy of each in the page). A failed load can be tried again.
import { h, render } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type * as DeviceModule from "../device";
import type { DeviceHost } from "../device";
import { link } from "../session";
import { t } from "../strings";
import { askLaptop } from "../ui/AskHere";
import { BookmarkField } from "../ui/BookmarkField";
import { NotifyField } from "../ui/NotifyField";
import { Sheet } from "../ui/Sheet";
import { SetPage } from "../views/desk/DeskSettings";
import { prefs, setPref } from "./prefs";
import { toast } from "./toasts";

let mod: Promise<typeof DeviceModule> | null = null;

const host: DeviceHost = {
  h,
  render,
  useState,
  useEffect,
  useRef,
  Sheet,
  link,
  toast,
  ask: (label, text, send) => (askLaptop.value = { label, text, send }),
  t,
  prefs,
  setPref,
  SetPage,
  NotifyField,
  BookmarkField,
};

function load(): Promise<typeof DeviceModule> {
  return (mod ??= import("/assets/device.js").then((m) => (m.init(host), m))).catch((e: unknown) => {
    mod = null;
    toast("warn", t("cmd.failed"));
    throw e;
  });
}

/** openPair opens web-pair (Pair a Device…) or web-devices (Devices…). */
export function openPair(what: string): void {
  load().then((m) => m.open(what), () => undefined);
}

/** DeviceView is the This device page, once device.js is in. */
export function DeviceView() {
  const [m, setM] = useState<typeof DeviceModule | null>(null);
  useEffect(() => void load().then(setM, () => undefined), []);
  return m && <m.DevicePage />;
}
