// Images on the page (WEB-57): the hub sends an image's bytes (file.image,
// chat.image); the page shows them from a blob: URL it makes itself. Only
// the four raster types are drawn (the hub checked the bytes; the page
// checks the type again). Never a remote URL.
import type { ImageRes } from "../../../../webproto/proto";
import { link } from "../session";

const MIMES: ReadonlySet<string> = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"]);

export type ImageOp = "file.image" | "chat.image";

/** imageURL asks op for an image and returns a blob: URL of its bytes
 * (the caller revokes it). */
export function imageURL(op: ImageOp, args: Record<string, unknown>): Promise<string> {
  return link.request(op, args).then((data) => {
    const res = data as ImageRes;
    if (!MIMES.has(res.mime) || typeof res.data !== "string") throw new Error("not an image");
    const bin = atob(res.data);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    return URL.createObjectURL(new Blob([bytes], { type: res.mime }));
  });
}

/** IMAGE_EXT: names the file view shows as a picture. */
export const IMAGE_EXT = /\.(png|jpe?g|gif|webp)$/i;
