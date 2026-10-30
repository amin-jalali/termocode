// An image from the hub's bytes (WEB-57): loading, then the picture from
// a blob: URL (revoked when it goes), or the hub's words when it cannot be
// shown.
import { h } from "preact";
import { useEffect, useState } from "preact/hooks";
import { imageURL, type ImageOp } from "../lib/image";
import { ReqError } from "../net/link";
import { t } from "../strings";
import { errText } from "./CardSheet";

function imgErr(e: unknown): string {
  if (e instanceof ReqError && e.code === "limit") return t("img.too_big");
  if (e instanceof ReqError && e.code === "bad_request") return t("img.not_image");
  if (!(e instanceof ReqError)) return t("img.not_image");
  return errText(e);
}

export function BlobImg({ op, args, alt, cls }: { op: ImageOp; args: Record<string, unknown>; alt: string; cls?: string }) {
  const [url, setUrl] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const key = op + JSON.stringify(args);
  useEffect(() => {
    let dead = false;
    let made: string | null = null;
    setUrl(null);
    setErr(null);
    imageURL(op, args).then(
      (u) => {
        if (dead) URL.revokeObjectURL(u);
        else setUrl((made = u));
      },
      (e: unknown) => !dead && setErr(imgErr(e)),
    );
    return () => {
      dead = true;
      if (made) URL.revokeObjectURL(made);
    };
  }, [key]);
  if (err !== null)
    return (
      <p class="img-err" role="status" dir="auto">
        {err}
      </p>
    );
  if (!url)
    return (
      <p class="img-wait" role="status">
        {t("img.loading")}
      </p>
    );
  return <img class={`blob-img${cls ? " " + cls : ""}`} src={url} alt={alt} />;
}
