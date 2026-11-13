# Persian community drafts

Places: Virgool (article), LinkedIn (post), Telegram groups for Go, Linux
and Neovim, POSSF Discord. Test every account before launch week: some
services restrict accounts from Iran.

## Short post (LinkedIn / Telegram)

termocode یک IDE متن‌باز است که کامل داخل ترمینال اجرا می‌شود.

- ویرایشگر آن Neovim واقعی است (`nvim --embed`)؛ پیکربندی و کلیدهای خودتان کار می‌کنند.
- گیت، دیباگر، تست، داکر، کوبرنتیز و دیتابیس در یک برنامهٔ Go.
- دستیار هوش مصنوعی هر تغییر را به شکل diff نشان می‌دهد و شما تک‌تک قسمت‌ها را می‌پذیرید یا رد می‌کنید؛ با Ollama و LM Studio هم کار می‌کند، یعنی بدون اینترنت و بدون کلید API خارجی هم قابل استفاده است.
- اتصال SSH که قطع شود، کار شما نمی‌میرد: ویرایشگر، ترمینال‌ها و کارهای در حال اجرا روی سرور می‌مانند و با اجرای دوبارهٔ `termocode` برمی‌گردند.
- متن فارسی و عربی در چت و پیش‌نمایش Markdown راست‌به‌چپ و درست نمایش داده می‌شود.

نسخهٔ اولیه است: لینوکس، macOS و Termux (ویندوز فعلاً نه؛ از WSL استفاده کنید).

نصب:

    curl -fsSL https://raw.githubusercontent.com/amin-jalali/termocode/main/scripts/install.sh | sh

گیت‌هاب: <لینک مخزن>
خوشحال می‌شوم اگر امتحان کنید و ایرادها را بگویید.

## Virgool article outline

1. Why a terminal IDE (SSH, servers, low-RAM machines, Termux).
2. Real Neovim inside: what that means for your own config.
3. Sessions that outlive the connection (a short story: drop the link,
   come back, same screen).
4. AI you control: per-hunk review, undo a whole turn, local models.
5. Persian and Arabic text in the chat and in Markdown.
6. Install, what is rough, how to report a bug.

Keep the numbers to those in docs/benchmarks.md. Add 2 or 3 screenshots
from `assets/screenshots/` and the hero GIF.
