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

#### Scenario: No device
- **WHEN** the Mac has no input device
- **THEN** the report names the missing input, the window says a call would have no microphone, and nothing crashes

### Requirement: A device that goes away during a call
When the capture or playback device of a running call is removed, the call SHALL stay up, the application SHALL say which device was lost, and it SHALL NOT switch to another microphone by itself.

#### Scenario: Headset unplugged
- **WHEN** the headset in use is unplugged during a call
- **THEN** the call continues, the window names the lost device, and no other microphone starts capturing

### Requirement: Microphone permission
The application SHALL declare why it uses the microphone so that macOS asks the user at the first capture, and when macOS denies the microphone it SHALL say so with the name of the system setting that allows it, instead of sending silence without a word.

#### Scenario: First call
- **WHEN** the first call starts on a Mac where the application was never allowed or denied
- **THEN** macOS asks for the microphone with the application's reason, and after the user allows it the other side hears the caller

#### Scenario: Microphone denied
- **WHEN** a call starts and macOS denies the microphone to the application
- **THEN** the window shows a notice that names Privacy & Security, Microphone, and the call is not reported as having working capture

### Requirement: The password in the Keychain
On macOS the account's password SHALL be kept in the user's login keychain, written and read without ever appearing on a command line, in a file of the application or in a log. A locked or unreachable keychain SHALL be reported with the same typed errors as on the other systems.

#### Scenario: Saved and read back
- **WHEN** the user saves the account's password in Settings and restarts the application
- **THEN** the application registers with that password and `doctor` reports the keyring as its source

#### Scenario: Keychain not reachable
- **WHEN** a command needs the password in a session where the login keychain cannot be used, such as a remote shell
- **THEN** the command fails with the keyring-unavailable error and names the other accepted sources of the password

#### Scenario: Removed
- **WHEN** the user removes the password in Settings
- **THEN** the keychain holds no item of the application for that account

### Requirement: Agent control on macOS
On macOS the command line and the MCP server SHALL reach the running window through a same-user local transport with one owner per user, and the rules of grants, approvals, idempotency and audit SHALL be the ones of the other systems. A connection from another user SHALL be refused.

#### Scenario: A command reaches the window
- **WHEN** `go-microsip status` runs while the window is open
- **THEN** it prints the window's state and exits 0

#### Scenario: Unapproved call
- **WHEN** an agent without a grant asks to dial
- **THEN** the call is not placed until the user approves it in the window, and the command exits with the awaiting-approval status meanwhile

#### Scenario: Another user
- **WHEN** a process of another user of the same Mac connects to the control endpoint or reads the grant files
- **THEN** the connection is refused and the files cannot be read

### Requirement: Menu-bar item and window behavior
On macOS the application SHALL show a menu-bar item with the tray's menu (show the window, the latest calls, quit), SHALL keep running in the menu bar when the window is closed, and SHALL quit from that menu, from the application menu and with Command-Q.

#### Scenario: Close keeps it running
- **WHEN** the user closes the window while registered
- **THEN** the application stays registered, the menu-bar item stays, and choosing Show from it brings the window back

#### Scenario: Quit
- **WHEN** the user chooses Quit from the menu-bar item or presses Command-Q
- **THEN** the application unregisters and exits

### Requirement: Incoming-call notification on macOS
An incoming call SHALL always be shown by the window, which comes to the front and rings. Where macOS lets the application post notifications, the call SHALL also be a notification with Answer and Reject; where it does not, the application SHALL work without it and the documentation SHALL say so.

#### Scenario: Notification allowed
- **WHEN** a call arrives while the window is hidden and notifications are allowed for the application
- **THEN** a notification names the caller, and its Answer and Reject buttons act on that call

#### Scenario: Notification not available
- **WHEN** a call arrives and macOS refuses notifications from the application
- **THEN** the window comes to the front and rings, and the call can be answered there

### Requirement: Links on macOS
The application bundle SHALL declare itself able to open `sip:` and `tel:` links. A link opened with go-microsip SHALL reach the running instance, or start one, and SHALL be shown for confirmation before any call is placed.

#### Scenario: A link while running
- **WHEN** the user opens a `sip:` link with go-microsip while the window is open
- **THEN** the window comes to the front with the destination filled in and asks before it calls

#### Scenario: A link that starts it
- **WHEN** the user opens a `tel:` link with go-microsip while it is not running
- **THEN** the application starts and shows the destination for confirmation

### Requirement: One instance per user on macOS
A second start of the application by the same user SHALL bring the first window to the front and exit, on macOS as on the other systems.

#### Scenario: Second start
- **WHEN** the application is opened while it is already running
- **THEN** the running window comes to the front and no second process stays

### Requirement: Folders on macOS
Without `--config`, the application on macOS SHALL read its configuration from, and keep its database and control files under, the user's `Library/Application Support/go-microsip` folder, private to the user. The folder each one uses SHALL be printed by `doctor`.

#### Scenario: First start
- **WHEN** the application starts for the first time on a Mac
- **THEN** it creates its folder under the user's Application Support, readable by that user alone, and `doctor` prints the paths it uses

#### Scenario: Explicit configuration
- **WHEN** the application starts with `--config` and a file elsewhere
- **THEN** that file is the configuration, with the same permission checks as on Linux
