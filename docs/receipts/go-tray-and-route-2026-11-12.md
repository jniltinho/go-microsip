# Tray, close button and the local address (2026-11-12)

Two changes the owner asked for after M2 closed, while trying the 0.2.0-m2 AppImage on a desktop. Neither closes an OpenSpec task: the tray is part of task 6.2 (M4), which also needs notifications and the keyring.

## The local address follows the route to the registrar

Found on a desktop with a VPN: the account stayed at "Failed: no response". The softphone's own log showed the REGISTER leaving from the address of the first network interface while the kernel's route to the registrar went through the VPN interface, so no answer could come back. `diago` resolves an unspecified bind (`0.0.0.0`) to the first interface, for signalling and for audio.

`internal/core/sip` now binds to the address the routing table gives for the registrar (`bindHost`: a connected UDP socket, nothing is sent), computed again at every registration attempt. It does so only when every IPv4 address of the registrar is reached from the same address; when they differ, when one has no route, when the route is the loopback (a proxy or a tunnel on this host) or when the registrar has no IPv4 address, the bind is `0.0.0.0` as before.

- `TestTheEndpointBindsWhereTheRegistrarIsReachedFrom`, in `make go-test`: the rule over a route table of its own (one address, two that agree, two that differ, no route, the loopback, IPv6).
- `make go-lab`: 66 of 66 scenarios at the first version, 68 of 68 after the review (two redial scenarios added), run directories under `artifacts/go-g0/` (not versioned). The lab has one interface: it shows no regression, not the VPN case.
- **Not verified here**: registration through the VPN itself. It needs the owner's account, which tests never use.

Limits, recorded after the review:

- The address bound is also the one offered for audio, as softphones without ICE do. Where the audio's other end is reached by another route than the registrar, the call can register and have no sound; ICE or STUN is not part of this change.
- The route is looked up with UDP also for TCP and TLS accounts: a host that routes by protocol is not covered.
- IPv6 registrars are not supported, as before.

## Tray icon and close button

- A tray icon (the application's), with a menu: "Show go-microsip", "Quit". A click on the icon shows the window. Starting the application again also shows a hidden window.
- The close button hides the window where the desktop shows tray icons (a `org.kde.StatusNotifierWatcher` owner on the session bus, asked at the moment of closing; always on Windows). Calls go on.
- An incoming call that starts ringing brings the window back in sight, wherever it is.
- Without a tray the close button quits through the page's own Quit, which asks before ending a call ("Hang up and quit?"). The same for the tray's Quit, after showing the window. Asking again is never consent: a page that answers is only asked again.
- When no page is listening, or the page did not answer the last request within three seconds, the window asks by itself in a native dialog (Cancel by default) and only a yes ends a call; without a call it quits at once.
- A shutdown is taken once: a second request while the first is on its way is that same shutdown.
- `--self-test` creates no tray and keeps the toolkit's close.

Evidence:

- `TestLeavingAsksThePageThatListens` (`internal/gui`), in `make go-test`: four requests in a row over a page that answers quit nothing; an unanswered request is asked again until its time is over and is then left to the window; three shutdown requests are one shutdown.
- `make go-window-call` ([log](go-m3/window-call.log), after the review): on the lab desktop, which has a session bus and no tray host, Alt+F4 during an established call shows the question and Cancel keeps the call; Alt+F4 without a call ends the process, and the same extension then registers again, which the lab PBX refuses while a contact remains (`max_contacts=1`, `remove_existing=no`); a minimized window is back in sight when a call comes in.
- `make go-appimage-call`: the same through the AppImage of `make go-dist`, and `make go-self-test` on that build ([first run](go-tray/appimage-call.log) at commit `a24194c`).
- `make go-check`, `make go-frontend-test`.

**Not verified**: the icon, its menu and hiding to the tray on a desktop that has a tray host (KDE, GNOME with the AppIndicator extension) and on Windows, beyond the owner's report below; the native dialog of a page that does not answer (no test drives it). The lab cannot show a tray; it is for the owner's desktop and for tasks 6.2 and 6.3. Recent calls in the tray menu, which the Rust candidate has, are not built.

## The owner's desktop (2026-11-12)

With the AppImage of commit `a24194c` the owner reported that the account registered on the desktop whose route to the registrar goes through the VPN, and that the close button sent the window to the tray. This is the owner's report of a registration, not a run recorded here and not a call: whether a call there has sound is not known, the tray's menu and Windows remain unverified, and tasks 6.2 and 6.3 stay open.
