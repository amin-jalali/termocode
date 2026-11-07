// The comment box (CMP-88): a viewer of a shared chat cannot send to the
// AI, so under the chat it gets this box instead of the composer. A
// comment goes to the laptop (chat.comment), which keeps it beside the
// chat and tells the user; the AI never reads it unless the user adds it
// to a message. The text stays while offline or on a refusal.
import { h } from "preact";
import { useState } from "preact/hooks";
import { OfflineError } from "../net/link";
import { link, live } from "../session";
import { toast } from "../lib/toasts";
import { t } from "../strings";
import { Button } from "../ui/Button";
import { errText } from "../ui/CardSheet";

/** MAX_COMMENT matches webproto.MaxChatComment (bytes). */
const MAX_COMMENT = 2000;

export function CommentBox({ chat }: { chat: string }) {
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const tooLong = new TextEncoder().encode(text).length > MAX_COMMENT;

  const send = () => {
    const body = text.trim();
    if (!live.peek() || sending || !body || !chat || tooLong) return;
    setSending(true);
    link.request("chat.comment", { id: chat, text: body }).then(
      () => {
        setSending(false);
        setNote(null);
        setText("");
        toast("ok", t("chat.comment_sent"));
      },
      (e: unknown) => {
        setSending(false);
        if (e instanceof OfflineError) return; // the button shows Offline; the text stays
        setNote(errText(e));
      },
    );
  };

  return (
    <div class="composer chat-comment">
      <p class="composer-note" dir="auto">
        {note ?? t("chat.comment_note")}
      </p>
      <div class="composer-row">
        <textarea
          class="composer-box"
          dir="auto"
          rows={2}
          value={text}
          placeholder={t("chat.comment_placeholder")}
          aria-label={t("chat.comment_placeholder")}
          onInput={(e) => {
            setText((e.currentTarget as HTMLTextAreaElement).value);
            if (note) setNote(null);
          }}
        />
        <span class="send">
          <Button kind="primary" needsLink disabled={sending || !text.trim() || !chat || tooLong} onClick={send}>
            {t("chat.comment_send")}
          </Button>
        </span>
      </div>
    </div>
  );
}
