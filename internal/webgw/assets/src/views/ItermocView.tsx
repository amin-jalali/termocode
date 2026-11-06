// Itermoc (WEBP-ITERMOC): the laptop's AI setup, read only: its agent
// profiles, AI providers and model slots, AI services and outside
// agents (itermoc.state). A model slot shows only whether its key is
// there; the key itself never reaches the page. Every change is made on
// the laptop: a profile is used through ai.use_profile, and each setup
// form (Add AI, model slots, profiles) opens on the laptop through
// itermoc.ask, after the question here (Cancel first) and the laptop's
// card. The desktop shows it in the side bar, the phone on More.
// #/more/itermoc
import { Fragment, h, type ComponentChildren } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { signal } from "@preact/signals";
import type { Itermoc, ItermocProfile, ItermocSlot } from "../../../../webproto/proto";
import { hubSays } from "../lib/ai";
import { go } from "../lib/router";
import { link, live } from "../session";
import { t, tn, type StringKey } from "../strings";
import { AskLaptop } from "../ui/AskLaptop";
import { Cod } from "../ui/icons";
import { View } from "./View";

/** ImSection is a section of the view, by the laptop's names. */
export type ImSection = "profiles" | "models" | "services" | "agents";

/** ImAction is a laptop setup form itermoc.ask may open. */
export type ImAction = "ai-add" | "ai-model-slot-add" | "ai-models-edit" | "ai-profile-edit" | "ai-profile-new" | "ai-profile-form";

const ACTION_LABEL: Record<ImAction, StringKey> = {
  "ai-add": "im.add_ai",
  "ai-model-slot-add": "im.add_slot",
  "ai-models-edit": "im.edit_slots",
  "ai-profile-edit": "im.edit_profiles",
  "ai-profile-new": "im.new_profile",
  "ai-profile-form": "im.edit_profile",
};

type Ask = { kind: "form"; action: ImAction } | { kind: "use"; name: string } | { kind: "next" };

/** imTarget is the section a command asked for; imAsk the question it
 * asked (the view shows both once it has the laptop's state). */
const imTarget = signal<ImSection | null>(null);
const imAsk = signal<Ask | null>(null);

/** openItermoc shows the view at a section, with a question if asked. */
export function openItermoc(sec: ImSection | null, ask?: Ask): void {
  imTarget.value = sec;
  imAsk.value = ask ?? null;
  go({ tab: "more", rest: "itermoc", query: {} });
}

/** askLaptopForm opens the view and asks to open a laptop setup form. */
export function askLaptopForm(action: ImAction): void {
  openItermoc(action.startsWith("ai-profile") ? "profiles" : "models", { kind: "form", action });
}

/** nextProfile is the profile after the one in use that the laptop
 * trusts here (the laptop's AI: Next Profile), or null. */
function nextProfile(ps: ItermocProfile[]): string | null {
  const at = ps.findIndex((p) => p.active);
  for (let i = 1; i <= ps.length; i++) {
    const p = ps[(at + i) % ps.length];
    if (!p.active && p.trust === "use") return p.name;
  }
  return null;
}

const KEY_LABEL: Record<ItermocSlot["hasKey"], StringKey> = { ok: "im.key_ok", missing: "im.key_missing", none: "im.key_none", unset: "im.key_unset" };

function KeyMark({ slot }: { slot: ItermocSlot }) {
  const label = t(KEY_LABEL[slot.hasKey]);
  const ok = slot.hasKey === "ok" || slot.hasKey === "none";
  return (
    <span class={`im-key${ok ? " is-ok" : " is-off"}`} data-key={slot.hasKey} role="img" aria-label={label} title={label}>
      {ok ? "✓" : "✗"}
    </span>
  );
}

function Section({ sec, title, children, acts }: { sec: ImSection; title: string; children: ComponentChildren; acts?: ComponentChildren }) {
  const target = imTarget.value === sec;
  const ref = useRef<HTMLElement | null>(null);
  useEffect(() => {
    if (target) ref.current?.scrollIntoView?.({ block: "start" });
  }, [target]);
  return (
    <section class={`im-sec${target ? " is-target" : ""}`} data-sec={sec} ref={ref} aria-label={title}>
      <h2 class="im-sec-title">{title}</h2>
      {children}
      {acts && <div class="im-acts">{acts}</div>}
    </section>
  );
}

// form, not action: a vnode prop named action is checked as a URL (lib/safety.ts).
function FormButton({ form, onAsk }: { form: ImAction; onAsk: (a: Ask) => void }) {
  return (
    <button type="button" class="btn btn-neutral im-act" data-act={form} onClick={() => onAsk({ kind: "form", action: form })}>
      {t(ACTION_LABEL[form])}
    </button>
  );
}

const SOURCE: Record<ItermocProfile["source"], StringKey> = { builtin: "im.src_builtin", user: "im.src_user", project: "im.src_project" };

export function ItermocView() {
  const [st, setSt] = useState<Itermoc | null>(null);
  const [err, setErr] = useState("");
  const [again, setAgain] = useState(0);
  const on = live.value;
  useEffect(() => {
    if (!on) return;
    let stop = false;
    link.request("itermoc.state").then(
      (r) => {
        if (stop) return;
        setErr("");
        setSt(r as Itermoc);
      },
      (e: unknown) => !stop && setErr(hubSays(e)),
    );
    return () => {
      stop = true;
    };
  }, [on, again]);
  const canAct = link.meta.value?.role === "controller";
  const ask = canAct ? imAsk.value : null;
  // AI: Next Profile: the next profile is known once the state is here.
  useEffect(() => {
    if (imAsk.peek()?.kind !== "next" || !st) return;
    const name = nextProfile(st.profiles);
    imAsk.value = name ? { kind: "use", name } : null;
    if (!name) setErr(t("im.no_next"));
  }, [st, ask]);
  const setAsk = (a: Ask | null) => (imAsk.value = a);
  return (
    <View
      tab="more"
      name="itermoc"
      title={t("im.title")}
      actions={
        <button type="button" class="tb tb-icon" data-act="refresh" aria-label={t("ctr.refresh")} title={t("ctr.refresh")} onClick={() => setAgain(again + 1)}>
          <Cod name="refresh" />
        </button>
      }
    >
      {err && <p class="empty refused">{err}</p>}
      {!st ? (
        !err && <p class="empty">{t("im.loading")}</p>
      ) : (
        <div class="im">
          {st.problems ? <p class="view-note">{t("im.problems", { n: st.problems })}</p> : null}
          <Section
            sec="profiles"
            title={t("im.profiles")}
            acts={
              canAct && (
                <>
                  <FormButton form="ai-profile-form" onAsk={setAsk} />
                  <FormButton form="ai-profile-new" onAsk={setAsk} />
                  <FormButton form="ai-profile-edit" onAsk={setAsk} />
                </>
              )
            }
          >
            <ul class="im-list">
              {st.profiles.map((p) => (
                <li key={p.name} class={`im-row im-profile${p.active ? " is-on" : ""}`} data-name={p.name}>
                  <bdi class="im-name" dir="auto">
                    {p.title || p.name}
                  </bdi>
                  <span class="im-desc">
                    {[p.title ? p.name : "", t(SOURCE[p.source]), p.active ? t("ai.current") : p.trust !== "use" ? t("ai.trust_laptop") : ""].filter(Boolean).join(" · ")}
                  </span>
                  {canAct && !p.active && p.trust === "use" && (
                    <button type="button" class="tb im-use" data-act="use" onClick={() => setAsk({ kind: "use", name: p.name })}>
                      {t("im.use")}
                    </button>
                  )}
                </li>
              ))}
            </ul>
          </Section>
          <Section
            sec="models"
            title={t("im.models")}
            acts={
              canAct && (
                <>
                  <FormButton form="ai-add" onAsk={setAsk} />
                  <FormButton form="ai-model-slot-add" onAsk={setAsk} />
                  <FormButton form="ai-models-edit" onAsk={setAsk} />
                </>
              )
            }
          >
            {st.providers.length === 0 ? (
              <p class="empty">{t("im.no_providers")}</p>
            ) : (
              <ul class="im-list">
                {st.providers.map((p) => (
                  <li key={p.kind} class="im-row im-provider" data-kind={p.kind}>
                    <bdi class="im-name" dir="auto">
                      {p.name}
                    </bdi>
                    <span class="im-desc">{[p.label, p.cost, tn("im.models_n", p.models), p.signedOut ? t("im.signed_out") : ""].filter(Boolean).join(" · ")}</span>
                  </li>
                ))}
              </ul>
            )}
            <h3 class="im-sub">{t("im.slots")}</h3>
            <ul class="im-list">
              {st.slots.map((s) => (
                <li key={s.name} class="im-row im-slot" data-name={s.name}>
                  <KeyMark slot={s} />
                  <bdi class="im-name" dir="ltr">
                    {s.name}
                  </bdi>
                  <bdi class="im-desc" dir="ltr">
                    {[s.provider, s.model].filter(Boolean).join(" · ")}
                  </bdi>
                </li>
              ))}
            </ul>
          </Section>
          <Section sec="services" title={t("im.services")}>
            {st.services.length === 0 ? (
              <p class="empty">{t("im.no_services")}</p>
            ) : (
              <ul class="im-list">
                {st.services.map((s) => (
                  <li key={s.name} class="im-row im-service" data-name={s.name}>
                    <bdi class="im-name" dir="ltr">
                      {s.name}
                    </bdi>
                    <span class="im-desc">{[s.state, tn("im.tools_n", s.tools)].join(" · ")}</span>
                  </li>
                ))}
              </ul>
            )}
          </Section>
          <Section sec="agents" title={t("im.agents")}>
            <p class="im-note">{t(st.listening ? "mcp.listening" : "im.not_listening")}</p>
            {st.listening && st.agents.length === 0 && <p class="empty">{t("im.no_agents")}</p>}
            <ul class="im-list">
              {st.agents.map((a, i) => (
                <li key={i} class="im-row im-agent">
                  <bdi class="im-name" dir="auto">
                    {a.name}
                  </bdi>
                  <span class="im-desc">{t("im.agent_desc", { since: a.since, n: a.calls })}</span>
                </li>
              ))}
            </ul>
          </Section>
        </div>
      )}
      {ask?.kind === "form" && (
        <AskLaptop
          label={t(ACTION_LABEL[ask.action])}
          text={t("im.body_ask", { name: t(ACTION_LABEL[ask.action]) })}
          send={() => link.request("itermoc.ask", { action: ask.action })}
          onDone={() => setAsk(null)}
        />
      )}
      {ask?.kind === "use" && (
        <AskLaptop
          label={t("ai.profile")}
          text={t("ai.use_profile", { name: ask.name })}
          send={() => link.request("ai.use_profile", { name: ask.name })}
          onDone={() => setAsk(null)}
          onConfirmed={() => setTimeout(() => setAgain((n) => n + 1), 500)}
        />
      )}
    </View>
  );
}
