// The phone views' one door (MOB-50): other projects' topics and requests
// go through it; the home project stays on the link and its main store.
// It is made when phone.js loads, so app.js and the desktop never carry
// it.
import { Door } from "../../net/door";
import { link, store } from "../../session";

export const door = new Door(link, store);
