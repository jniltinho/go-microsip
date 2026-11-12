# M4 so far: links, bounded quit, the keyring (2026-11-12)

Change `openspec/changes/migrate-to-go-wails`, section 6. Work in progress: no task of M4 is closed by this receipt alone, and each section says what is still open. Nothing here used a production account: the PBX is the disposable lab, the extensions and passwords are its fictional ones.

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
| the lab scenarios `engine/shutdown/*` | pass in `make go-lab` |

Open in 6.1:

- The whole process against a PBX that does not answer is not driven (the engine is, in the unit tests above); the lab has no way to silence the PBX from inside the test.
- Registering go-microsip as the desktop's handler for `sip:` and `tel:` needs the user's consent. The entry the application installs for its icon passes a link on and claims no type; the AppImage's own entry declares both. A setting for it is not built.
- On Wayland compositors that refuse focus stealing, a second start may not raise the window; the question is still there when the user gets to it. Not tested outside the lab's X display.

## Task 6.2: the keyring

A password typed in the settings is kept in the user's keyring and nowhere else; the database keeps a reference of the form `go-microsip/<random>`, which says nothing of the account. The next start reads it and registers by itself. A password the configuration provides (the environment, a private file) still comes first. A keyring that is locked, missing or slow leaves the password for the session, with a warning on the standard error; the unlock prompt of the desktop is never started from here, as in the Rust candidate.

- Linux: the freedesktop Secret Service on the session bus (`internal/platform/secrets`), items of the default collection with the attributes `application = go-microsip` and `account = <reference>`, a "plain" session, every operation bounded by five seconds.
- Windows: generic credentials of the Credential Manager, named `go-microsip:<reference>`.
- The Rust application's entries (`application = rust-microsip`) can be read for the import of its password; they are never written or deleted.

| Check | Result |
| --- | --- |
| `make go-secrets` | pass: against GNOME Keyring on a private session bus, unlocked with a throwaway password: store, read, replace, another reference, delete, invalid references; then the collection is locked and reading and storing answer "locked" with the keyring's prompt dismissed; then a session bus with no Secret Service and a session with no bus answer "no keyring", and no error repeats a password |
| `TestATypedPasswordIsKeptInTheKeyringForTheNextStart` (`internal/core/app`) | pass: the typed password registers, is the one entry of the keyring under a reference without the account, is in no file of the profile, registers by itself at the next start, is replaced by the next one typed; without a keyring it lives for the session and the next start asks again |
| `make go-window-call`, subtest `the password is remembered` | pass: in the built window, with no password configured, the password is typed in the settings, the account registers; no file of the profile (database, its journal, configuration, the window's trace and output) holds the password; the next start registers by itself |

`doctor` reports a password the configuration does not provide and the keyring holds as present, from "keyring". An imported configuration replaces the account, and the replaced account's password is removed from the keyring (both in `TestATypedPasswordIsKeptInTheKeyringForTheNextStart`).

**Notifications.** A call that rings here is notified through `org.freedesktop.Notifications`, as in the Rust candidate (LIN-01): "Incoming call", the caller as plain text (control and text-direction characters removed, markup escaped, 64 characters), urgency critical, and Answer and Reject where the service offers buttons. The notification goes when the call is answered, refused or over. Answer also brings the window in sight. Requests are queued (eight) and never block the engine's events; without a notification service nothing is shown.

| Check | Result |
| --- | --- |
| `make go-notify` | pass: the test is the notification service of a private session bus: the text and the buttons asked for, both buttons back as actions of their call, a button of another notification and a button never offered ignored, a call that rings on replaces its notification, Close closes once; then a bus without a service and a session without a bus: nothing shown, nothing blocked |
| `TestCallerTextIsPlain` (`internal/platform/notify`) | pass |
| `TestARingingCallIsNotifiedAndItsButtonsDecide` (`internal/gui`) | pass: only a call that rings here is notified, once; it is closed when answered or over; Answer and Reject reach the engine for that call |
| `make go-window-call` | pass: the lab test is the notification service of its session; the second incoming call arrives with the window minimized, the notification names the caller and offers the two buttons, and Reject pressed there refuses the call (the caller is told) |

Open in 6.2: the tray on a desktop that has one (the owner's report in the tray receipt); the keyring and the notifications on a real desktop (KWallet, GNOME Keyring, the desktop's own notification service) and a locked keyring there; a way for the user to forget the stored password; notifications on Windows (task 6.3).

## Task 6.3: Windows, as far as the VM shows

- The instance lock (`LockFileEx`), which the database is opened under: `TestLockIsExclusiveAndReleased` passes in the Windows VM.
- The database's address: a Windows path was handed to SQLite with its drive in the place of a host, so the database never opened there. Found once the lock let the window reach it; fixed, with `TestTheDatabaseAddressKeepsADriveInThePath`. The run in the Windows VM that shows settings, contacts and history available is in "Runs" below.
- The Credential Manager adapter: `TestKeyringStoresReplacesAndDeletes` passes in the Windows VM, in the desktop session of the logged-on user.
- With the lock and the address fixed, the window on Windows opens its database: the seven-skin run there requires it, and its pictures show an empty history where the earlier ones showed "the database is not available" ([`docs/prints/go/windows/`](../prints/go/windows/)).

Open in 6.3: notifications, the tray and link registration on Windows; the sound on real hardware (the VM's emulated card fails the threshold); the call walkthrough of the `.exe`; that credentials survive removing the application folder (they are in the Credential Manager, outside it, which is not the same as having tried).

## Runs

Filled in at the commit that closes this batch.
