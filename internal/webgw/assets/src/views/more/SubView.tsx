// A page under More (Search, Tasks, Runs): the More frame with a back
// button to the list.
import { h, type ComponentChildren } from "preact";
import { go } from "../../lib/router";
import { t } from "../../strings";
import { View } from "../View";

export function SubView({ title, children }: { title: string; children: ComponentChildren }) {
  return (
    <View
      tab="more"
      title={title}
      sub={
        <button type="button" class="sub-back" onClick={() => go({ tab: "more", rest: "", query: {} })}>
          ‹ {t("back")}
        </button>
      }
    >
      {children}
    </View>
  );
}
