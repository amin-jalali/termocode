// Schema guard for messages from the hub. The Go side (internal/webproto)
// is the source of truth; proto.d.ts gives the types and the lists below
// are kept equal to webproto by internal/webgw/assets/guard_test.go.
// Readers ignore unknown fields (newer hubs may add some) but refuse a
// message whose known fields have the wrong shape, an unknown kind, or a
// topic the protocol does not have.
import type {
  CloseCode,
  Delta,
  ErrCode,
  ErrMsg,
  Event,
  Meta,
  Pong,
  Res,
  Resync,
  ServerBye,
  ServerMsg,
  Snap,
} from "../../../../webproto/proto";

export const PROTOCOL_VERSION = 2;
export const SUBPROTOCOL = "termocode.v2";

export const TOPIC_NAMES = [
  "status", "toasts", "chats", "chat", "cards", "tree", "bufs", "buf", "terms", "term",
  "git", "tasks", "runs", "db", "containers", "mcp", "settings", "diagnostics",
] as const;

export const KEYED_TOPICS = ["buf", "term"] as const;

export const SERVER_KINDS = ["bye", "delta", "err", "ev", "meta", "pong", "res", "resync", "snap"] as const;

export const ERR_CODES = [
  "bad_request", "forbidden", "limit", "conflict", "gone", "busy", "not_found", "internal",
] as const;

export const CLOSE_CODES = [
  1000, 1003, 1008, 1009, 4001, 4002, 4003, 4004, 4005, 4006, 4007, 4008, 4009, 4010, 4011, 4012, 4013,
] as const;

export class ProtoError extends Error {}

type Obj = Record<string, unknown>;

const topicNames: ReadonlySet<string> = new Set(TOPIC_NAMES);
const keyedTopics: ReadonlySet<string> = new Set(KEYED_TOPICS);
const errCodes: ReadonlySet<string> = new Set(ERR_CODES);
const closeCodes: ReadonlySet<number> = new Set(CLOSE_CODES);

function fail(why: string): never {
  throw new ProtoError(why);
}

function isObj(v: unknown): v is Obj {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function str(o: Obj, k: string): string {
  const v = o[k];
  if (typeof v !== "string") fail(`${k}: want a string`);
  return v;
}

function uint(o: Obj, k: string, min = 0): number {
  const v = o[k];
  if (typeof v !== "number" || !Number.isSafeInteger(v) || v < min) fail(`${k}: want an integer >= ${min}`);
  return v;
}

function optUint(o: Obj, k: string, min = 0): void {
  if (o[k] !== undefined) uint(o, k, min);
}

/** isTopic reports whether s is a topic of the protocol: "status", "buf:12". */
export function isTopic(s: string): boolean {
  const i = s.indexOf(":");
  const name = i < 0 ? s : s.slice(0, i);
  if (!topicNames.has(name)) return false;
  if (!keyedTopics.has(name)) return i < 0;
  return i > 0 && /^[1-9][0-9]{0,9}$/.test(s.slice(i + 1));
}

/** topicName is the name part of a topic: "buf:12" → "buf". */
export function topicName(s: string): string {
  const i = s.indexOf(":");
  return i < 0 ? s : s.slice(0, i);
}

function topic(o: Obj): string {
  const t = str(o, "topic");
  if (!isTopic(t)) fail(`topic: unknown ${JSON.stringify(t.slice(0, 40))}`);
  return t;
}

/** checkServerMsg checks one decoded message and returns it typed. */
export function checkServerMsg(v: unknown): ServerMsg {
  if (!isObj(v)) fail("message: want an object");
  switch (v.t) {
    case "meta": {
      if (uint(v, "v") !== PROTOCOL_VERSION) fail("meta.v: unsupported version");
      str(v, "conn");
      const role = str(v, "role");
      if (role !== "viewer" && role !== "controller") fail("meta.role: unknown");
      str(v, "device");
      uint(v, "idle_s");
      uint(v, "max_s");
      uint(v, "viewers");
      if (!isObj(v.ui)) fail("meta.ui: want an object");
      str(v.ui, "theme");
      if (!Array.isArray(v.caps) || !v.caps.every((c) => typeof c === "string")) fail("meta.caps: want strings");
      return v as unknown as Meta;
    }
    case "snap":
      topic(v);
      uint(v, "seq");
      if (!("data" in v)) fail("snap.data: missing");
      return v as unknown as Snap;
    case "delta":
      topic(v);
      uint(v, "seq", 1);
      if (!("data" in v)) fail("delta.data: missing");
      return v as unknown as Delta;
    case "resync":
      topic(v);
      return v as unknown as Resync;
    case "res":
      uint(v, "id", 1);
      return v as unknown as Res;
    case "err":
      optUint(v, "id", 1);
      if (!errCodes.has(str(v, "code"))) fail("err.code: unknown");
      str(v, "msg");
      return v as unknown as ErrMsg & { code: ErrCode };
    case "ev":
      str(v, "kind");
      return v as unknown as Event;
    case "pong":
      str(v, "n");
      return v as unknown as Pong;
    case "bye":
      if (!closeCodes.has(uint(v, "code"))) fail("bye.code: unknown");
      return v as unknown as ServerBye & { code: CloseCode };
    default:
      return fail("t: unknown kind");
  }
}

/** decodeServerMsg parses one text frame and checks it. */
export function decodeServerMsg(text: string): ServerMsg {
  let v: unknown;
  try {
    v = JSON.parse(text);
  } catch {
    fail("frame: not JSON");
  }
  return checkServerMsg(v);
}
