# Tray, close button and the local address (2026-11-12)

Two changes the owner asked for after M2 closed, while trying the 0.2.0-m2 AppImage on a desktop. Neither closes an OpenSpec task: the tray is part of task 6.2 (M4), which also needs notifications and the keyring.

## The local address follows the route to the registrar

Found on a desktop with a VPN: the account stayed at "Failed: no response". The softphone's own log showed the REGISTER leaving from the address of the first network interface while the kernel's route to the registrar went through the VPN interface, so no answer could come back. `diago` resolves an unspecified bind (`0.0.0.0`) to the first interface, for signalling and for audio.

`internal/core/sip` now binds to the address the routing table gives for the registrar (`bindHost`: a connected UDP socket, nothing is sent), computed again at every registration attempt, with `0.0.0.0` kept when there is no IPv4 route.

- `TestTheEndpointBindsWhereTheRegistrarIsReachedFrom`, in `make go-test`.
- `make go-lab`: 66 of 66 scenarios, run directory `artifacts/go-g0/20261112T012907Z` (not versioned). The lab has one interface: it shows no regression, not the VPN case.
- **Not verified here**: registration through the VPN itself. It needs the owner's account, which tests never use; the owner tries it with the rebuilt AppImage.

## Tray icon and close button

- A tray icon (the application's), with a menu: "Show go-microsip", "Quit". A click on the icon shows the window. Starting the application again also shows a hidden window.
- The close button hides the window where the desktop shows tray icons (a `org.kde.StatusNotifierWatcher` owner on the session bus, asked at the moment of closing; always on Windows). Calls go on.
- Without a tray the close button quits through the page's own Quit, which asks before ending a call ("Hang up and quit?"). The same for the tray's Quit, after showing the window. A page that is not listening is not asked, and a second request within three seconds does not wait for the page.
- `--self-test` creates no tray and keeps the toolkit's close.

Evidence:

- `TestLeavingAsksThePageThatListens` (`internal/gui`), in `make go-test`.
- `make go-window-call` ([log](go-tray/window-call.log)): on the lab desktop, which has a session bus and no tray host, Alt+F4 during an established call shows the question and Cancel keeps the call; Alt+F4 without a call ends the process and unregisters the extension.
- `make go-appimage-call` ([log](go-tray/appimage-call.log)): the same through the AppImage of `make go-dist` at commit `a24194c` (version `0.2.0-m2`, `dirty false`), and `make go-self-test` on that build.
- `make go-check`, `make go-frontend-test` (31 tests).

**Not verified**: the icon, its menu and hiding to the tray on a desktop that has a tray host (KDE, GNOME with the AppIndicator extension) and on Windows. The lab cannot show a tray; it is for the owner's desktop and for tasks 6.2 and 6.3. Recent calls in the tray menu, which the Rust candidate has, are not built.
