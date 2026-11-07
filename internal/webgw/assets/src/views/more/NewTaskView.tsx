// New task on a phone (CMP-49): More → New task (#/more/new-task). The
// form starts a background task on the laptop: a new worktree with an
// agent that gets the task. Back to More when it started.
import { h } from "preact";
import { go } from "../../lib/router";
import { t } from "../../strings";
import { NewTaskForm } from "../NewTask";
import { SubView } from "./SubView";

export function NewTaskView() {
  return (
    <SubView title={t("nt.title")} name="new-task">
      <NewTaskForm onDone={() => go({ tab: "more", rest: "", query: {} })} />
    </SubView>
  );
}
