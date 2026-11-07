// This phone (MOBUX-20, mobile plan 2.10): what the laptop keeps of this
// device, and the device's own choices. Phone layout only; built into
// home.js, so app.js never carries it. #/more/phone
//
//   This phone   name, role (● can act / ◐ watch only), paired, last seen,
//                passkey (✓ / ✗), bookmark
//   Laptops      the laptop serving this page (● connected now); the app
//                (MOB-21) keeps the others. Pair a laptop: QR scan in the
//                app, otherwise how to scan with the camera + 6-digit code
//   Notifications  Off / On (the browser asks only after On)
//   Quiet hours  Off / On + From / To; also kept here so notify() honours it
//   Lock the app Off / On (the app asks for the phone's own lock; a browser
//                shows "Only in the termocode app")
//   Forget this phone…  a sheet: Cancel first and focused, Forget arms
//                after 600 ms; then the page signs out and starts over.
//
// Every change goes to the laptop with phone.prefs (the gateway keeps it
// with the device record); nothing here is a secret.
import { h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useRef } from "preact/hooks";
import type { PhoneInfo, PhonePrefsArgs } from "../../../../../webproto/proto";
import { notifyState, perm, turnOff, turnOn } from "../../lib/notify";
import { prefs, setPref } from "../../lib/prefs";
import { ReqError } from "../../net/link";
import { forgetToken } from "../../net/auth";
import { forgetThisBrowser } from "../../net/devicekey";
import { link, live } from "../../session";
import { tp } from "../../strings.phone";
import { useArming } from "../../ui/arming";
import { Button } from "../../ui/Button";
import { Segmented } from "../../ui/Segmented";
import { Sheet } from "../../ui/Sheet";
import { SkeletonRows } from "../../ui/Skeleton";
import { SubView } from "../more/SubView";

/** AppBridge is what the termocode app (MOB-21) adds to the page; a plain
 * browser has none of it. */
interface AppBridge {
  scanQR?: () => void;
  lock?: boolean;
}

function app(): AppBridge | null {
  const b = (window as unknown as { termocodeApp?: AppBridge }).termocodeApp;
  return typeof b === "object" && b !== null ? b : null;
}

type Load = "loading" | "ready" | "failed" | "unpaired";
type Save = "" | "saved" | "failed";

const load = signal<Load>("loading");
const info = signal<PhoneInfo | null>(null);
const saved = signal<Save>("");
const forgetOpen = signal(false);
const forgetFailed = signal(false);

const DEF_FROM = "22:00";
const DEF_TO = "07:00";

async function read(): Promise<void> {
  load.value = "loading";
  try {
    const res = (await link.request("phone.get")) as PhoneInfo;
    info.value = res;
    // The laptop's copy of the quiet hours wins: one place for all phones' parts.
    const p = res.prefs;
    setPref("quiet", p.quiet_from && p.quiet_to ? `${p.quiet_from}-${p.quiet_to}` : "");
    load.value = "ready";
  } catch (e) {
    load.value = e instanceof ReqError && e.code === "forbidden" ? "unpaired" : "failed";
  }
}

/** save sends the changed choices to the laptop and keeps the quiet hours here too. */
async function save(next: PhonePrefsArgs): Promise<void> {
  const cur = info.peek();
  if (!cur) return;
  const args: PhonePrefsArgs = { alerts: next.alerts };
  if (next.quiet_from && next.quiet_to) {
    args.quiet_from = next.quiet_from;
    args.quiet_to = next.quiet_to;
  }
  const off = -new Date().getTimezoneOffset();
  if (off !== 0) args.utc_offset_min = off;
  if (next.lock) args.lock = true;
  info.value = { ...cur, prefs: args };
  setPref("quiet", args.quiet_from ? `${args.quiet_from}-${args.quiet_to}` : "");
  try {
    await link.request("phone.prefs", args);
    saved.value = "saved";
  } catch {
    saved.value = "failed";
    info.value = cur; // show what the laptop still has
  }
}

async function forget(): Promise<void> {
  forgetFailed.value = false;
  try {
    await link.request("phone.forget");
  } catch {
    forgetFailed.value = true;
    return;
  }
  forgetToken();
  try {
    await forgetThisBrowser();
  } catch {
    // The laptop forgot the bookmark with the device already.
  }
  location.replace("/");
}

function when(ms: number | undefined): string {
  return ms && ms > 0 ? new Date(ms).toLocaleString() : "—";
}

function Row({ k, v }: { k: string; v: string }) {
  return (
    <div class="kv-row">
      <dt class="kv-key">{k}</dt>
      <dd class="kv-value">{v}</dd>
    </div>
  );
}

const onOff = [
  { value: "off", label: tp("ph.off") },
  { value: "on", label: tp("ph.on") },
] as const;

function ForgetSheet() {
  const root = useRef<HTMLDivElement | null>(null);
  const arm = useArming(root);
  const close = () => {
    forgetOpen.value = false;
    forgetFailed.value = false;
  };
  return (
    <Sheet onLater={close} label={tp("ph.forget_title")}>
      <div class="phone-forget" ref={root} role="alertdialog" aria-labelledby="ph-forget-t" aria-describedby="ph-forget-b">
        <h2 class="card-title" id="ph-forget-t">
          {tp("ph.forget_title")}
        </h2>
        <p class="group-text" id="ph-forget-b">
          {tp("ph.forget_body")}
        </p>
        {forgetFailed.value && (
          <p class="phone-line is-error" role="alert">
            ✗ {tp("ph.forget_failed")}
          </p>
        )}
        <div class="phone-actions">
          <Button kind="neutral" wide autoFocus onClick={close}>
            {tp("ph.forget_cancel")}
          </Button>
          <button
            type="button"
            class={`btn btn-danger btn-wide arm${arm.armed ? " is-armed" : ""}`}
            aria-disabled={arm.armed && live.value ? undefined : "true"}
            aria-describedby={arm.armed ? undefined : "ph-forget-arm"}
            onPointerDown={arm.pressBegan}
            onKeyDown={(e: KeyboardEvent) => (e.key === "Enter" || e.key === " ") && arm.pressBegan()}
            onClick={() => {
              if (arm.pressOK() && live.peek()) void forget();
            }}
          >
            <span class="arm-fill" aria-hidden="true" />
            <span class="arm-label">{tp("ph.forget_yes")}</span>
          </button>
          <span id="ph-forget-arm" class="sr-only">
            {tp("ph.forget_arming")}
          </span>
        </div>
      </div>
    </Sheet>
  );
}

function Body({ d }: { d: PhoneInfo }) {
  void perm.value; // redraw once the browser answered
  const p = d.prefs;
  const nst = notifyState();
  const quietOn = !!(p.quiet_from && p.quiet_to);
  const from = p.quiet_from || DEF_FROM;
  const to = p.quiet_to || DEF_TO;
  const inApp = app() !== null;
  const guest = d.guest === true;
  const role = d.role === "controller" ? tp("ph.role_controller") : tp("ph.role_viewer");
  const passkey = d.needs_repair ? tp("ph.passkey_repair") : d.passkey ? tp("ph.passkey_ok") : tp("ph.passkey_none");
  const setTime = (which: "from" | "to", v: string) => {
    const f = which === "from" ? v : from;
    const t = which === "to" ? v : to;
    if (!/^\d\d:\d\d$/.test(f) || !/^\d\d:\d\d$/.test(t) || f === t) return;
    void save({ ...p, quiet_from: f, quiet_to: t });
  };
  return (
    <div class="phone">
      <section class="group" aria-labelledby="ph-dev">
        <h2 class="group-title" id="ph-dev">
          {tp("ph.device")}
        </h2>
        <dl class="kv">
          <Row k={tp("ph.name")} v={d.name} />
          <Row k={tp("ph.role")} v={guest ? tp("ph.guest") : role} />
          <Row k={tp("ph.paired")} v={when(d.paired_ms)} />
          <Row k={tp("ph.last_seen")} v={when(d.last_seen_ms)} />
          <Row k={tp("ph.passkey")} v={passkey} />
          {d.remembered && <Row k="" v={tp("ph.remembered")} />}
        </dl>
      </section>

      <section class="group" aria-labelledby="ph-laptops">
        <h2 class="group-title" id="ph-laptops">
          {tp("ph.laptops")}
        </h2>
        <ul class="phone-laptops">
          {d.laptops.map((l) => (
            <li class="phone-laptop" key={l.name}>
              <span class="phone-laptop-name">{l.name}</span>
              {l.this && <span class="phone-live">{tp("ph.laptop_this")}</span>}
            </li>
          ))}
        </ul>
        {!inApp && <p class="group-text">{tp("ph.laptops_app")}</p>}
        <div class="field">
          <span class="field-label">{tp("ph.pair")}</span>
          {inApp && app()?.scanQR ? (
            <Button kind="neutral" onClick={() => app()?.scanQR?.()}>
              {tp("ph.pair_scan")}
            </Button>
          ) : (
            <p class="group-text">{tp("ph.pair_body")}</p>
          )}
        </div>
      </section>

      <section class="group" aria-labelledby="ph-alerts">
        <h2 class="group-title" id="ph-alerts">
          {tp("ph.alerts")}
        </h2>
        {nst === "unsupported" ? (
          <p class="group-text">{tp("ph.alerts_none")}</p>
        ) : (
          <div class="field">
            <p class="group-text">{nst === "blocked" ? tp("ph.alerts_blocked") : tp("ph.alerts_body")}</p>
            <Segmented<"off" | "on">
              label={tp("ph.alerts")}
              value={nst === "on" ? "on" : "off"}
              choices={onOff}
              onChange={(v) => {
                if (v === "on") turnOn();
                else turnOff();
                if (!guest) void save({ ...p, alerts: v === "on" });
              }}
            />
          </div>
        )}
        <div class="field">
          <span class="field-label" id="ph-quiet">
            {tp("ph.quiet")}
          </span>
          <p class="group-text">{tp("ph.quiet_body")}</p>
          <Segmented<"off" | "on">
            label={tp("ph.quiet")}
            value={quietOn ? "on" : "off"}
            choices={onOff}
            onChange={(v) =>
              void save(v === "on" ? { ...p, quiet_from: from, quiet_to: to } : { ...p, quiet_from: undefined, quiet_to: undefined })
            }
          />
          {quietOn && (
            <div class="phone-times">
              <label class="phone-time">
                <span class="field-label">{tp("ph.quiet_from")}</span>
                <input class="search-input phone-time-input" type="time" value={from} disabled={guest} onChange={(e) => setTime("from", (e.currentTarget as HTMLInputElement).value)} />
              </label>
              <label class="phone-time">
                <span class="field-label">{tp("ph.quiet_to")}</span>
                <input class="search-input phone-time-input" type="time" value={to} disabled={guest} onChange={(e) => setTime("to", (e.currentTarget as HTMLInputElement).value)} />
              </label>
            </div>
          )}
        </div>
        <div class="field">
          <span class="field-label">{tp("ph.lock")}</span>
          <p class="group-text">{inApp ? tp("ph.lock_body") : tp("ph.lock_app")}</p>
          {inApp && (
            <Segmented<"off" | "on"> label={tp("ph.lock")} value={p.lock ? "on" : "off"} choices={onOff} onChange={(v) => void save({ ...p, lock: v === "on" })} />
          )}
        </div>
        {guest && <p class="group-text">{tp("ph.guest_no_prefs")}</p>}
        {saved.value !== "" && (
          <p class={`phone-line${saved.value === "failed" ? " is-error" : ""}`} role="status">
            {saved.value === "saved" ? tp("ph.saved") : tp("ph.save_failed")}
          </p>
        )}
      </section>

      <section class="group phone-danger">
        <Button kind="danger" needsLink onClick={() => (forgetOpen.value = true)}>
          {tp("ph.forget")}
        </Button>
      </section>
      {forgetOpen.value && <ForgetSheet />}
    </div>
  );
}

export function ThisPhone() {
  useEffect(() => {
    saved.value = "";
    void read();
  }, []);
  const s = load.value;
  const d = info.value;
  return (
    <SubView title={tp("ph.title")} name="phone">
      {s === "loading" && (
        <div aria-busy="true" aria-label={tp("ph.loading")}>
          <SkeletonRows rows={6} mono={false} />
        </div>
      )}
      {s === "unpaired" && <p class="empty">{tp("ph.not_paired")}</p>}
      {s === "failed" && (
        <div class="phone-line is-error" role="alert">
          <span>✗ {tp("ph.failed")}</span>{" "}
          <Button small needsLink onClick={() => void read()}>
            {tp("ph.retry")}
          </Button>
        </div>
      )}
      {s === "ready" && d && <Body d={d} />}
    </SubView>
  );
}
