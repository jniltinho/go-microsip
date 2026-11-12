# M4 so far: links, bounded quit, the keyring (2026-11-12)

Change `openspec/changes/migrate-to-go-wails`, section 6. Work in progress: each section says what its checks show and what is still open; "Status" at the end says which tasks are checked on this evidence. Nothing here used a production account: the PBX is the disposable lab, the extensions and passwords are its fictional ones.

## Task 6.1: single instance, links, bounded shutdown

**Links.** `internal/core/domain/link.go` is the strict `sip:`/`tel:` parser of the Rust candidate (`crates/platform/src/uri.rs`, LIN-02), with its cases ported: what is accepted, what is refused and why, shell metacharacters, a bounded fuzz. Only the user part of a link is ever dialed, through the configured account; a host is checked and dropped. The window takes one optional link argument, checked before anything starts (a refusal is a usage error that never repeats the link). A second start hands the number to the running instance through the toolkit's single-instance channel on the session bus and ends; the running instance checks it again. The page asks ("A link asks to call 600. Call it?", Cancel focused) and dials only on the yes. A question on the screen is not replaced by another link, and the view that was open is left as it is until the yes.

| Check | Result |
| --- | --- |
| `TestDialLinksThatAreAccepted`, `TestDialLinksThatAreRefused`, `TestShellMetacharactersInALinkAreRefused`, `TestDialLinkFuzzIsBounded` (`internal/core/domain`) | pass, in `make go-test` |
| `TestALinkArgumentIsCheckedBeforeTheWindow` (`cmd`) | pass: valid links reach the window as a number alone; five invalid ones exit 2 without starting it or repeating the link |
| `TestALinkIsHandedToThePageToAsk` (`internal/gui`) | pass: the number waits for a page, is checked again, and nothing is dialed by the window |
| "a link asks before it calls" (the page's tests) | pass: no command before the yes; an invalid number, a second link, a pending delete, a form being typed, a call in progress |
| `make go-window-call`, subtest `links` | pass: the built window started with `tel:6-0-0` registers and asks, the yes connects the call; a second start with a `sip:` link ends with 0, the running window asks, one window stays, Cancel places no call for the next three seconds; a link with parameters ends with 2 and nothing reaches the window |

**Bounded shutdown.** The engine's stop is bounded by `shutdownTimeout` (5 s): two fifths for the call, two more for the account, the rest for the sound devices.

| Check | Result |
| --- | --- |
| `TestShutdownIsBoundedWhenThePBXStopsAnswering`, `TestShutdownDuringACallIsBoundedWhenThePBXStopsAnswering` (`internal/core/engine`) | pass: a loopback PBX that registers and answers a call, then answers nothing; the engine reports the call ended and the registration disabled and stops in 4.0 s, having sent the hang-up and the unregistration |
| `TestAConditionalShutdownIsRefusedDuringACall` | pass: a quit nobody was asked about is the engine's to decide |
| `make go-window-call`, subtest `quit during a call` | pass: the close button during a call asks; after the yes the process is gone (0.1 s with the lab PBX answering), the call ended and the account unregistered, and the PBX takes the same extension again |
| `make go-window-call`, subtest `a termination signal during a call` | pass: the same without a question, on SIGTERM; the call's last event arrives while the window is leaving and does not hold it |
| `make go-window-call`, subtest `quit with a PBX that stopped answering` | pass: the built window registers with and calls a PBX of the test's own (a SIP responder on the loopback), which then reads what it is sent and answers nothing more; the close button asks, and after the yes the whole process is gone in 4.0 s, within the five the engine's stop is bounded by, having sent the hang-up and the unregistration |
| the lab scenarios `engine/shutdown/*` | pass in `make go-lab` |

**The desktop's links.** By the owner's decision of 2026-11-12, the entry the application installs for its icon declares `x-scheme-handler/sip` and `x-scheme-handler/tel` and passes the link on (`%u`), as the AppImage's own entry does (`TestInstallDesktop` in `internal/gui`). That makes go-microsip a candidate for those links in the desktop's list of applications; it does not make it the default, which stays the user's choice in the desktop's settings. A link never calls by itself: the page asks.

Not covered in 6.1:

- A link opened from another application of a real desktop (a browser, `xdg-open`): the lab starts the binary with the link as the desktop would, and does not go through a desktop's handler table.
- On Wayland compositors that refuse focus stealing, a second start may not raise the window; the question is still there when the user gets to it. Not tested outside the lab's X display.
- The native question shown when the page does not answer a quit, and a quit overlapping a termination signal, are not driven by a test.

## Task 6.2: the keyring

A password typed in the settings is kept in the user's keyring and nowhere else; the database keeps a reference of the form `go-microsip/<random>`, which says nothing of the account. The next start reads it and registers by itself. A password the configuration provides (the environment, a private file) still comes first. A keyring that is locked or missing leaves the password for the session, with a warning on the standard error. One that does not answer in time may or may not have taken the password: the warning says the outcome is not known, the reference is recorded, and the next start finds the password or asks. An operation has one time limit of five seconds from before it connects, and starts no call after it; a call already sent is the keyring's to finish. For that, every password typed is written under a reference of its own, and the entry of the one before is removed once the database names the new one: a write the keyring finishes late replaces nothing, and is left as an entry nothing names. The unlock prompt of the desktop is never started from here, as in the Rust candidate.

By the owner's decision of 2026-11-12 there is no encrypted copy of the password in the database for a desktop without a keyring: there the password is typed once per session, or provided by the configuration (the environment, a private file).

- Linux: the freedesktop Secret Service on the session bus (`internal/platform/keyring`), items of the default collection with the attributes `application = go-microsip` and `account = <reference>`, a "plain" session, every operation bounded by five seconds, connecting to the bus included.
- Windows: generic credentials of the Credential Manager, named `go-microsip:<reference>`.
- The Rust application's entries (`application = rust-microsip`) can be read for the import of its password; they are never written or deleted.

| Check | Result |
| --- | --- |
| `make go-keyring` | pass: against GNOME Keyring on a private session bus, unlocked with a throwaway password: store, read, replace, another reference, delete, invalid references; then the collection is locked and reading and storing answer "locked" with the keyring's prompt dismissed; then a session bus with no Secret Service and a session with no bus answer "no keyring", and no error repeats a password; then a bus that accepts the connection and never answers lets the caller go after 5.0 s |
| `TestThePasswordKeptIsTheOneOfTheLastRegistration` (`internal/core/app`) | pass: with the first registration's keyring write held back, the second waits, and its password is the one kept |
| `TestAnOperationGivenUpOnFindsItsTimeOver` (`internal/platform/keyring`), `TestAKeyringThatAnswersLateLeavesNothingAstray` (`internal/core/app`) | pass: an operation the caller gave up on finds its time over, so it starts no further call; a write whose outcome is not known is said so, its reference is recorded, and the next start registers if the keyring took it and asks if it did not |
| `TestAWriteThatLandsLateDoesNotReplaceTheLaterPassword` (`internal/core/app`) | pass: the first password's write is given up on and done by the keyring only after the second password's; the next start reads the second |
| `TestATypedPasswordIsKeptInTheKeyringForTheNextStart` (`internal/core/app`) | pass: the typed password registers, is the one entry of the keyring under a reference without the account, is in no file of the profile, registers by itself at the next start, is replaced by the next one typed; without a keyring it lives for the session and the next start asks again |
| `make go-window-call`, subtest `the password is remembered` | pass: in the built window, with no password configured, the password is typed in the settings, the account registers; no file of the profile (database, its journal, configuration, the window's trace and output) holds the password; the next start registers by itself |

`doctor` reports a password the configuration does not provide and the keyring holds as present, from "keyring". An imported configuration replaces the account, and the replaced account's password is removed from the keyring (both in `TestATypedPasswordIsKeptInTheKeyringForTheNextStart`).

**Notifications.** A call that rings here is notified through `org.freedesktop.Notifications`, as in the Rust candidate (LIN-01): "Incoming call", the caller as plain text (control and text-direction characters removed, markup escaped, 64 characters), urgency critical, and Answer and Reject where the service offers buttons. The notification goes when the call is answered, refused or over. Answer also brings the window in sight. A button counts only when it comes from the notification service itself (the bus name that owns it). Requests are queued (eight) and never block the engine's events; without a notification service nothing is shown.

| Check | Result |
| --- | --- |
| `make go-notify` | pass: the test is the notification service of a private session bus: the text and the buttons asked for, both buttons back as actions of their call, a button of another notification, a button never offered and a button "pressed" by another process of the session ignored, a call that rings on replaces its notification, Close closes once; then a service that left: the bus starts it again for the next call, which it shows; then a bus without a service and a session without a bus: nothing shown, nothing blocked |
| `TestCallerTextIsPlain` (`internal/platform/notify`) | pass |
| `TestARingingCallIsNotifiedAndItsButtonsDecide` (`internal/gui`) | pass: only a call that rings here is notified, once; it is closed when answered or over; Answer and Reject reach the engine for that call |
| `make go-window-call` | pass: the lab test is the notification service of its session; the second incoming call arrives with the window minimized, the notification names the caller and offers the two buttons, and Reject pressed there refuses the call (the caller is told) |

**The tray.** The tray icon, its menu (Show, the last five calls, Quit) and the close button that hides to it are in [the tray receipt](go-tray-and-route-2026-11-12.md). The lab now has a tray host of its own:

| Check | Result |
| --- | --- |
| `make go-window-call`, subtest `the tray` | pass: the test is the session's `org.kde.StatusNotifierWatcher`, so the built window registers its tray item with it. The item's menu, read over the bus, holds "Show go-microsip" and Quit, and after a call to 600 also "↗ 600"; the close button hides the window, the process stays and the account stays registered; activating the item shows the window again; with the window hidden, choosing the call in the menu shows it and calls 600; Quit in the menu unregisters and ends the process. The other subtests run on a session without a tray host, where the close button during a call asks before it quits |

Not covered in 6.2: the keyring, the notifications and the tray on a real desktop beyond the owner's use of the tray on KDE (KWallet, GNOME Keyring, the desktop's own notification service, a locked keyring there); a way for the user to forget the stored password; notifications on Windows (task 6.3).

## Task 6.3: Windows, as far as the VM shows

- The instance lock (`LockFileEx`), which the database is opened under: `TestLockIsExclusiveAndReleased` passes in the Windows VM.
- The database's address: a Windows path was handed to SQLite with its drive in the place of a host, so the database never opened there. Found once the lock let the window reach it; fixed, with `TestTheDatabaseAddressKeepsADriveInThePath`. The run in the Windows VM that shows settings, contacts and history available is in "Runs" below.
- The Credential Manager adapter: `TestKeyringStoresReplacesAndDeletes` passes in the Windows VM, in the desktop session of the logged-on user.
- With the lock and the address fixed, the window on Windows opens its database: the seven-skin run there requires it, and its pictures show an empty history where the earlier ones showed "the database is not available" ([`docs/prints/go/windows/`](../prints/go/windows/)).

Open in 6.3: notifications, the tray and link registration on Windows; the sound on real hardware (the VM's emulated card fails the threshold); the call walkthrough of the `.exe`; that credentials survive removing the application folder (they are in the Credential Manager, outside it, which is not the same as having tried).

## Status

42 of 61 tasks checked: section 1 complete (5), G0 with 8 of 11 (2.1, 2.8 and 2.10 open), M1 complete (12), M2 complete (8), M3 complete (8), M4 with 1 of 7.

- **6.1 is checked** on the lab evidence above: a second start with a link, the exit during a call (asked for, and on a termination signal) and a PBX that stopped answering, each with the built window. What "Not covered in 6.1" lists is left to the manual checklist of task 7.2.
- **6.2 is not checked.** Each service is driven with and without it, and no password reaches a file or the database, but in the lab container: GNOME Keyring on a private bus, and the test itself as the notification service and as the tray host. On a real desktop there is the owner's report of the tray on KDE and nothing recorded for the keyring and the notifications.
- **6.3 is not checked**: see "Open in 6.3".
- 6.4 to 6.7 are not started.

Independent review: Codex round 6 found the M4 work in its extract sound with two minor changes; they were made and are not reviewed ([record](../reviews/migrate-to-go-wails-m3/README.md)). A review is not call evidence.

## Runs

Artifacts `0.3.0-m3` of commit `3b6b8bf` (`make go-dist`, `dirty false`, checksums verified) with the checks of that commit, except the virtual machines, which ran the artifacts and checks of commit `4a5bd14`.

| Command | Result |
| --- | --- |
| `make go-check`, `make go-test`, `make go-frontend-test` | pass (33 page tests) |
| `make go-lab` | 68 of 68 scenarios |
| `make go-appimage-call` | pass: the AppImage through the two accounts (with the notification's Reject), the wrong password, every skin, links, quitting during a call, a termination signal during a call and the remembered password ([log](go-m3/appimage-call.log)) |
| `make go-self-test`, `make go-offline-skins` | pass |
| `make go-keyring` | pass ([log](go-m3/keyring.log)) |
| `make go-notify` | pass ([log](go-m3/notify.log)) |
| `make go-vm GO_VMS="windows11 ubuntu2604 debian13"` (commit `4a5bd14`) | Ubuntu 26.04 and Debian 13 (GNOME on Wayland): pass ([log](go-m3/vm-ubuntu2604.log), [log](go-m3/vm-debian13.log)). Windows 11: the instance lock, the Credential Manager, the window, no listening port and the seven skins with their database pass; the emulated sound card fails the audio threshold, so the run as a whole fails ([log](go-m3/vm-windows11.log)) |
