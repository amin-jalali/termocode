// Download and upload sheets (WEB-49): progress, Cancel, and the outcome.
// Saving over an existing file asks the laptop (web design 3.2).
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { toast } from "../../lib/toasts";
import { download, MAX_TRANSFER, saveBlob, TransferStopped, upload, type Upload } from "../../lib/transfer";
import { ReqError } from "../../net/link";
import { link } from "../../session";
import { t } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { Sheet } from "../../ui/Sheet";
import { gitFailed as failed } from "../git/errors";

const base = (p: string): string => p.slice(p.lastIndexOf("/") + 1);
const pct = (d: number, a: number): number => (a > 0 ? Math.floor((d * 100) / a) : 100);

function Progress({ label, done, all, onCancel }: { label: string; done: number; all: number; onCancel(): void }) {
  return (
    <div class="xfer" role="status">
      <p class="xfer-label" dir="auto">
        {label}
      </p>
      <progress class="xfer-bar" max={100} value={pct(done, all)} />
      <p class="xfer-pct">{t("xfer.percent", { n: pct(done, all) })}</p>
      <button type="button" class="btn btn-neutral btn-wide" onClick={onCancel}>
        {t("cancel")}
      </button>
    </div>
  );
}

export function DownloadSheet({ path, onDone }: { path: string; onDone(): void }) {
  const [p, setP] = useState({ done: 0, all: 0 });
  const stop = useRef(false);
  useEffect(() => {
    download(path, (done, all) => setP({ done, all }), () => stop.current).then(
      (b) => {
        saveBlob(b, base(path));
        toast("ok", t("xfer.downloaded", { name: base(path) }));
        onDone();
      },
      (e: unknown) => {
        if (e instanceof TransferStopped) toast("info", t("xfer.stopped"));
        else if (e instanceof ReqError || !(e instanceof Error) || e.message !== "changed") failed(e);
        else toast("warn", t("xfer.changed"));
        onDone();
      },
    );
    return () => {
      stop.current = true;
    };
  }, []);
  return (
    <Sheet onLater={() => (stop.current = true)} label={t("xfer.download")}>
      <Progress label={t("xfer.downloading", { name: base(path) })} done={p.done} all={p.all} onCancel={() => (stop.current = true)} />
    </Sheet>
  );
}

export function UploadSheet({ dir, onDone }: { dir: string; onDone(): void }) {
  const input = useRef<HTMLInputElement | null>(null);
  const [file, setFile] = useState<File | null>(null);
  const [p, setP] = useState({ done: 0, all: 0 });
  const [replace, setReplace] = useState<Upload | null>(null);
  const stop = useRef(false);
  const target = file ? (dir ? dir + "/" : "") + file.name : "";

  const send = (f: File) => {
    if (f.size > MAX_TRANSFER) {
      toast("warn", t("xfer.too_big"));
      return onDone();
    }
    setFile(f);
    const path = (dir ? dir + "/" : "") + f.name;
    upload(path, f, (done, all) => setP({ done, all }), () => stop.current)
      .then((u) => {
        if (u.exists) {
          setReplace(u);
          return;
        }
        return link.request("file.upload_save", { id: u.id }).then(
          () => {
            toast("ok", t("xfer.uploaded", { path }));
            onDone();
          },
          (e: unknown) => {
            if (e instanceof ReqError && e.code === "conflict") setReplace(u);
            else {
              failed(e);
              onDone();
            }
          },
        );
      })
      .catch((e: unknown) => {
        if (e instanceof TransferStopped) toast("info", t("xfer.stopped"));
        else failed(e);
        onDone();
      });
  };

  if (replace) {
    return (
      <AskLaptop
        label={t("xfer.upload")}
        text={t("xfer.replace_q", { path: target })}
        send={() => link.request("file.upload_replace", { id: replace.id })}
        onDone={() => {
          link.request("file.upload_cancel", { id: replace.id }).catch(() => undefined);
          onDone();
        }}
      />
    );
  }
  return (
    <Sheet
      onLater={() => {
        stop.current = true;
        if (!file) onDone();
      }}
      label={t("xfer.upload")}
    >
      {file ? (
        <Progress label={t("xfer.uploading", { name: file.name })} done={p.done} all={p.all} onCancel={() => (stop.current = true)} />
      ) : (
        <div class="xfer">
          <p class="xfer-label" dir="auto">
            {t("xfer.upload_to", { dir: dir || t("files.ws_root") })}
          </p>
          <p class="ask-note">{t("xfer.limit")}</p>
          <input
            ref={input}
            class="xfer-input"
            type="file"
            aria-label={t("xfer.choose")}
            onChange={(e) => {
              const f = (e.currentTarget as HTMLInputElement).files?.[0];
              if (f) send(f);
            }}
          />
          <div class="choices">
            <button type="button" class="btn btn-neutral" onClick={onDone}>
              {t("cancel")}
            </button>
            <button type="button" class="btn btn-primary" onClick={() => input.current?.click()}>
              {t("xfer.choose")}
            </button>
          </div>
        </div>
      )}
    </Sheet>
  );
}
