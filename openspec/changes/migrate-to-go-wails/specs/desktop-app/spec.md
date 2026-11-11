# Spec Delta

## Purpose

Defines the go-microsip desktop application: one binary that is the compact softphone window, the command-line tool and the local MCP server, with an embedded web interface confined to the application.

## ADDED Requirements

### Requirement: Single binary with three modes
The application SHALL ship as one executable that opens the desktop window by default and also provides the CLI commands and the stdio MCP server. The CLI and MCP modes SHALL run without creating a window, a webview or a graphics context.

#### Scenario: Headless command
- **WHEN** `go-microsip doctor --json` runs in a session with no display
- **THEN** it prints the sanitized report and exits with the documented code without initializing the GUI

#### Scenario: MCP handshake without a display
- **WHEN** the MCP mode is started in a session with no display and a client sends the initialize request and lists the tools
- **THEN** both succeed and no window, webview or graphics context is created

#### Scenario: Default start
- **WHEN** the executable is started with no arguments in a desktop session
- **THEN** the compact softphone window opens

### Requirement: Compact softphone screens
The window SHALL offer Dialpad, Calls and Contacts tabs with a 3×4 keypad, local search, favorites, call controls and settings, at workflow parity with the Rust candidate's screens. Each screen SHALL be fully operable by keyboard and remain legible at 100–200 % scaling.

#### Scenario: Keyboard-only call
- **WHEN** a user types a number, presses Enter, and later presses the hangup shortcut without using the pointer
- **THEN** the call is placed and ended

#### Scenario: Scaled window
- **WHEN** the window is shown at 150 % and 200 % scaling in every shipped skin
- **THEN** no control is clipped, overlapped or unreadable

### Requirement: States come from the backend
Every status, call state, registration state and error shown in the window SHALL derive from core events. A feature that is not implemented or not available on the current system SHALL be absent or visibly unavailable, never a control that appears to work.

#### Scenario: Registration fails
- **WHEN** the registrar rejects the credentials
- **THEN** the window shows the failure reason from the core and does not show the account as registered

#### Scenario: Incoming call while the window is hidden
- **WHEN** a call arrives while the window is closed to the tray
- **THEN** the incoming-call screen becomes reachable with answer and reject actions

### Requirement: Confined web interface
The webview SHALL load only the application's embedded interface and SHALL NOT navigate to remote content. The backend SHALL NOT listen on a network interface for the interface's use, and text controlled by a remote party (caller names, SIP headers, contact fields) SHALL be rendered as inert text.

#### Scenario: Hostile caller name
- **WHEN** an incoming call carries a display name containing markup or a script
- **THEN** it is shown as literal text and nothing is executed or navigated

#### Scenario: Navigation attempt
- **WHEN** content in the window attempts to open an external address inside the webview
- **THEN** the navigation is blocked

### Requirement: Desktop integration
The application SHALL keep a single instance per user, hand `sip:` and `tel:` links to the running instance with user confirmation before dialing, show optional notifications and an optional tray, and end calls and unregister within a bounded time on quit or termination signal.

#### Scenario: Second launch with a link
- **WHEN** the executable is started with a `tel:` link while an instance is running
- **THEN** the running window asks for confirmation before dialing and no second instance stays open

#### Scenario: Quit during a call
- **WHEN** the user quits during an active call and confirms
- **THEN** the call is ended, the account unregisters, and the process exits within the documented bound even if the PBX does not answer

### Requirement: Agent control contract
The CLI and MCP server SHALL provide the Rust candidate's `agent-cli-mcp` behavior: a single owner per user, same-user local transport, grants and explicit approvals for effects, idempotent operations, an audit trail, and no password accepted as an argument or returned in any output.

#### Scenario: Unapproved call from an agent
- **WHEN** an MCP client without a grant requests an outgoing call
- **THEN** the call is not placed until the user approves it in the window, and the decision is audited

#### Scenario: Password on the command line
- **WHEN** a password-like option is passed to any command
- **THEN** the command refuses it and exits with the usage error code
