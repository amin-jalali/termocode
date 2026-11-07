// Projects list (MOBUX-6, plan §0 "Projects"): one card per project the
// laptop knows, what needs the user first. A card shows the name, the
// state as a colour and a shape (● ◐ ✓ ◌), the open task count and the
// CI result; the whole card is one 44 px target that opens the project
// (its page is MOBUX-7; an asleep project opens after a laptop card,
// MOB-51). Adding a folder stays on the laptop. Phone layout only: it is
// part of the phone's lazy chunk, never app.js.
import { h } from "preact";
import "../styles/phone-projects.css";
import { useMemo } from "preact/hooks";
import { store } from "../session";
import { t, tn, type StringKey } from "../strings";
import { TopicBody } from "../views/Placeholder";
import { CI_LOOK, projectCards, type CI, type ProjectCard } from "./projects";
import { StatusMark } from "./Mark";
import { STATUS } from "./status";

const CI_WORD: Readonly<Record<CI, StringKey>> = {
  passed: "ph.proj.ci_passed",
  failed: "ph.proj.ci_failed",
  running: "ph.proj.ci_running",
};

function CardFacts({ p }: { p: ProjectCard }) {
  return (
    <span class="ph-proj-facts" dir="ltr">
      {p.waiting > 0 && <span class="ph-proj-fact">{t("ph.proj.waiting", { n: p.waiting })}</span>}
      {p.working > 0 && <span class="ph-proj-fact">{t("ph.proj.working", { n: p.working })}</span>}
      {p.tasks !== undefined && <span class="ph-proj-fact">{tn("ph.proj.tasks", p.tasks)}</span>}
      {p.ci && (
        <span class={`ph-proj-fact ph-proj-ci is-${p.ci}`}>
          <StatusMark s={CI_LOOK[p.ci]} hidden />
          {t(CI_WORD[p.ci])}
        </span>
      )}
    </span>
  );
}

/** label is what a screen reader says for the whole card. */
function label(p: ProjectCard): string {
  const parts = [p.name, t(STATUS[p.status].label)];
  if (p.here) parts.push(t("ph.proj.here"));
  if (p.waiting > 0) parts.push(t("ph.proj.waiting", { n: p.waiting }));
  if (p.working > 0) parts.push(t("ph.proj.working", { n: p.working }));
  if (p.tasks !== undefined) parts.push(tn("ph.proj.tasks", p.tasks));
  if (p.ci) parts.push(t(CI_WORD[p.ci]));
  return parts.join(", ");
}

export function ProjectCardView({ p, onOpen }: { p: ProjectCard; onOpen: (p: ProjectCard) => void }) {
  return (
    <li class="ph-proj-item">
      <button
        type="button"
        class={`ph-proj st-${p.status}${p.here ? " is-here" : ""}`}
        data-key={p.id}
        aria-label={label(p)}
        onClick={() => onOpen(p)}
      >
        <StatusMark s={p.status} live={p.status === "working"} hidden />
        <span class="ph-proj-main">
          <span class="ph-proj-name" dir="auto">
            {p.name}
            {p.here && <span class="ph-proj-here">{t("ph.proj.here")}</span>}
          </span>
          {p.where && (
            <span class="ph-proj-where" dir="ltr">
              {p.where}
            </span>
          )}
          <CardFacts p={p} />
        </span>
        <span class="ph-proj-go" aria-hidden="true">
          ›
        </span>
      </button>
    </li>
  );
}

/** ProjectsList draws the projects topic as cards; onOpen gets the tapped one. */
export function ProjectsList({ onOpen }: { onOpen: (p: ProjectCard) => void }) {
  const data = store.topic("projects").value?.data;
  const cards = useMemo(() => projectCards(data), [data]);
  return (
    <section class="ph-projs" aria-label={t("ph.proj.title")}>
      <TopicBody topic="projects" empty={<p>{t("ph.proj.none")}</p>}>
        {cards.length === 0 ? (
          <div class="empty">
            <p>{t("ph.proj.none")}</p>
          </div>
        ) : (
          <ul class="ph-proj-list" aria-label={t("ph.proj.title")}>
            {cards.map((p) => (
              <ProjectCardView key={p.id} p={p} onOpen={onOpen} />
            ))}
          </ul>
        )}
      </TopicBody>
    </section>
  );
}
