// A page under More (Search, Tasks, Runs): the More frame with a back
// button to the list.
import { h, type ComponentChildren } from "preact";
import { layout } from "../../lib/layout";
import { go } from "../../lib/router";
import { t } from "../../strings";
import { View } from "../View";

export function SubView({ title, children, actions, name }: { title: string; children: ComponentChildren; actions?: ComponentChildren; name?: string }) {
  // The desktop has each of them in its activity bar: no way back to a list.
  const desk = layout.value === "desktop";
  return (
    <View
      tab="more"
      title={title}
      name={name}
      actions={actions}
      sub={
        desk ? undefined : (
          <button type="button" class="sub-back" onClick={() => go({ tab: "more", rest: "", query: {} })}>
            ‹ {t("back")}
          </button>
        )
      }
    >
      {children}
    </View>
  );
}
