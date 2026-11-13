# Spec Delta

## Purpose

Defines what go-microsip does on macOS that depends on the system: sound devices and the microphone permission, the password in the Keychain, the agent-control transport, the menu-bar item, notifications, `sip:` and `tel:` links, the single instance and the folders it uses.

## ADDED Requirements

### Requirement: Sound devices on macOS
On macOS the application SHALL list the system's input and output devices, open the one chosen by its identifier or the system default, and carry a call's audio both ways within the same threshold as on the other systems: at most 0.5 % of frames late or lost and no gap over 60 ms in a five-minute call.

#### Scenario: Devices are listed
- **WHEN** `go-microsip doctor --json` runs on a Mac with an input and an output device
- **THEN** the report says audio is available and counts at least one input and one output

#### Scenario: Five-minute call
- **WHEN** a five-minute call runs against the lab PBX on a Mac with a capture and a playback device
- **THEN** audio is measured in both directions and the late-or-lost frames and the longest gap are within the threshold

#### Scenario: No input device
- **WHEN** `go-microsip doctor --json` runs on a Mac with an output and no input device
- **THEN** the report counts zero inputs and at least one output, and nothing crashes

### Requirement: A microphone that goes away during a call
When the microphone of a running call is removed or stops delivering, the call SHALL stay up, the window SHALL say that the microphone was lost, as on the other systems, and the application SHALL NOT switch to another microphone by itself.

#### Scenario: Microphone unplugged
- **WHEN** the microphone in use is unplugged during a call
- **THEN** the call continues, the window says the microphone was lost, and no other microphone starts capturing

### Requirement: Nothing recorded while muted or on hold is sent
On macOS, as on the other systems, audio the microphone recorded while a call was muted or on hold SHALL never be sent after the mute or the hold ends, however late the system delivers it.

#### Scenario: Late delivery after unmute
- **WHEN** the system delivers, after an unmute, audio that was recorded before it
- **THEN** none of that audio reaches the other side

### Requirement: Calls survive a hidden window and a sleep
On macOS a call SHALL keep its audio within the threshold while the window is hidden or covered, an incoming call SHALL ring while the application is hidden and idle, and after the Mac sleeps and wakes the application SHALL register again by itself, or say that it could not.

#### Scenario: Hidden during a call
- **WHEN** the window is hidden for the length of a five-minute call
- **THEN** the audio stays within the threshold

#### Scenario: Hidden and idle
- **WHEN** a call arrives after the application sat hidden in the menu bar with no call
- **THEN** the window comes to the front and rings

#### Scenario: Wake
- **WHEN** the Mac wakes from sleep while the application was registered
- **THEN** the application registers again without the user, or the window shows the registration error

### Requirement: Microphone permission
The application SHALL declare why it uses the microphone so that macOS asks the user at the first capture, and when macOS denies the microphone it SHALL say so with the name of the system setting that allows it, instead of sending silence without a word.

#### Scenario: First call
- **WHEN** the first call starts on a Mac where the application was never allowed or denied
- **THEN** macOS asks for the microphone with the application's reason, and after the user allows it the other side hears the caller

#### Scenario: Microphone denied
- **WHEN** a call starts and macOS denies or restricts the microphone for the application
- **THEN** the window shows a notice that names Privacy & Security, Microphone, and the call is not reported as having working capture

#### Scenario: Question not answered
- **WHEN** a call starts while macOS's question about the microphone is still open
- **THEN** the call goes on without capture until the user answers, no audio recorded before the answer is sent, and ending the call or quitting meanwhile leaves nothing waiting

### Requirement: A PBX on the local network
On macOS the application SHALL be able to register and call through a PBX on the Mac's own network, or the documentation SHALL state first among the macOS limits that it cannot and why. The guide's troubleshooting SHALL name macOS's Local Network setting as one possible cause of a registration that gets no answer from a local PBX.

#### Scenario: Local PBX reachable
- **WHEN** the application registers with a PBX on the Mac's own subnet and macOS allows it the local network
- **THEN** the registration succeeds and a call carries audio both ways

#### Scenario: Measured and stated
- **WHEN** a reader opens the guide's macOS limits and troubleshooting
- **THEN** they state what the recorded measurement found for a PBX on the Mac's own subnet, signaling and media, and where macOS's Local Network setting is

### Requirement: The password in the Keychain
On macOS the account's password SHALL be kept in the user's login keychain, written and read without ever appearing on a command line, in a file of the application or in a log. A locked or unreachable keychain SHALL be reported with the same typed errors as on the other systems.

#### Scenario: Saved and read back
- **WHEN** the user saves the account's password in Settings and restarts the application
- **THEN** the application registers with that password and `doctor` reports the keyring as its source

#### Scenario: Keychain not reachable
- **WHEN** the application starts in a session where the login keychain cannot be used, or the keychain does not answer within the time the other systems allow
- **THEN** the password is asked for as on the other systems, a warning says the keychain was not read, and nothing waits without bound

#### Scenario: Removed
- **WHEN** the user removes the password in Settings
- **THEN** the keychain holds no item of the application for that account

### Requirement: Agent control on macOS
On macOS the command line and the MCP server SHALL reach the running window through a same-user local transport with one owner per user, and the rules of grants, approvals, idempotency and audit SHALL be the ones of the other systems. A connection from another user SHALL be refused, and a second start, a crashed owner or a path prepared by someone else SHALL NOT let two owners serve or let a stranger's file be used.

#### Scenario: A command reaches the window
- **WHEN** `go-microsip status` runs while the window is open
- **THEN** it prints the window's state and exits 0

#### Scenario: Unapproved call
- **WHEN** an agent without a grant asks to dial
- **THEN** the call is not placed until the user approves it in the window, and the command exits with the awaiting-approval status meanwhile

#### Scenario: Another user
- **WHEN** a process of another user of the same Mac connects to the control endpoint or reads the grant files
- **THEN** the connection is refused and the files cannot be read

#### Scenario: Two starts at once
- **WHEN** two instances start at the same moment
- **THEN** exactly one becomes the owner and the other's commands reach it

#### Scenario: A prepared folder
- **WHEN** the control folder already exists as a link, with another owner, or open to anyone else by its permissions or an access-control entry
- **THEN** the application refuses to use it and says why

### Requirement: Menu-bar item and window behavior
On macOS the application SHALL show a menu-bar item with the tray's menu (show the window, the latest calls, quit), SHALL keep running in the menu bar when the window is closed, and SHALL quit from that menu, from the application menu and with Command-Q.

#### Scenario: Close keeps it running
- **WHEN** the user closes the window while registered
- **THEN** the application stays registered, the menu-bar item stays, and choosing Show from it brings the window back

#### Scenario: Quit
- **WHEN** the user chooses Quit from the menu-bar item or presses Command-Q with no call
- **THEN** the application unregisters and exits

#### Scenario: Quit during a call
- **WHEN** the user presses Command-Q during a call
- **THEN** the application asks before it ends the call, as the window's own Quit does

### Requirement: Incoming-call notification on macOS
An incoming call SHALL always be shown by the window, which comes to the front and rings. Where macOS lets the application post notifications, the call SHALL also be a notification with Answer and Reject; where it does not, the application SHALL work without it and the documentation SHALL say so.

#### Scenario: Notification allowed
- **WHEN** a call arrives while the window is hidden and notifications are allowed for the application
- **THEN** a notification names the caller, and its Answer and Reject buttons act on that call

#### Scenario: The call ends or is missed
- **WHEN** a notified call is answered, refused or cancelled, or stops ringing unanswered
- **THEN** its notification is taken away, or replaced by one that names the missed caller, as on the other systems, and a button of an old notification does nothing

#### Scenario: Notification not available
- **WHEN** a call arrives and macOS refuses notifications from the application
- **THEN** the window comes to the front and rings, the call can be answered there, and the commands that open no window are not affected

### Requirement: Links on macOS
The application bundle SHALL declare itself able to open `sip:` and `tel:` links. A link opened with go-microsip SHALL reach the running instance, or start one, and SHALL be shown for confirmation before any call is placed; a link that is not a valid destination SHALL be dropped.

#### Scenario: A link while running
- **WHEN** the user opens a `sip:` link with go-microsip while the window is open
- **THEN** the window comes to the front with the destination filled in and asks before it calls

#### Scenario: A link that starts it
- **WHEN** the user opens a `tel:` link with go-microsip while it is not running
- **THEN** the application starts and shows the destination for confirmation

### Requirement: Window confinement on macOS
On macOS the page SHALL be confined by its content security policy, and the self-test SHALL say which confinement checks it could not make there (the navigation guard and the listening sockets) instead of counting them as passed. This qualifies, for macOS, the blocked-navigation rule of the desktop application's confined web interface; the documentation SHALL state the difference from Linux.

#### Scenario: Self-test report
- **WHEN** `--self-test` runs on macOS
- **THEN** it reports the inline-script and evaluation refusals as checked and names the navigation guard and the listening-socket check as not made on this system

### Requirement: One instance per user on macOS
A second start of the application by the same user SHALL bring the first window to the front and exit, on macOS as on the other systems.

#### Scenario: Second start
- **WHEN** the application is opened while it is already running
- **THEN** the running window comes to the front and no second process stays

### Requirement: Folders on macOS
Without `--config`, the application on macOS SHALL read its configuration from, and keep its database and control files under, the user's `Library/Application Support/go-microsip` folder, private to the user. The paths in use SHALL be printed by `doctor`.

#### Scenario: First start
- **WHEN** the application starts for the first time on a Mac
- **THEN** it creates its folder under the user's Application Support, readable by that user alone, and `doctor` prints the paths it uses

#### Scenario: Explicit configuration
- **WHEN** the application starts with `--config` and a file elsewhere
- **THEN** that file is the configuration, with the same permission checks as on Linux
