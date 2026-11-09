// The chat's profile and model picker under the input (desktop,
// WEBP-3): the laptop's active agent profile and model, each a button
// with a list. The web cannot change them itself (web design 3.3):
// picking one asks the laptop, which switches only after its card
// (Cancel first). A project profile the laptop has not trusted shows,
// but only the laptop can trust it. In aictx.js (look.tsx gives it to
// the composer), so the phone's page does not carry it.
import type { MenuEntry } from "../ui/ContextMenu";
import { Fragment, H, h } from "./jsx";

type Ask = { kind: "profile" | "model"; name: string };

export function Picker() {
  const [ask, setAsk] = H.useState<Ask | null>(null);
  const s = H.aiSnap();
  if (!s || !s.configured) return null;
  const { t, Cod } = H;
  const canAsk = H.link.meta.value?.role === "controller";
  const list = (kind: Ask["kind"]) => (e: MouseEvent) => {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    const items: MenuEntry[] =
      kind === "profile"
        ? s.profiles.map((p) => ({
            label: p.name,
            keys: p.name === s.profile ? t("ai.current") : p.trust !== "use" ? t("ai.trust_laptop") : undefined,
            disabled: !canAsk || p.name === s.profile || p.trust !== "use",
            run: () => setAsk({ kind, name: p.name }),
          }))
        : s.models.map((m) => ({ label: m, keys: m === s.model ? t("ai.current") : undefined, disabled: !canAsk || m === s.model, run: () => setAsk({ kind, name: m }) }));
    H.openMenu(r.left, r.bottom + 2, kind === "profile" ? t("ai.profile") : t("ai.model"), items);
  };
  const hint = s.profiles.find((p) => p.name === s.profile)?.hint;
  return (
    <>
      <button type="button" class="ai-pick" data-pick="profile" aria-haspopup="menu" title={hint ? `${t("ai.profile")}: ${hint}` : t("ai.profile")} onClick={list("profile")}>
        <Cod name="sparkle" />
        <span class="ai-pick-name">{s.profile}</span>
        <Cod name="chevron-down" />
      </button>
      {s.models.length > 0 && (
        <button type="button" class="ai-pick" data-pick="model" aria-haspopup="menu" title={s.provider ? `${t("ai.model")} (${s.provider})` : t("ai.model")} onClick={list("model")}>
          <span class="ai-pick-name">{s.model}</span>
          <Cod name="chevron-down" />
        </button>
      )}
      {ask && (
        <H.AskLaptop
          label={ask.kind === "profile" ? t("ai.profile") : t("ai.model")}
          text={t(ask.kind === "profile" ? "ai.use_profile" : "ai.use_model", { name: ask.name })}
          send={() => H.link.request(ask.kind === "profile" ? "ai.use_profile" : "ai.use_model", { name: ask.name })}
          onDone={() => setAsk(null)}
        />
      )}
    </>
  );
}
