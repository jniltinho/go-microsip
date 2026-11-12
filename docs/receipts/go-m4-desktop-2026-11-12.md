# M4 so far: links, bounded quit, the keyring, agent control, command line, MCP and service (2026-11-12)

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

**On the distributions' own desktops.** The check run in the desktop VMs (`build/vm/check.sh`, `make go-vm`) asks the services each distribution installs with its default desktop, with nothing added for go-microsip: Ubuntu 26.04 (GNOME Shell 50.1 on Wayland) and Debian 13 (GNOME Shell 48.7 on Wayland), the AppImage of commit `69c0dc9`.

| Service | Ubuntu 26.04 | Debian 13 |
| --- | --- | --- |
| Notifications | the desktop's service (GNOME Shell) offers buttons; asked through the application's notifier, it shows a call's notification and, when the call is over, says it took it away (`TestTheDesktopsNotificationServiceShowsACall`) | the same |
| Tray | the desktop has a tray host (Ubuntu ships one with its GNOME): the running window's icon is in the host's list, and was not before the window started | the desktop has no tray host: the window has no icon there |
| Keyring, as the session has it | the test user is logged in without a password, so the session has no login keyring (the first run of each VM, whose log is not kept: "the keyring has no default collection") or a locked one once it exists (the recorded runs): the adapter says so in under 10 ms, stores nothing and leaves no prompt open (`TestTheDesktopsKeyringAsFound`). For the application this is a password kept for the session, with its warning | the same |
| Keyring, started with the login password | the session's own keyring daemon (GNOME Keyring), started again with the login password as a login with a password leaves it: a password is stored, read, replaced and removed; locked, it says so and no prompt stays open (`TestKeyringStoresReplacesAndDeletes`, `TestALockedKeyringIsReportedAndNotPrompted`) | the same |

Logs: [Ubuntu](go-m4/vm-ubuntu2604.log), [Debian](go-m4/vm-debian13.log). Found on the way: when GNOME locks its screen it turns its extensions off, and Ubuntu's tray host goes with them; the application asks for the host when the window is closed, so a window closed then asks and quits as on a desktop without a tray.

Not covered in 6.2:

- On those desktops the services are asked by the application's adapters from their test binaries and by the running AppImage for its tray icon; no call rings there (the VMs have no PBX), the notification's buttons are not pressed and the tray's menu is not opened. Those are driven in the lab, where the test is the service, and the close button that hides to the tray is the owner's report on KDE.
- KWallet and KDE's notification service; a KDE session is in the manual checklist of task 7.2.
- A user who logs in without a password types the SIP password at every start, unless the configuration provides it: nothing here creates a keyring or asks the desktop to.
- A way for the user to forget the stored password; notifications on Windows (task 6.3).

## Task 6.3: Windows, as far as the VM shows

- The instance lock (`LockFileEx`), which the database is opened under: `TestLockIsExclusiveAndReleased` passes in the Windows VM.
- The database's address: a Windows path was handed to SQLite with its drive in the place of a host, so the database never opened there. Found once the lock let the window reach it; fixed, with `TestTheDatabaseAddressKeepsADriveInThePath`. The run in the Windows VM that shows settings, contacts and history available is in "Runs" below.
- The Credential Manager adapter: `TestKeyringStoresReplacesAndDeletes` passes in the Windows VM, in the desktop session of the logged-on user.
- With the lock and the address fixed, the window on Windows opens its database: the seven-skin run there requires it, and its pictures show an empty history where the earlier ones showed "the database is not available" ([`docs/prints/go/windows/`](../prints/go/windows/)).

- **Agent control on Windows** (`internal/control/endpoint_windows.go`): the socket is of the Unix-domain family, which Windows has, in `%LOCALAPPDATA%\go-microsip\control\`, with one owner by a lock there. Both ends ask the system which process is at the other end (`SIO_AF_UNIX_GETPEERPID`) and whose it is (its token's user), and a peer of another user is neither served nor trusted. This is not the named pipe design D10 names: the same-user rule is kept with the transport Linux uses, so that the server, the client and their limits are one piece of code; the design records the choice. Grant files are written under the same directory and take the permissions of the user's profile; they are given no access list of their own, and the check a grant file gets on Linux (owner and mode) has no counterpart on Windows.
- **Links** (`internal/gui/entry_windows.go`): go-microsip registers itself for `sip:` and `tel:` under the current user (a link class, its command with this executable, and the capabilities Windows lists applications by), as the desktop entry declares the types on Linux. It is then a candidate in Settings, Default apps; it is not made the default.
- **Notifications** (`internal/platform/notify/notify_windows.go`): a ringing call is a Windows toast with Answer and Reject, through the toast library the toolkit itself uses (`go-toast`, a new direct dependency, MIT or Unlicense). A toast is not taken away when its call is answered or over; a button pressed late names a call that is gone, which the engine refuses.
- **The tray and the single instance** are the toolkit's, as on Linux.

| Check, in the Windows 11 VM (`build/vm/check.ps1`, `desktop.ps1`) | Result |
| --- | --- |
| the control transport (`TestAClientReachesTheOwnerOnWindows`, `TestGrantFilesOnWindows`) | pass: a client reaches the owner and is named by the system's process id, from both ends; one owner at a time, and the next replaces a socket file left behind; without an owner the answer is "unavailable"; the other application's hello and a request without a hello get no answer; grant files are written once and read back |
| a stored password and the application folder (`TestAStoredPasswordOutlivesTheRunThatLeftIt`) | pass: one copy of the test program stores a password in the Credential Manager, that copy's folder is removed, and a copy in another folder finds it |
| links | pass: after the window ran once, the registry holds the link class with `"<this executable>" "%1"`, the `sip` and `tel` associations and the registered application, under the current user |
| the command line against the window | pass: `status --json` is answered by the window (mode gui); `history list` exits 3 with `grant_required`; `TestTheWindowOfThisSessionAnswers`; `call dial 600 --key K --json` exits 3 with `approval_required` and the window shows who asks and for what; Escape denies it and `operation get` says `denied` |
| a second start with a link | pass: `go-microsip.exe tel:600` ends with 0 and the running window asks "A link asks to call 600. Call it?"; one softphone process stays |
| the close button and the notification area | pass: the close button hides the window and the softphone goes on; a second start brings the window back and ends with 0 |
| notifications (`TestAToastOnThisDesktop`) | Windows takes the toast and lists it under go-microsip in its notification center, with the caller as plain text ([picture](../prints/go/windows/windows-toast.png)). **Not verified: its buttons.** The new Windows installation has its own do-not-disturb on, which keeps the toast off the screen, and the check did not find the button to press in the notification center |
| the sound adapter (`TestWASAPI`) | **fails**, as in every run in this VM: the emulated sound card has no host behind it and leaves gaps of most of a second |

Open in 6.3, and why the task is not checked:

- **The call walkthrough of the `.exe`** (open, dial a lab extension, hang up, with pictures). The Windows VM cannot reach a PBX: the lab's PBX is on an internal Docker network, and its host profile is, by the lab's own rule, not to be exposed to a VM. A PBX the VM can reach on an isolated network is lab work of its own (task 7.1 builds the lab that 7.2 runs the VMs against).
- **The five-minute sound threshold, the frozen window and the unplugged microphone on Windows.** They need a sound card with a host behind it; the VM's has none.
- **The buttons of a notification**, and a notification taken away when its call ends.
- The tray's menu on Windows (the icon, hiding to it and coming back are checked; its menu is not opened).
- A link opened from another application with go-microsip chosen in Default apps (the registration is checked, and the start with a link; the choice is the person's and is not made by the check).
- Grant files on Windows have no access list of their own (above).

## Task 6.4: agent control

`internal/control` is the Rust candidate's local control contract (its `add-agent-cli-mcp`: `crates/control`, the owner in `apps/rust-microsip`, the ledger in `crates/storage`, the socket in `crates/platform`), version 1, under go-microsip's own names: protocol `go-microsip-control`, socket `$XDG_RUNTIME_DIR/go-microsip/control.sock`. A client of one application is not answered by the other.

- **Protocol** (`protocol.go`): hello and welcome, the fifteen operations with their arguments, the request and response envelopes, the eighteen error codes with their exit statuses, one JSON object per line of at most 1 MiB. Unknown fields, unknown operations, missing or mistyped arguments are refused, and no error repeats what was sent.
- **Owner** (`owner.go`, `grants.go`): one epoch per start of the window. A mutation is written to the ledger before it has any effect, under the epoch and the client's idempotency key, with a MAC of the request under a key that lives in memory for that start: the same request again is reported and never repeated, the key with another request is a conflict, a key of an earlier start is stale and never run again. Without a grant a mutation waits up to 60 seconds for the person at the window (eight at most; "Deny all" pauses new ones for five minutes). A grant issued for the session (scopes, exact destinations and callers, a call quota, at most an hour) runs what it covers without a prompt; its capability is stored as a hash. Status names nobody; contacts, history, devices and the configuration need a read grant. The audit trail holds what was asked and decided with a destination's last two characters and a count of digits, never a number or the digits.
- **Ledger** (`internal/core/store/control.go`): on the tables the schema already had from the Rust candidate. A new epoch expires what waited and marks what may have been sent as unknown; operations are kept 24 hours, the audit 90 days.
- **Transport** (`server.go`, `endpoint_linux.go`): a Unix socket (0600) in a directory only this user can enter (0700), both ends asking the kernel who the other is. The owner is the first go-microsip to take the lock of that directory; a second one runs without agent control and says so on the standard error. Sixteen connections, ten requests a second for each and fifty in all.
- **The window** opens the epoch and serves the socket once its phone is open, shows the request that waits (the page had the dialog since M2), comes in sight for it, and stops serving before the phone closes.

| Check | Result |
| --- | --- |
| the tests of `crates/control`, ported (`protocol_test.go`): the documented shape, unknown fields and operations, bounds without echo, exit codes, framing | pass, in `make go-test` |
| the owner's tests, ported from the Rust owner (`owner_test.go`): one approval and no repeat, denial and expiry, the call an operation names, keys of an earlier epoch, paced DTMF with no digit stored, a key past its retention, deny all and the pause, grants (scope, destination, caller, quota, revocation, another session), paged reads | pass |
| the ledger's tests, ported from `crates/storage` (`internal/core/store/control_test.go`) | pass |
| the transport (`endpoint_linux_test.go`): a client reaches the owner and is named by the kernel's process id; one owner per session and the next takes over a socket left behind; no owner is "unavailable" and nothing is started; a directory others can enter, a file that is not a socket and a symbolic link are refused; the other application's hello, and its welcome, are refused; the limits | pass |
| `TestAnAgentRequestWaitsForThePersonAtTheWindow` (`internal/gui`) | pass: the page is told of the request, the window is brought in sight, nothing is sent before the yes, a no and "deny all" send nothing |
| `make go-window-call`, subtest `agent control` | pass: against the built window registered at the lab PBX, a client of the session's socket reads a status that names nobody; history, contacts, configuration and devices answer `grant_required`; a call to 600 asked for shows "A local program (process N) asks to: call 600" and nothing is dialed for two seconds; the same request again is the same operation; Allow connects the call and the operation is `succeeded`; a hang-up at a version the call does not have is `not_found`; a hang-up asked for and denied leaves the call; asked for again and allowed, the call ends; the client leaves and the window stays registered; after the window ends a client gets "unavailable"; the database's audit trail is the eight lines of what was asked and decided, the destination as `***00` ([log](go-m4/window-call.log)) |
| `internal/archtest` | pass: `internal/control` reaches the core through `domain`, `ports` and `app` only |

**Beside the Rust candidate** (implementation-migration, "Coexistence until cutover"). The VM check starts the Rust candidate's AppImage (`0.1.0-rc.3`) and go-microsip's in the same desktop session of Ubuntu 26.04 and Debian 13, with the session's runtime directory and the same data and configuration homes, each application under its own name (`build/vm/check.sh`, step 6):

| Check | Ubuntu 26.04 and Debian 13 |
| --- | --- |
| both run at once | each has its own lock and its own control socket (`rust-microsip/control.sock`, `go-microsip/control.sock`) in the session's runtime directory |
| each client reaches its own owner | go-microsip's client is answered by go-microsip's window (`TestTheWindowOfThisSessionAnswers`: protocol `go-microsip-control`, epoch 1); the Rust candidate's owner does not take go-microsip's hello; the Rust candidate's client is answered by the Rust candidate's window |
| a second start of each | ends by itself with 0, and one process of each stays |
| files | the Rust candidate's files are byte for byte what they were before go-microsip started, was asked and ended; go-microsip's are what they were before the Rust candidate was asked, started again and ended |

Logs: [Ubuntu](go-m4/vm-ubuntu2604-beside.log), [Debian](go-m4/vm-debian13-beside.log) (20 s of sound in these runs).

Found on the way, about the Rust candidate and not about go-microsip: while another window covers the Rust candidate's, its client is told `timeout`. Its owner answers from the window's redraw, which a Wayland desktop stops for a window out of sight. It happened with go-microsip's window over it and, tried by hand, with the file manager's; it answered again as soon as the other window was gone. go-microsip's owner answers on its own, whatever the window shows. The VM check records what the Rust client is told while both windows are open and requires an answer before and after.

Different from the Rust candidate, on purpose or for now:

- **The headless service** was left out of 6.4 and is built with 6.5, by the owner's decision of 2026-11-12 (below).
- The owner of agent control is decided by a lock in the runtime directory, apart from the lock of the database: two go-microsip windows on different databases in one session do not fight over the socket; the second has no agent control.
- A grant that covers a request runs it at once, where the Rust owner queued it for its next redraw; "Deny all" therefore has no queued granted operation to cancel. DTMF is paced by the owner's own clock, so the Rust "stalled" outcome does not exist.
- A line over the size limit ends the connection, where the Rust owner answered and read on from the middle of the line.
- The owner answers on the connection that asked, with no queue or deadline of its own; a client gives up after six seconds and is told the outcome may still change.

Not part of 6.4: the command line's control commands, the MCP tools, the window's screen that issues grants and the grant files (6.5, below); the Windows transport (6.3). Not covered by a test: a client of another user (the kernel's answer is replaced in the test, there is no second user in the lab), and a client on a real desktop asking for a call (the VMs have no PBX).

## Task 6.5: the command line, the MCP server, the headless service

All three are clients or owners of the contract of 6.4; none adds a rule of its own.

- **Command line** (`cmd/control.go`): `status`, `operation get ID`, `devices list`, `contacts search [TEXT]`, `history list`, `config show`, `call dial TARGET|-`, `call answer|reject|hangup CALL_ID VERSION`, `call mute|unmute|hold|resume CALL_ID VERSION`, `call dtmf CALL_ID VERSION DIGITS|-`, `call transfer CALL_ID VERSION TARGET|-`, `dnd on|off`; `--json`, `--key`, `--grant-file`, and `--limit`, `--cursor`, `--filter` on the listings. A change without `--key` gets a random key, which is printed. A target or digits given as `-` come from the standard input. Exit statuses as the Rust candidate's: 0 done or taken, 2 invalid, 3 not authorized or waiting for approval, 4 not running or unavailable, 5 timeout, 1 internal. No command starts a softphone.
- **MCP server** (`internal/mcp`, `go-microsip mcp [--grant-file FILE]`): the official Go SDK over the standard input and output, fifteen tools named as the operations, each with a schema that takes no unknown argument, read and effect hints, ten calls a second, input lines of at most 1 MiB. A tool call becomes one control request; what does not match a tool never reaches the owner. There is no tool that approves, runs a command or opens a file.
- **Headless service** (`cmd/service.go`, `go-microsip service --grant-file FILE`): the softphone without a window. It owns the profile as the window does, registers with a password from the environment or a private file (it does not read the keyring and asks nobody), consumes the single-use bootstrap grant of FILE in an epoch of its own and serves agent control in service mode: what the grant covers runs, everything else is `grant_required`. It says on the standard error when a call's media starts and ends, and stops on a termination signal or an interrupt, ending calls and unregistering first. While it runs, starting the window answers `engine_owned_by_service` and a link `uri_confirmation_unavailable`, both with status 4.
- **Grants in the window** (`internal/gui`, `frontend/src/components/AgentAccess.vue`): Settings has "Agent access": the thirteen permissions, the numbers a program may call and the callers it may answer, the validity (1 to 60 minutes) and the number of calls. "Create grant file" writes the capability to a new file of this user alone in the session's runtime directory (`go-microsip/grants/`), gone with the session; "Export for headless service" writes the bootstrap beside the configuration (`go-microsip/bootstrap/`). The grants in use are listed and can be revoked; the audit log can be shown and, after a confirmation, cleared. A grant whose file cannot be written is revoked. The capability is never shown.

| Check | Result |
| --- | --- |
| `cmd/control_linux_test.go`, against an owner on a socket of the test's own | pass: 21 command lines become the operations and arguments of the contract; answers become the exit statuses; 15 invalid command lines exit 2 and reach nobody; without an owner the answer is `engine_unavailable`, status 4, and nothing is left in the runtime directory; `-` reads one line; a grant file goes with the request and is never printed, and one that others can read is refused before anything is sent; beside a service the window and a link are turned away; the service needs a bootstrap file |
| `internal/mcp/server_test.go`, client and server in one process | pass: the fifteen tools with strict schemas and hints; each tool is one operation with its arguments and key; eleven calls that do not match a tool, and four tools that do not exist, reach nobody; waiting for approval is not an error and a refusal is; the grant of `--grant-file` goes with every request and is in no answer; the eleventh call of a second is `rate_limited` |
| the service's owner (`internal/control/owner_test.go`, `internal/core/store/control_test.go`) | pass: the bootstrap is consumed once into the service's epoch; what it grants runs, the rest is `grant_required` and nothing waits; a second start on the same bootstrap is refused; a bootstrap is of no use to the window that issued it; a revoked one starts nothing |
| grant files (`internal/control/files_linux_test.go`) | pass: written 0600 in a directory 0700 and never written over; a file the group or others can read, a symbolic link, a file over 512 bytes or without a token are refused, and no error repeats the file |
| `TestAGrantMadeInTheWindowIsAPrivateFile`, `TestThePageCallsMethodsTheServiceHas` (`internal/gui`) | pass: a grant made by the window's service is a private file whose capability runs what it covers without a prompt; the list, the revocation, the export, the audit trail; every backend method the page calls exists, and the grant the page sends is read field for field |
| the page (`make go-frontend-test`, `frontend/scripts/prints.sh`, `scale-check.sh`, `keyboard-check.sh`) | pass: 34 tests; 210 screenshots, the Agent access screen and its audit log in the seven skins among them, all at 7:1 and 3:1; a focus ring on every control of 84 screens; scaling and keyboard checks as before ([logs](go-m4/)) |
| `make go-window-call`, subtest `the command line and MCP` | pass: the built binary's `status --json` names nobody; `history list` exits 3 with `grant_required`; `call dial 600 --key K --json` exits 3 with `approval_required` and the window shows the request; nothing is dialed for two seconds; `operation get` says `awaiting_approval`, then, after Allow, `succeeded`; the same command with its key again answers `ok` and calls nothing. The built `mcp` server, started as an agent starts it, lists fifteen tools; `history_list`, `config_show`, `devices_list` and `contacts_search` are refused with `grant_required`; no `approve` tool exists; `call_hangup` waits for Allow, the call stays for two seconds, and ends after the yes. The database's audit trail is the six lines of what was asked and decided ([log](go-m4/window-call.log)) |
| `make go-window-call`, subtest `the headless service` | pass: the built `service` starts on a bootstrap, says "service ready (epoch 2)" and registers at the lab PBX; without the grant, a call, do-not-disturb and the history are `grant_required`, and so are a destination and a read outside the grant; with it do-not-disturb is taken, the history is read and a call to 600 connects, with "microphone active" on the standard error; a second call is over the quota; the window's start exits 4 with `engine_owned_by_service`, a link with `uri_confirmation_unavailable`, a second service with "owns the profile"; the hang-up by the grant ends the call ("microphone released"); a termination signal ends it with 0; a start on the same bootstrap exits 3. The audit trail is the seven lines of what the grant ran, the destination as `***00`, and the capability is in none of them nor in the service's output |

That is the verification task 6.5 asks for: an unapproved MCP call waits for approval in the window and is audited, and the tools that need a grant are rejected without one.

Not covered in 6.5:

- **The Agent access form, anywhere.** No check fills the form and presses its buttons: the page's test calls the mock backend's functions directly, the screenshot run draws the screen and opens its audit log, and the window's service behind the form has its own tests. The form and the service are tied by a check of the method names and of the fields the page sends. A button that does nothing, or a field the form leaves out, would pass all of these. In the lab the bootstrap of the service subtest is issued by the test with the window's own code. The walkthrough of this screen on the artifacts is part of 6.7.
- A grant used through the built command line against the window (`--grant-file` there is driven in the service subtest and in the unit tests).
- An MCP input line over 1 MiB (the limit is the SDK's, set here; no test sends one).
- The service on a machine that really has no desktop session, and its sound devices there; here it runs in the lab container beside the session's sound service. What it says about the microphone follows the call's media, not the devices themselves as in the Rust candidate.
- Windows: none of this has a transport there yet (6.3).
- The documentation of these commands came with 6.6, below.

## Task 6.6: the documentation

- [`docs/agent-cli-mcp.md`](../agent-cli-mcp.md) is now go-microsip's: the rules that always apply, grants, the headless service, the command line with every command, the MCP server and what is recorded. The Rust application's page is kept as [`docs/agent-cli-mcp-rust.md`](../agent-cli-mcp-rust.md), and the README and the development guide, which are still the Rust application's, point to it.
- The README has a section on go-microsip with the platform notes: what it needs on Linux, where the password is kept, notifications, the tray, links, agent control; what the Windows preview has and lacks; that both applications run side by side and how to import.

| Check | Result |
| --- | --- |
| `TestEveryDocumentedCommandRunsAsWritten` (`cmd/docs_linux_test.go`) | pass: the 26 control command lines of the sh blocks of both documents are run as written, with their placeholders filled and their piped input, against an owner on a socket of the test's own, which receives each as the operation it names, with the grant where one is written; the 10 others (the service, the MCP server, the import, `--help`, `--safe-gpu`, `doctor`) are checked against the command tree, their options and their arguments, since running them here would start a server, a softphone, a window or an import; the MCP client configuration names the `mcp` command with an absolute path; `--help` lists every command the documents name; `make go-dist` is a target; the document names the fifteen tools |
| the test against a document with three mistakes put in (a missing argument, an unknown option, a misspelt value) | each is reported; the document was then restored |
| the built binary | `make go-window-call` runs `status --json`, `history list`, `call dial 600 --key K --json`, `operation get ID`, `mcp`, `service --grant-file FILE --config FILE`, `dnd on --grant-file FILE --key K`, `history list --json --grant-file FILE` and `call hangup ID VERSION --grant-file FILE --key K` as the document writes them (the run of commit `eec4a52`) |

Not covered in 6.6: the documented commands are not each run on the built binary, only those of the lab walkthrough; `claude mcp add` and `codex mcp add` are other programs' commands and are not run; the README's notes on a desktop other than GNOME (the packages it may have to install) and on KDE's tray rest on the owner's report and on the package names, not on a run.

## Status

46 of 61 tasks checked: section 1 complete (5), G0 with 8 of 11 (2.1, 2.8 and 2.10 open), M1 complete (12), M2 complete (8), M3 complete (8), M4 with 5 of 7.

- **6.1 is checked** on the lab evidence above: a second start with a link, the exit during a call (asked for, and on a termination signal) and a PBX that stopped answering, each with the built window. What "Not covered in 6.1" lists is left to the manual checklist of task 7.2.
- **6.2 is checked**: each service is driven with and without it in the lab (GNOME Keyring on a private bus, the test itself as the notification service and as the tray host; a bus with none of them), where no password reaches a file or the database; and on the default desktops of the two supported distributions the services they bring are asked with the same adapters: one desktop with a tray host and one without, both with a notification service, both with a keyring that is missing or locked and then unlocked. What "Not covered in 6.2" lists stays open for 7.2.
- **6.3 is not checked**: its adapters are built and what a VM can show of them passes, but the call walkthrough of the `.exe` and the sound on a real card are not done; see "Open in 6.3".
- **6.4 is checked**: the tests of `crates/control` are ported and pass, with those of the Rust owner and of its ledger; the built window serves the contract and a call asked for waits for the person; the two applications ran side by side in both supported desktops without sharing a lock, a socket or a file. The headless service was left out of it and came with 6.5.
- **6.5 is checked**: the control commands, the MCP server with the Rust tool set and rules, and the headless service; an unapproved call from the built command line and from the built MCP server waits for Allow in the built window and is audited, and the tools that need a grant are refused without one. What "Not covered in 6.5" lists stays open.
- **6.6 is checked**: the command line, the MCP server and the service are documented for go-microsip, the README has the platform notes, and a test runs or checks every command the two documents write.
- 6.7 is not started.

Independent review: Codex round 6 found the M4 work in its extract sound with two minor changes; they were made and are not reviewed ([record](../reviews/migrate-to-go-wails-m3/README.md)). Agent control (6.4 to 6.6) had its first Codex round, which found it unsound for one major finding (a revocation the database refused was reported as done) and nine minor ones; all ten were answered ([record](../reviews/migrate-to-go-wails-m4/README.md)), and no round has reviewed the answers or the Windows work of 6.3 yet. A review is not call evidence.

## Runs

Artifacts `0.3.0-m3` of commit `1ca14bf` (`make go-dist`, `dirty false`, checksums verified; the third-party notices now carry the toast library, 82 components). The checks ran on that commit's tree, with one exception: the Windows VM's desktop script (`build/vm/desktop.ps1`) of that commit had lost three of its steps to an editing mistake (the second start, the call that waits and the tray; it also never started the toast test), which the run on it showed; the script was restored in the next commit, and the Windows run recorded here is the restored script on this commit's `.exe`.

| Command | Result |
| --- | --- |
| `make go-check`, `make go-test`, `make go-frontend-test` | pass (36 page tests) |
| `make go-appimage-call` | pass: the AppImage through the 27 subtests ([log](go-m4/appimage-call.log)) |
| `KEEP=1 make go-vm GO_VMS=windows11 SECONDS=60` | every step but the sound passes: the lock, the control transport, the Credential Manager and the application folder, the window, no listening port, the seven skins, links, the command line against the window, the second start, the tray; the toast reaches Windows, its buttons not verified; **the emulated sound card fails the audio threshold**, so the run as a whole fails ([log](go-m4/vm-windows11.log), [the window](../prints/go/windows/windows-desktop.png), [the notification center](../prints/go/windows/windows-toast.png)) |
| `KEEP=1 make go-vm GO_VMS="ubuntu2604 debian13" SECONDS=20` | Ubuntu 26.04: pass. Debian 13: the desktop's services and the Rust candidate beside it pass; **the sound fails** again in this reused VM, by one stretch of 98 ms without samples in 20 s ([Ubuntu](go-m4/vm-ubuntu2604-beside.log), [Debian](go-m4/vm-debian13-beside.log)) |
| `make go-window-call`, `make go-headless`, `make go-self-test`, `make go-offline-skins`, the page's audits | not run again on this commit: the lab walkthrough ran through the AppImage; the page's drawing is as in commit `eec4a52` |
| `make go-notify`, `make go-keyring`, `make go-lab` | not run again: nothing they exercise changed since their runs recorded for commits `aec4441` and `3b6b8bf` |
| the five-minute sound threshold | last run on commit `69c0dc9`: Ubuntu 26.04 passed ([log](go-m4/vm-ubuntu2604.log)); Debian 13 failed by one stretch of 99 ms ([log](go-m4/vm-debian13.log)). The Debian VM, reused and sharing the host with two others now, has failed the sound in most runs since and passed in some; the adapter is the one that passed five minutes on a clean Debian VM at commit `4a5bd14` ([log](go-m3/vm-debian13.log)). Not explained further: to be run again on a clean VM before task 7.2 relies on it |

The two Linux VMs, and the Windows one since task 6.3, are kept up between runs from here on, by the owner's decision of 2026-11-12, and are destroyed when the change's tasks are done (`cd build/vm && vagrant destroy -f`). They stay on VirtualBox's NAT, with the two read-only shared folders. A run in a VM that was used before is not a run on a clean installation: the first run of each VM in this session was one, the later ones were not.
