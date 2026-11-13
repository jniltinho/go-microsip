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
- The addresses compared are those of one DNS answer, and requests follow the registrar's name: a registrar whose later answers are reached through another interface stays bound to the first answer's address.

## Tray icon and close button

- A tray icon (the application's), with a menu: "Show go-microsip", the five latest calls, "Quit". A click on the icon shows the window. Starting the application again also shows a hidden window.
- A latest call is listed as the Rust candidate lists it: "↗ who" for a call placed, "↙ who" for one received, "Missed: who", where who is the contact that has the number or the number itself; a call whose other party cannot be dialed is left out. Choosing one shows the window and calls the number again. The menu follows the history and the contacts.
- The close button hides the window where the desktop shows tray icons (a `org.kde.StatusNotifierWatcher` owner on the session bus, asked at the moment of closing; always on Windows). Calls go on.
- An incoming call that starts ringing brings the window back in sight, wherever it is.
- Without a tray the close button quits through the page's own Quit. The same for the tray's Quit, after showing the window. Asking again is never consent: a page that answers is only asked again.
- The page's Quit asks first ("Hang up and quit?") when it shows a call. When it shows none it does not decide by itself: it asks the engine to stop only if it has no call, and the engine, which is where calls are, refuses when it has one; the page then asks. Only the yes sends the unconditional stop.
- When no page is listening, or the page did not answer the last request within three seconds, the window asks by itself in a native dialog (Cancel by default) whenever an engine is running, and only a yes ends it. The window does not judge whether there is a call: what it knows of calls comes after the fact. Without an engine, or once it has stopped, it quits at once.
- A shutdown is taken once: a second request while the first is on its way is that same shutdown. A wish to quit is kept when the engine has already ended and its last events have not arrived, and the application quits when they are through.
- `--self-test` creates no tray and keeps the toolkit's close.

Evidence:

- `TestLeavingAsksThePageThatListens` (`internal/gui`), in `make go-test`: four requests in a row over a page that answers quit nothing; an unanswered request is asked again until its time is over and is then left to the window; three shutdown requests are one shutdown.
- `TestAQuitOutlivesALateStopped`: the engine's last events held back until after the quit was asked, a stream that closes without `Stopped`, and a full queue, which is a refusal.
- `TestAConditionalShutdownIsRefusedDuringACall` (`internal/core/engine`): a conditional stop asked right after dialing, before any snapshot of the call is read, is refused and the call goes on; without a call it is taken. `TestAnUnaskedQuitIsTheEnginesToDecide` (`internal/gui`) and "quitting during a call asks first and signals do not" (the page): the unasked quit is the conditional one, "busy" brings the question, the yes is unconditional.
- `make go-window-call` ([log](go-m3/window-call.log), after the review): on the lab desktop, which has a session bus and no tray host, Alt+F4 during an established call shows the question and Cancel keeps the call; Alt+F4 without a call ends the process, and the same extension then registers again, which the lab PBX refuses while a contact remains (`max_contacts=1`, `remove_existing=no`); a minimized window is back in sight when a call comes in.
- `make go-appimage-call`: the first version of the close button through the AppImage of commit `a24194c` ([log](go-tray/appimage-call.log)). The run of the reviewed behavior (the minimized window, the third registration) through an AppImage is the one named in [the M3 receipt](go-m3-skins-2026-11-12.md), with its commit.
- `make go-check`, `make go-frontend-test`.

**Driven in the lab since** (`make go-window-call`, subtest `the tray`): the test is the session's tray host (`org.kde.StatusNotifierWatcher`), so the built window registers its item with it. The menu read over the bus, the call listed after it was made, the close button hiding the window with the account still registered, a click on the item showing it again, a call chosen from the menu, and Quit from the menu are in [the M4 receipt](go-m4-desktop-2026-11-12.md). That is the application's side of the tray protocol, not a desktop's drawing of it.

**Not verified**: the icon and its menu as a desktop draws them (KDE, GNOME with the AppIndicator extension) and on Windows, beyond the owner's report below; the native dialog of a page that does not answer (no test drives it).

## The owner's desktop (2026-11-12)

With the AppImage of commit `a24194c` the owner reported that the account registered on the desktop whose route to the registrar goes through the VPN, and that the close button sent the window to the tray. This is the owner's report of a registration, not a run recorded here and not a call: whether a call there has sound is not known, the tray's menu and Windows remain unverified, and tasks 6.2 and 6.3 stay open.
