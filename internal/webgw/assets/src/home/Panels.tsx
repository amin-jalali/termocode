// The Panels drawer of the Home (MOB-4, ≡): the views the chips open,
// and the old phone tabs (Files, Terminal, Git, More, This phone), so
// nothing is lost. A choice here says whether the page starts on the
// Home or on the tab bar (This phone, kept on this device).
import { h, H } from "./jsx";
import { hs } from "./strings";
import type { ChipDef } from "./chips";
import type { Tab } from "../state/route";

const TABS: readonly { tab: Tab; label: "panels.files" | "panels.term" | "panels.git" | "panels.more" }[] = [
  { tab: "files", label: "panels.files" },
  { tab: "term", label: "panels.term" },
  { tab: "git", label: "panels.git" },
  { tab: "more", label: "panels.more" },
];

export function Panels({ views, onView, onAll, onClose }: { views: readonly ChipDef[]; onView: (c: ChipDef) => void; onAll: () => void; onClose: () => void }) {
  const S = H.Sheet;
  const start = H.phoneHome.value === "chat";
  const pick = (f: () => void) => {
    onClose();
    f();
  };
  return (
    <S onLater={onClose} label={hs("panels")}>
      <div class="home-panels">
        <h2 class="home-panels-title">{hs("panels")}</h2>
        <section aria-labelledby="hp-views">
          <h3 class="home-group-title" id="hp-views">
            {hs("panels.views")}
          </h3>
          <ul class="home-rows">
            {views.map((c) => (
              <li key={c.id}>
                <button type="button" class="home-row" onClick={() => pick(() => onView(c))}>
                  <span class="home-row-main">
                    <span class="home-row-title">{hs(c.label)}</span>
                  </span>
                </button>
              </li>
            ))}
            <li>
              <button type="button" class="home-row" onClick={() => pick(onAll)}>
                <span class="home-row-main">
                  <span class="home-row-title">{hs("views.all")}</span>
                </span>
              </button>
            </li>
          </ul>
        </section>
        <section aria-labelledby="hp-tabs">
          <h3 class="home-group-title" id="hp-tabs">
            {hs("panels")}
          </h3>
          <ul class="home-rows">
            {TABS.map((x) => (
              <li key={x.tab}>
                <button type="button" class="home-row" onClick={() => pick(() => H.goTab(x.tab))}>
                  <span class="home-row-main">
                    <span class="home-row-title">{hs(x.label)}</span>
                  </span>
                </button>
              </li>
            ))}
            <li>
              <button type="button" class="home-row" onClick={() => pick(() => H.goTab("more"))}>
                <span class="home-row-main">
                  <span class="home-row-title">{hs("panels.phone")}</span>
                </span>
              </button>
            </li>
            <li>
              <button type="button" class="home-row" onClick={() => pick(() => H.goTab("chat"))}>
                <span class="home-row-main">
                  <span class="home-row-title">{hs("panels.tabs")}</span>
                </span>
              </button>
            </li>
          </ul>
        </section>
        <label class="home-check">
          <input type="checkbox" checked={start} onChange={(e) => H.setPhoneHome((e.currentTarget as HTMLInputElement).checked ? "chat" : "tabs")} />
          <span>{hs("panels.start")}</span>
        </label>
        <button type="button" class="btn home-panels-close" onClick={onClose}>
          {hs("panels.close")}
        </button>
      </div>
    </S>
  );
}
