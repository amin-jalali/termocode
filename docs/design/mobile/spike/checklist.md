# MOB-1 device spike: checklist

Goal: know, on real phones, whether the panel's two ways to sign in
work where the phone app will run:

- a **passkey** (WebAuthn, the phone's unlock), and
- the **bookmark cookie** (httpOnly, Secure, SameSite=Strict, 30 days
  of no use; docs/guides/web.md "Open from a bookmark").

Where: an **Android WebView** (the `termocode-mobile` app) and an
**iPhone Home-Screen web app** (Safari, Add to Home Screen). The answer
decides MOB-27 (app key sign-in, built but off): if the passkey fails in
the WebView, MOB-27 is turned on.

You need: two phones (one Android 11+, one iPhone with iOS 17+), the
laptop with termocode in tailnet mode (`"web_listen": "tailnet"`), the
phones on the same tailnet, and a debug build of the Android app
(`termocode-mobile`, `assembleDebug`).

Nothing here makes an account or sends data anywhere. The spike page
makes one test passkey per phone, named "termocode spike (delete me)".

## 0. Serve the spike page (laptop, 2 minutes)

```sh
cd ~/termocode
python3 -m http.server 8799 --bind 127.0.0.1 -d docs/design/mobile/spike
# second terminal:
tailscale serve --bg --https=8443 http://127.0.0.1:8799
```

The page is then at `https://<machine>.<tailnet>.ts.net:8443/`. Same
host name as the panel, so a passkey made here is for the panel's host
too (WebAuthn binds a passkey to the host name, not the port).

When done: `tailscale serve --https=8443 off` and stop the python
server.

## 1. iPhone: Home-Screen web app

| # | Step | Expect |
|---|---|---|
| 1.1 | Safari → open the spike address → Share → **Add to Home Screen** | an icon "tc spike" |
| 1.2 | Open it from the icon; type the phone and iOS version in the box | `Runs as an installed app` says yes |
| 1.3 | Tap **Make a passkey**, then **Sign in with it** | both pass |
| 1.4 | Close it fully (swipe it away), wait 1 minute, open it again | `Cookie kept`, `localStorage kept`, `Non-extractable key kept` pass |
| 1.5 | Wait a day (iOS may clear the data of a web app not opened for 7 days); open again | same as 1.4 |
| 1.6 | **Copy the result table** and paste it into the plan's MOB-1 table | |
| 1.7 | Real panel: Safari → panel address → pair (Web: Pair a Device…) with **Remember this browser** ticked → Add to Home Screen | the panel opens from the icon |
| 1.8 | Close the Home-Screen app fully; open it again | the panel opens **without** a passkey prompt (bookmark cookie kept) |
| 1.9 | **This phone** → Forget this browser; reopen from the icon | **Sign in with passkey** works with Face ID |

Note: on iOS the Home-Screen app and Safari have **separate** cookie
jars. A cookie set in Safari is not in the icon's app. Pair from inside
the Home-Screen app if 1.8 fails, and write down which one you did.

## 2. Android: the app's WebView

| # | Step | Expect |
|---|---|---|
| 2.1 | Install the debug APK; pair with the laptop (Scan the code) | the panel opens in the app |
| 2.2 | At the passkey step of pairing: save the passkey | saved, the workspace opens |
| 2.3 | Close the app fully (recents → swipe away); open it again | panel signs in **without** a prompt (bookmark cookie kept) |
| 2.4 | **This phone** → Forget this browser; close and reopen | **Sign in with passkey** shows the fingerprint / PIN prompt and works |
| 2.5 | Laptop Chrome → `chrome://inspect` → the app's WebView → **inspect** | DevTools of the WebView, on the panel's origin |
| 2.6 | Paste `docs/design/mobile/spike/spike.js` into the console | a table prints; X25519 / AES-GCM / P-256 pass |
| 2.7 | In the console: `tcSpike.buttons()`; tap **Make a passkey**, then **Sign in with it** on the phone | both pass (prints in the console) |
| 2.8 | Close the app fully, reopen, inspect again, paste spike.js again | `Cookie kept`, `Non-extractable key kept` pass |
| 2.9 | Copy the markdown the console printed into the plan's MOB-1 table | |
| 2.10 | Same phone, Chrome (not the app): open the spike address, run 1.3–1.4 | to compare WebView and Chrome |

If 2.2, 2.4 or 2.7 fails: write the exact error (it is in the result
detail, for example `NotAllowedError: …` or `NotSupportedError: …`).
That is the case for MOB-27.

## 3. After

- Delete the test passkeys: iPhone Settings → Passwords; Android
  Settings → Passwords & accounts (Google Password Manager).
- `tailscale serve --https=8443 off`; stop the python server.
- Fill the plan's MOB-1 table (docs-local/plans/mobile-app.md):

| Phone · OS · where | Passkey make | Passkey sign-in | Bookmark kept after restart | Key kept after restart | X25519 | Notes |
|---|---|---|---|---|---|---|
| iPhone … · iOS … · Home Screen | | | | | | |
| Android … · … · app WebView | | | | | | |
| Android … · … · Chrome | | | | | | |

- Decision: passkey works in the WebView → MOB-27 stays off. It fails
  → turn on MOB-27 (`web_app_key_login` on the laptop, the app built
  with `-PtermocodeAppKey=true`). X25519 fails → the relay page (ADR
  0022) cannot run there; say so on MOB-42.
