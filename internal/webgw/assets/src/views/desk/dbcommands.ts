// The Databases commands of the desktop's command list (the laptop
// palette's "Databases: …" commands). Each opens what the view's buttons
// open; a change asks the laptop there.
import type { Command } from "../../lib/commands";
import { activeDesk } from "../../lib/deskeditor";
import { DBH_TAB } from "../../lib/vtabs";
import { showFile } from "../../lib/workbench";
import { openSide } from "../../ui/ActivityBar";
import { exportNow, pendingOf, queryRun } from "./DBTabs";
import { openConfig } from "./DeskDB";
import { canAct, dbFilter, dbForm, type DBForm } from "./dbstate";

const form = (f: DBForm) => {
  openSide("db");
  dbForm.value = f;
};

export function dbCommands(): Command[] {
  return [
    { id: "dbNewConsole", label: "dbv.new_console", run: () => form({ kind: "pick-console" }) },
    { id: "dbRunFile", label: "dbv.run_file", when: () => canAct() && activeDesk.peek() !== null, run: () => form({ kind: "pick-file" }) },
    { id: "dbFilter", label: "dbv.filter_cmd", run: () => (openSide("db"), (dbFilter.value = dbFilter.peek() ?? "")) },
    { id: "dbAddConnection", label: "dbv.add_cmd", when: canAct, run: () => form({ kind: "add" }) },
    { id: "dbConfigure", label: "dbv.configure", when: canAct, run: openConfig },
    { id: "dbHistory", label: "dbv.history_cmd", when: canAct, run: () => showFile(DBH_TAB) },
    { id: "dbExport", label: "dbv.export_cmd", when: () => exportNow !== null, run: () => exportNow?.({ x: innerWidth / 2 - 80, y: innerHeight / 3 }) },
    { id: "dbResultPin", label: "dbv.pin_cmd", when: () => queryRun !== null, run: () => queryRun?.pin() },
    { id: "dbSubmitChanges", label: "dbv.submit_cmd", when: () => (pendingOf?.n ?? 0) > 0, run: () => pendingOf?.submit() },
    { id: "dbRevertChanges", label: "dbv.revert_cmd", when: () => (pendingOf?.n ?? 0) > 0, run: () => pendingOf?.revert() },
  ];
}
