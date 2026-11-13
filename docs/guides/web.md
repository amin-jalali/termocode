# The web panel

The web panel is a second screen for the termocode you already run. It
opens in a browser tab. It shows the same chats, files, terminals and git
state as the terminal, and you can work in both at the same time. Nothing
is copied: the page shows what termocode shows, and what you do in the
page happens in termocode.

The web panel is **experimental**. It runs only when you open it.

!!! warning "Anyone who controls the browser tab can run any command as you"
    The page can type into your terminals and answer the AI's cards.
    Open the link only in a browser you trust, and stop the panel when
    you are done.

## Open it

In termocode, press `F1` and type `web`. **Web: Open in Browser** is
first. Press `Enter`. That is all: the panel starts and opens in your
browser. You do not need to change a setting or restart termocode.

Or, in a terminal in the folder where termocode runs:

```sh
termocode web                # open it in the browser now
termocode web status         # is it on, and until when?
termocode web stop
```

- **Web: Open in Browser** (also named **Web: Start Panel**) starts the
  panel and opens it. If it runs already, it opens a new link.
- **Web: Copy Link** copies a new link while the panel runs.
- **Web: Stop Panel** stops it. Every browser is disconnected at once.
  The web chip in the status bar does the same when you click it.

The panel listens only on this computer (`127.0.0.1`).

If no browser opens (for example, on a computer with no desktop),
termocode copies the link for you and shows it in a toast. Paste it into
a browser. Click the toast to copy it again.

If the panel runs already, `termocode web` opens a new link, like
**Web: Open in Browser**. From a script it only says that the panel runs.

When a script runs `termocode web` (not you, typing in a terminal),
termocode shows a card on the laptop first. **Not now** is first and is
what `Enter` and `Esc` do. `termocode web --yes` skips the card only when
your `config.json` has `"web_panel": "on"`.

To turn the panel off completely, add to your own `config.json` (see
[Config files](../reference/config-files.md)):

```json
{ "web_panel": "off" }
```

Settings apply when you save `config.json`. No restart is needed.

The link works **once**, for **5 minutes**. The page then gets its own
key, which lives only in that tab. A new tab needs a new link.

The panel stays up while termocode runs, used or not, so a phone that
was locked or out of reach connects again by itself. It stops only with
**Web: Stop Panel** or when you quit termocode. A browser's sign-in ends
after **15 minutes** with no use (`web_idle_minutes`) or **12 hours**
(`web_max_hours`); a paired device signs in again by itself.

Your work does not stop with it. Chats, terminals and runs go on in
termocode.

## What you can do in the page

| Tab | What it does |
|---|---|
| **Chat** | Read and write in the AI chat. Answers stream in. Answer the AI's cards. |
| **Files** | Browse the workspace, read files, change lines and send them. Each row's **⋯** makes a new file or folder, renames, deletes, downloads a file or uploads into a folder. |
| **Term** | Watch the terminals and type into them, with a key bar for Esc, Ctrl, Alt and arrows. |
| **Git** | See changes and diffs, stage and unstage, commit. The **⇅** button pushes, pulls, stashes and switches branch. |
| **More** | Search, **New task** (start an agent on the laptop in a new worktree; see [Worktrees](worktrees.md#start-a-new-task-from-the-phone-or-the-page)), tasks, runs, databases, containers, the MCP server, settings (read only), the theme, text direction and language. |

On a phone the tabs are at the bottom. On a wider screen the files and
the chat sit side by side.

An AI change card with more than one change has a **Keep change** box on
each change. Clear the boxes of the changes you do not want, then press
**Accept**: only the kept changes are written.

When you change a file in the page, the laptop shows a mark (**▍**) on
that line with **✎** and your device's name. It moves with your next
change and goes away after 30 seconds.

You and the laptop can edit one file at the same time. When someone types
on the laptop while you change other lines in the page, your change still
lands in the right place. Only when you both changed the same text does
the page say the file changed on the laptop; your text stays in the page
to check and send again.

## Use it from a remote machine: `termocode ssh --web`

When termocode runs on a server, open its panel on your own computer:

```sh
termocode ssh --web --dir ~/proj me@server
```

This is [`termocode ssh`](remote-ssh.md#reconnect-by-itself-termocode-ssh)
with one more step. termocode on the server starts the panel on a private
socket. A second `ssh`, with your own options and keys, forwards a port on
your computer to that socket. The link then opens in your browser here.

- The server opens **no network port**. Everything goes through your SSH
  connection.
- The panel must be turned on in the server's `config.json`:
  `"web_panel": "on"`.
- The second `ssh` cannot ask for a password. Use an SSH key or
  `ssh-agent`.
- After a reconnect the forward comes back, and the open tab connects
  again by itself.
- When you leave `termocode ssh`, the forward ends.

## See a dev server in the browser (port preview)

Run **Web: Forward Port…** on the laptop and type the port of a dev
server on this computer, for example `3000`. A card asks first; **Not
now** is the first button.

- In `termocode ssh --web` mode, `termocode ssh` on your computer adds
  one more SSH forward and opens `http://127.0.0.1:<free port>/` in your
  browser there.
- In tailnet mode, `tailscale serve` gives the port its own https
  address on your tailnet (port 8443 and up). Never Funnel, never the
  internet.
- When the panel runs on this computer, there is nothing to forward: the
  browser opens `http://127.0.0.1:<port>/`.

**Web: Forwarded Ports…** lists the open forwards. Press Enter on one to
stop it.

- You type only a port. The target is always `127.0.0.1` on this
  computer. There is no host field.
- The app gets its own address (its own port), never a path under the
  panel. Its pages cannot open the panel's connection or read its data.
- Every forward ends when the panel stops, and goes into the panel's
  audit log (`fwd_open`, `fwd_close`).
- Only you can start one. AI agents, MCP and project files cannot.
- The app on that port sees what you type into it.

## Use it on a phone

The page is made for a phone: big buttons, a tab bar for your thumb, and
a key bar for the terminal. The easy way to reach it from your phone is
your own [Tailscale](https://tailscale.com) network (your *tailnet*).

### Turn on tailnet mode

You need Tailscale on this computer and on the phone, both logged in to
the same tailnet, and **HTTPS certificates** turned on for the tailnet
(in the Tailscale admin console, on the DNS page). Then add to
`config.json`:

```json
{ "web_listen": "tailnet" }
```

**Web: Open in Browser** now says that your tailnet devices can reach the
panel. When it starts, termocode runs `tailscale serve` for the panel,
and the panel gets an address like `https://box.tail1234.ts.net/`. The
toast and `termocode web status` show it.

- The panel itself still listens only on `127.0.0.1`. Tailscale passes
  the requests on to it.
- It is **never public**. If the port is under Tailscale Funnel (open to
  the internet), the panel does not start, and it stops at once when
  Funnel is turned on later. Run `tailscale funnel off`.
- When the panel stops, `tailscale serve` stops with it. Nothing is left
  behind in your Tailscale settings.
- On Linux, Tailscale may refuse a normal user. Run once:
  `sudo tailscale set --operator=$USER`.

### Or: LAN mode (same Wi-Fi, no Tailscale)

When the phone or tablet is on the same Wi-Fi as this computer, you do
not need Tailscale or any cloud. Add to `config.json`:

```json
{ "web_panel": "on", "web_listen": "lan" }
```

- The panel listens with **https** on this computer's private address
  only (`192.168.x.x`, `10.x.x.x` or `172.16-31.x.x`), on `web_port`
  (`47443` when not set). Never on a public address, never on all
  addresses.
- termocode makes a certificate once and keeps it, with its key (`0600`),
  in the `web/lan` folder next to `config.json`. The key never leaves
  this computer.
- The pairing QR code also carries the certificate's **fingerprint**
  (`#p=…&fp=…`). The termocode app trusts exactly that certificate for
  that address, and nothing else.
- Passkeys need a name, not a number, so LAN mode has none. After the
  laptop says yes, the page shows **Finish**. Keep **Remember this
  browser** ticked (it is ticked for you); the device then signs in
  again by itself.
- A normal browser shows a certificate warning on this address. Use the
  termocode app.
- Share as Viewer needs tailnet mode.
- The AI can never change `web_listen`.

### Pair your phone

1. In termocode, run **Web: Pair a Device…**. A QR code shows. It works
   **once**, for **2 minutes**.
2. Scan it with the phone's camera. The page opens. Tap **Pair this
   device with the laptop**.
3. The phone shows a **6-digit code**. termocode shows a card with the
   phone's name and a code. Pair only if the two codes are the same.
   **Not now** is first; pick **Pair as viewer** or **Pair with
   control**.
4. The phone asks you to **save a passkey** (Face ID, fingerprint or the
   phone's PIN). Then the workspace opens.

If someone else scanned the code before you, their code is on the card,
not yours: pick **Not now**. Your phone then says the link was used;
make a new one.

### Sign in again

A paired phone needs no new link. Open the panel's address and tap
**Sign in with passkey**. There are no passwords.

### Open from a bookmark

Tick **Remember this browser** before you sign in (or before you save
the passkey when you pair). Next time, a bookmark of the panel's address
opens it without the passkey. The address must stay the same: use tailnet
mode, or set `web_port` for the panel on this computer. If a sign-in code
is on, the phone still types it.

- Only this browser on this one address is remembered. termocode keeps
  only a fingerprint (hash) of its key, never the key.
- The browser keeps the key in a locked cookie (httpOnly, SameSite=Strict,
  Secure over https). Scripts on the page cannot read it.
- The key changes each time it is used. If an old copy is used again,
  termocode ends the bookmark at once.
- Not used for 30 days, it ends.
- To end it: **Forget this browser** in **This device** (or **This
  phone** in More), or on the laptop **Web: Devices…** → the device →
  **Forget bookmark**. **Remove device** ends it too.
- A guest from **Share as Viewer** is never remembered.

### On your phone: one box

On a phone the page opens on the **Home**: one chat box at the bottom,
with a row of chips above it. Type a question or tap a chip; the answer
comes back in the same stream. The tabs (Files, Term,
Git, More) are still one tap away. A wider screen opens the chat as
before.

- **Add it to your Home Screen**: in the phone's browser menu pick
  **Add to Home Screen** (or **Install app**). It then opens full screen,
  straight on the Home. This needs tailnet mode: the address must stay
  the same.
- It is still the same page from your laptop. Nothing is installed in
  the background: there is no service worker, so nothing runs when the
  page is closed, and no file of the page is kept on the phone.
- Your files, chats and keys stay on the laptop. The phone only shows
  them.

### A sign-in code as well (optional)

For a second step after the passkey, run **Web: Set Up Sign-in Code…**.
It shows a QR code for an authenticator app on your phone, and the key
as text. The key is kept in your
[secret store](../reference/config-files.md), which must be unlocked when a phone signs
in. Then add `"web_require_totp": true` to `config.json`. A phone now
types the 6-digit code after its passkey. A code works once; after 5
wrong codes it waits 5 minutes. The one-time link from this computer
does not need it.

### Viewers and control

A **viewer** sees your chats, files, terminals and git state, but cannot
type, answer cards or change anything. The page shows **View only** at
the top.

To get control, a paired viewer taps **Ask for control**. The phone
asks for its passkey, and termocode shows a card: **Give control to
iPhone · Safari?** **Not now** is first. Only your answer on the laptop,
within 5 minutes of the passkey, gives control. The phone then comes
back with control by itself.

To take control back, open **Web: Devices…**, pick the device and choose
**Make viewer**.

### Devices

**Web: Devices…** lists the paired devices with their role, and whether
they are connected now. Pick one to see it, make it a viewer, or
**Remove device**. A removed device is disconnected at once and its
passkey works nowhere; it must pair again to come back.

### Notifications on the phone

A paired phone can hear that something waits for you (a card to
answer, a plan to approve), even with its screen off. There is no
cloud of termocode in the way. The laptop sends the push itself, and a
push carries **no content**: at most a number.

There are three ways. Pick the one that fits your phone:

| Way | Phone | Who carries it | What it carries |
|---|---|---|---|
| **Web Push** | iPhone (the page on the Home Screen), Android | the push service of the phone's browser (Apple, Google, Mozilla, Microsoft) | encrypted; inside, a number at most |
| **UnifiedPush** | Android, with a distributor app or server of your own | your own distributor | the word `wake`, nothing else |
| **Keep it running** (`web_phone`) | any | nobody | nothing: the open page just stays connected |

- **Web Push.** The laptop signs each push with its own **VAPID** key
  and encrypts it for that one phone's browser. The push service sees
  only ciphertext and the time. A push that waits longer than an hour
  for a phone that is off is dropped: by then, the laptop is the better
  place to look.
- **UnifiedPush.** The phone gives the panel the address of its own
  distributor. When a new card waits and the phone is not connected,
  the panel posts `wake` to that address, at most once every 30
  seconds. The phone wakes, connects as usual and reads the card over
  its own link. The address is kept only while the panel runs; the
  phone gives it again each time it connects.
- **Keep it running.** With `"web_phone": "on"` in `config.json`, a
  paired phone with the panel open keeps its sign-in while its screen is
  off. Guests and the one-time link do not.

What a Web Push says is set by `web_push_detail` in `config.json`:

| Value | The phone shows |
|---|---|
| `none` | "Something needs you" |
| `count` (default) | how many things wait, such as "2 things need you" |
| `title` | the number and the kind of the newest one, such as "A plan needs you" |

The kind comes from a short fixed list (tool, MCP, plan, review, trust,
workflow). The phone shows its own fixed text for it. A push never
holds a file name, a command, a path or words the AI wrote.

#### What a push can tell

- **On the lock screen:** only the fixed text above. Nothing private.
- **To the push service or your distributor:** that this phone got a
  push, and when. With Web Push, how big it was (a few bytes). Not who
  sent it from where, and not what waits.
- **To someone who steals the phone:** only the fixed text. To see a
  card, they still need the phone's passkey or its remembered browser,
  and a removed device gets no pushes and cannot sign in.
- **The answer is never in the push.** You answer a card only on the
  page, after it connects, with the same safe-first rules as always. A
  push has no buttons that allow or deny anything.
- **Keys stay on the laptop.** The VAPID private key and each phone's
  subscription keys live in your
  [secret store](../reference/config-files.md). They are never sent to a phone,
  written in a log or shown in an error.
- **The laptop posts only to push services.** A Web Push address must
  be an `https` address of a known browser push service. A UnifiedPush
  address may be your own server, but never this computer, a local
  link address or a cloud metadata address, and redirects are not
  followed. The audit log keeps the address's host, never the rest.
- **Stop it any time:** turn notifications off on the phone, or remove
  the device in **Web: Devices…**. A push address the service says is
  gone is dropped at once.

#### UnifiedPush in detail

An Android phone with a UnifiedPush distributor (your own server, or a
distributor app on the phone) can be woken when a card waits for you.
No cloud of ours takes part. The phone gives the panel its push address
when it connects; the address is kept in memory only, until the panel
stops.

- A wake is sent only when **more** cards wait than before, only to a
  phone that is **not** connected now, and at most once every 30
  seconds per phone.
- The wake says nothing: its whole body is the word `wake`. No count,
  no title, no card. The phone then connects and shows the cards.
- Only a paired phone of yours gets wakes, never a shared viewer.
- The address must be `https`. It may not point at this computer or at
  a link-local address. Redirects are not followed.
- The audit log keeps the push host and the result, never the full
  address.

### Workflows and agents from the phone

With control, the phone can start work on the laptop. Anything that
starts real work asks first, on a card with **Cancel** first. You
confirm it on the phone (see [How it keeps you safe](#how-it-keeps-you-safe)); with
`web_approve_on_laptop` on, the card shows on the laptop instead.

- **Workflows.** A **dry run** starts at once: it reads its sources
  read only and never writes. Plan steps still cost AI money, and the
  page says so before it sends. A real **run** and **saving** a
  workflow the chat drafted are laptop cards. A run's writes still ask
  on their own card (**Deny** first). An untrusted or broken workflow
  file is refused before any card; trust stays on the laptop.
- **Races.** The races view lists each race, its 2 to 4 lanes (agent,
  state, files and lines changed, verify result, cost) and the AI's
  short summary of the differences. Starting a race is a laptop card.
  From the phone you can then ask for the **summary** (it uses your
  Quick model and shows the cost), run the race's **verify** command
  (the one on the start card; the phone cannot change it) and **stop**
  the lanes. **Merge** and **Discard** stay on the laptop.
- **Compare models.** A race of AI profiles on different models, on the
  same task. It starts behind the same laptop card.
- **Coordinator runs.** Ask the laptop to run a task with a planned
  profile ("Plan, then run in waves" or "Work until done"). The laptop
  card names the profile, its steps and the task. The plan then comes to
  the phone as a card (**Reject** first). You can send a running step a
  short message; it reads it after its current tool call.
- **A new lane.** Start an agent in a new worktree, behind a laptop
  card.

The phone never gets a lane's folder, a file's contents or a secret.
Every one of these is in the audit log; a message to a running step
keeps only the run and the step, never the text.

### Share with another person

To let someone else watch, run **Web: Share as Viewer…**:

1. Share this computer with them in Tailscale first (in the admin
   console, **Share** on this machine). They need Tailscale too.
2. **Copy link** and send it to them. It works **once**, for **10
   minutes**.
3. They open it and read you the **6-digit code** they see. If it is the
   same as on your card, pick **Let them watch**.
4. They save a passkey and can watch. They can never get control. Remove
   them in **Web: Devices…** when you are done.

### Share one AI chat

Run **AI: Share Chat by Link…** to share the chat you have open. A card
says what they will see. **Cancel** is first. **Make link** opens the
same viewer link as above, with the chat open.

- They watch the chat live. They see the rest of the panel too, read
  only.
- They cannot type into the chat. Under it they get a **Comment** box.
- A comment comes to you as a toast. Read all of them in
  **AI: Chat Comments…**.
- The AI never reads a comment. To use one, pick it in
  **AI: Chat Comments…** and choose **Add to message**. It goes into the
  chat input; nothing is sent until you press `Enter`.

### Several projects from one phone

One paired phone reaches **every project on this computer**, not only
the one whose panel it paired with. That panel is the **door**: you pair
once, with one role, and the phone sees all your projects through it.

- The **Projects** list on the phone shows the projects this computer
  knows: recent folders, projects that run now, service folders and
  agent runs. You cannot add a folder from the phone; open it on the
  laptop once and it shows up.
- A project that **already runs** in termocode on the laptop opens on the
  phone at once. The door joins it on this computer only, through the
  project's private session socket. Nothing new listens on the network.
- A project that **does not run** starts only after **a card on the
  laptop**: *Open shop for the phone?* **Not now** is first. termocode
  then starts it without a window (headless). The phone can never start
  a project by itself, and the AI can never open one.
- At most **4** projects are open at once (`web_projects_max`, up to 8).
  The phone shows **Projects open now 2 of 4**. One more is turned
  away; close one first.
- A project the phone started stops by itself after **60 minutes** with
  no use from any phone (`web_project_idle_minutes`). Its views and
  pinned views then say **asleep**, with **Open**. A project you opened
  yourself on the laptop never stops this way.
- Each project keeps its own rules. A card, a review or a laptop-only
  action of one project stays in that project and is answered on the
  laptop as always.
- The phone names a project by its id from the list, never by a path.

### Without Tailscale

The panel can also be reached through an SSH app on the phone that
forwards the same port number to this computer. Then open a fresh link
(**Web: Copy Link**) in the phone's browser within 5 minutes.

## How it keeps you safe

In simple words:

- **Only this computer.** The panel listens on `127.0.0.1`, never on the
  network. Over SSH it uses a private socket instead.
- **Only you.** On Linux, a connection from another user on the same
  computer is refused before anything is read.
- **A one-time link.** The secret is in the part of the link after `#`,
  which the browser never sends anywhere. It is never in a program's
  command line or in a log. It is on the screen only when no browser
  opens, and never on another screen. It works once, for 5 minutes.
- **No cookies.** Each tab has its own key. Close the tab and the key is
  gone.
- **A strict page.** The page loads nothing from the internet and runs no
  code from your files or from the AI. Links from the chat open only
  after you confirm.
- **Safe first.** On every card, **Deny** (or Reject, Not now) is first
  and has the focus. `Enter` and `Esc` never do the risky thing, and
  the risky button works only after a short moment on screen. The page
  never offers "Always allow".
- **The page asks where the laptop asks.** Discarding changes, closing
  a terminal, deleting a file, replacing a file with an upload,
  stopping or removing a container, a command on a server and any SQL
  that may change data ask first. The page shows a card with the exact
  action (the files, the command, the SQL), **Cancel** first. Only the
  device that asked can confirm it. What the laptop does without a
  question runs at once from the page too: push, pull, stash, switching
  a branch or a tag, using a model or a profile, opening a shell or a
  host terminal.
- **The laptop's next question comes to the page.** When your action
  leads to one more question on the laptop (Sleep's **Keep running**
  card, the steps of a worktree **Archive** or **Delete**, a service's
  first run of its command, a merge that hits your changes), the same
  question shows in the page. Your yes there presses its main button;
  **Cancel** its safe one. The page never offers a trust button or
  "Always". A typed name and passwords stay on the laptop.
  You do not need the laptop: you paired this device, so it can do what
  you can do at the laptop. The laptop shows a short notice of what was
  done, and the audit log keeps who did it, from where, and that it was
  confirmed in the page.
- **Want the laptop to confirm?** Set `"web_approve_on_laptop": true`
  in `config.json`. Then these cards wait on the laptop, as before, and
  the phone can only cancel them. The AI can never change this setting.
- **Viewers only look.** A device that may only watch cannot act or
  answer a card.
- **Passkeys, no passwords.** A paired device signs in with a passkey
  that needs your face, finger or PIN. A copied passkey is noticed and
  blocked. A removed device is out at once. A remembered browser
  (bookmark) keeps a key that works on its one address only, changes
  each use and can be forgotten from either side.
- **Your yes, on the laptop.** Pairing a device, letting someone watch
  and giving control each need a card on the laptop, with **Not now**
  first. The phone and the card show the same code, so you know which
  phone you let in.
- **A push says almost nothing.** A notification to a phone holds at
  most a number (or, if you choose, the kind of card). It comes from the
  laptop, with no cloud of termocode, and you answer only on the page.
  See [Notifications on the phone](#notifications-on-the-phone).
- **Never public.** In tailnet mode the panel refuses to run under
  Tailscale Funnel.
- **Passwords stay on the laptop.** Git signs in on the laptop. The
  database view shows no host, user or password, and it hides values of
  columns such as `password` or `token`. Files that look like they hold
  secrets (such as `.env` or keys) are never downloaded.
- **The AI cannot open the door.** The AI can never start the panel,
  pair a device, give control or change its settings. It cannot run or
  type `termocode web` either.
- **One door, your own projects.** A paired phone reaches only the
  projects of your own user on this computer, by id from the Projects
  list, never by a path. The door joins running projects through their
  private session sockets (open only to your user), so nothing new
  listens on the network. A project that is not running starts only
  after your yes on a card on the laptop, and stops again when no phone
  uses it. The AI cannot open a project, and a project's files cannot
  raise the limits.
- **A record.** termocode keeps an audit log of web starts, stops and
  logins in its config folder (`web-audit.jsonl`).

## Check it with a screen reader

The page is checked by axe on every view, in both themes, on the phone
and the desktop. A tool cannot hear the page, so this list is for a
person with a real screen reader: VoiceOver on a Mac or an iPhone,
TalkBack on Android, NVDA on Windows or Orca on Linux. Tick each line
once in English and once in Persian (**More → Language**).

- [ ] The first Tab stop says **Skip to content**. Enter moves to the view.
- [ ] The tabs say their names: **Chat**, **Files**, **Term**, **Git**, **More**, and which one is selected.
- [ ] In the chat, a new answer is read when it ends, not word by word.
- [ ] Every icon button says what it does (no "button" with no name).
- [ ] An AI card is read as a group (on a phone, a dialog) with its title first.
- [ ] On a phone card, the safe choice (**Deny**, **Later**) has the focus first.
- [ ] The risky choice first says **Ready in a moment…** and only then can be used.
- [ ] A terminal does not read every new row by itself.
- [ ] Tab to the hidden button **Read the last 20 rows** in a terminal. Enter reads them once.
- [ ] A toast is read once, politely, without moving the focus.
- [ ] When the connection drops, the reader says **Connection lost. Trying again.** once, not each second.
- [ ] A save error or an edit conflict is read at once (an alert).
- [ ] In Files, each row says its name, and a folder says if it is open or closed.
- [ ] A sheet keeps the focus inside it. Closing a row menu puts the focus back on its row.
- [ ] In Persian, the reader uses the Persian voice and reads right to left.
- [ ] With reduced motion on, nothing on the page keeps moving.

If a line fails, note the reader, the browser and the view, and open an
issue.

## Limits

- At most **4** browsers at once (`web_max_viewers`, up to 8).
- A paste into a terminal is at most **1 MiB**.
- Long files open read-only (the first 2 000 lines). Files that look like
  they hold secrets (such as `.env`) open only on the laptop.
- Search stops at 500 matches or 10 seconds.
- An upload or a download is at most **256 MB**.
- Container logs show the last **64 KiB**; database results show at
  most 1 000 rows.
- No access from another computer on your network, other than through
  SSH or your own tailnet.
- At most 32 paired devices.
- At most **4** projects open at once from a phone (`web_projects_max`,
  up to 8).

## Settings

| Setting | Default | What it does |
|---|---|---|
| `web_panel` | `auto` | `auto`: open it on this computer. `on`: also over SSH, and a script's `--yes`. `off`: never. |
| `web_port` | `0` | A fixed port on `127.0.0.1` (1024..65535); `0` picks a free one. |
| `web_idle_minutes` | `15` | A browser's sign-in ends after this many minutes without use (1..120); the panel stays up. |
| `web_max_hours` | `12` | A browser's sign-in ends this many hours after it began (1..24); the panel stays up. |
| `web_open_browser` | `true` | Open the link in this computer's browser on start. |
| `web_max_viewers` | `4` | Browsers at once (1..8). |
| `web_push_detail` | `count` | What a phone push says: `none`, `count` or `title` (a fixed text per kind). |
| `web_listen` | `loopback` | `tailnet`: also reach it from your tailnet devices. `lan`: from devices on the same Wi-Fi (https, pinned certificate). |
| `web_require_totp` | `false` | A paired device also types a code from an authenticator app. |
| `web_app_key_login` | `false` | The Android app may pair and sign in with its own keystore key (when passkeys fail in the app). |
| `web_phone` | `off` | `on`: a paired phone with the panel open keeps it running, screen off too (not guests). |
| `web_projects_max` | `4` | Projects a phone may have open at once (1..8). |
| `web_project_idle_minutes` | `60` | A project the phone started stops after this many idle minutes (5..240). |

These settings live only in your own `config.json`. A project's settings
cannot change them.

## Troubleshooting

**`termocode web` says no termocode runs here**
:   Start termocode in this folder first. Check that `config.json` does
    not say `"web_panel": "off"`.

**"This link has expired or was already used"**
:   Each link works once, for 5 minutes. Use **Web: Open in Browser** for
    a new one.

**The page says "Offline"**
:   The page lost its connection and tries again by itself. Your work goes
    on in termocode. If the panel was stopped, start it again.

**"The web session stopped after 15 minutes without typing"**
:   That browser's sign-in ended; the panel is still up. A paired device
    signs in again by itself; raise `web_idle_minutes` for a longer one.

**"Too many browsers are open on this session"**
:   Close a tab you do not use, or raise `web_max_viewers`.

**`termocode ssh --web`: "SSH could not forward the web panel"**
:   The server may not allow socket forwarding (`AllowStreamLocalForwarding`
    in its `sshd_config`), or `ssh` wanted a password. Use a key or
    `ssh-agent`, or ask the server's admin.

**`termocode ssh --web`: "no web panel: The web panel is off on the far computer"**
:   Turn it on in the server's `config.json`: `"web_panel": "on"`.

**"Tailscale is not installed here" / "not on a tailnet"**
:   Install Tailscale and run `tailscale up`, or set `"web_listen":
    "loopback"`.

**"Your tailnet gives this computer no HTTPS name"**
:   Turn on HTTPS certificates in the Tailscale admin console (DNS page).

**"This port is public on the internet (Tailscale Funnel)"**
:   Run `tailscale funnel off`, then start the panel again.

**"Tailscale Serve already uses port 443 on this computer"**
:   Something else is served on port 443. See `tailscale serve status`.

**"Projects open now 4 of 4"**
:   The phone has as many projects open as `web_projects_max` allows.
    Close one on the phone, or raise `web_projects_max`.

**A project says "asleep"**
:   The project the phone started had no use for
    `web_project_idle_minutes`, so it stopped. Tap **Open** and say yes
    on the laptop's card.

**"This device was removed"**
:   The device was removed on the laptop. Pair it again.

**"This passkey looks copied"**
:   termocode saw the passkey's counter go back, so it blocked the
    device. Remove it in **Web: Devices…** and pair it again.

**The browser did not open**
:   termocode copied the link and shows it in a toast. Paste it into the
    browser yourself within 5 minutes. Click the toast to copy it again.
