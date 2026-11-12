# Controlling go-microsip from a terminal or an AI agent

The `go-microsip` executable is also a small command-line client and a stdio MCP server. Both
talk to the go-microsip that is **running** over a private local socket; they never start the
softphone, open its database or register SIP themselves. Without a running softphone every
command answers `engine_unavailable` and exits 4.

This page is about go-microsip, the Go rewrite. The Rust application's own page is
[agent-cli-mcp-rust.md](agent-cli-mcp-rust.md); the two applications never answer each other's
clients. Agent control exists on Linux; the Windows build does not have it yet.

## Rules that always apply

- **Every change needs a person.** Dialing, answering, rejecting, hanging up, DTMF, mute, hold,
  transfer and do-not-disturb wait up to 60 seconds for **Allow** or **Deny** in a prompt over
  the window. The window comes in sight for it and shows one request at a time: which local
  process asked and what would happen. **Allow** works only after the prompt has been on screen
  for one second; **Deny** has the focus. At most eight requests wait at once. There is no
  `approve` command, no `--yes`, and no MCP tool that approves anything.
- **Personal data stays private.** `status` shows the registration, do-not-disturb and the
  current call's id, version, direction and phase, never a name or a number. Contacts, history,
  devices and the configuration need a read grant and otherwise answer `grant_required`.
- **Retries are safe only with the same key.** Each change carries an idempotency key. Repeating
  a request with the same key reports the earlier result and never acts twice; the same key
  with a different request is a `conflict`. After the softphone restarts, old keys answer
  `stale_epoch` and are never run again.
- **Same-user programs.** The socket is in a directory only you can enter
  (`$XDG_RUNTIME_DIR/go-microsip/`), and both ends ask the kernel who the other is. A program
  already running as your user can do anything you can, including clicking **Allow**: this is a
  consent boundary for agents limited to the MCP tools, not a sandbox. Never put personal data
  into an idempotency key: keys are kept in the local ledger for 24 hours.

## Grants: letting a program act without asking each time

In **Settings → Agent access** tick what a program may do, list the exact numbers it may call
(**May call**) and the callers it may answer (**May answer**), its validity (1 to 60 minutes)
and how many calls it may place or answer (at most 100). **Create grant file** writes a secret
to a new private file (mode 0600) under `$XDG_RUNTIME_DIR/go-microsip/grants/` and shows its
path; pass it with `--grant-file FILE`. Requests the grant covers run without a prompt; anything
else (another number, a used-up call limit, another operation) still asks you.

A grant ends when it expires, when you press **Revoke**, or when the window restarts. The secret
is never shown and never stored: the database keeps only its hash. **Show audit log** lists what
was asked and decided; **Clear audit log** deletes that record after a confirmation. **Deny all
and pause** in a prompt also stops grants for five minutes.

Know the limits before granting:

- **May answer** trusts the caller identity the PBX passes on, which a caller can fake unless
  your PBX asserts identities. Granting the answer permission lets a program open the microphone
  for anyone who claims to be a listed number.
- **Send DTMF** sends any digits: PBX feature codes and IVRs can transfer or dial numbers outside
  **May call**. Grant it only with that in mind.
- Grant files are not deleted when the grant expires or is revoked (they are inert then). The
  ones under `$XDG_RUNTIME_DIR` go with the session; delete a bootstrap file yourself.

## Headless service

For a machine without a desktop session, **Export for headless service** in Settings → Agent
access writes a single-use bootstrap file under `$XDG_CONFIG_HOME/go-microsip/bootstrap/`
(`~/.config/go-microsip/bootstrap/` by default). Quit the window, then start the service with
that file:

```sh
go-microsip service --grant-file FILE
```

The service registers the configured account, serves only what the bootstrap grants, and answers
everything else with `grant_required`: nobody is there to ask. It needs the password from
`account.password_env` (by default the variable `GO_MICROSIP_SIP_PASSWORD`) or from a private
`account.password_file`; it never prompts and never reads the keyring. It says on the standard
error when a call's media starts ("microphone active") and ends, and stops on SIGTERM or an
interrupt, ending calls and unregistering first.

Clients use the same file:

```sh
go-microsip status --grant-file FILE
go-microsip dnd on --grant-file FILE
```

A bootstrap starts one service session only; a new session needs a new export from the window.
While the service runs, the window does not open (`engine_owned_by_service`) and a `sip:` or
`tel:` link is not dialed (`uri_confirmation_unavailable`). The service's grant cannot be
revoked while it runs, because the window is closed: stop the service to end it. After the grant
expires the service stays registered and answers only `status`.

Exit statuses of `service`: 0 stopped by a signal, 2 invalid arguments or grant file, 3 the
bootstrap cannot be used (already used, unknown, expired or revoked), 4 something it needs is
unavailable (no runtime directory, another go-microsip owns the profile, no password, no
database), 1 otherwise, including a configuration that cannot be used.

## Command line

```sh
go-microsip status
go-microsip status --json
go-microsip call dial 1002
go-microsip operation get OPERATION_ID
go-microsip call hangup CALL_ID VERSION
go-microsip call answer CALL_ID VERSION
go-microsip call reject CALL_ID VERSION
go-microsip call mute CALL_ID VERSION
go-microsip call unmute CALL_ID VERSION
go-microsip call hold CALL_ID VERSION
go-microsip call resume CALL_ID VERSION
go-microsip call transfer CALL_ID VERSION 1003
go-microsip call dtmf CALL_ID VERSION 12#
go-microsip dnd on --key my-key-1
go-microsip dnd off
```

`call dial` prints the operation id and the idempotency key; `operation get` then says
`awaiting_approval`, `executing`, `succeeded`, `failed`, `denied`, `expired` or
`outcome_unknown`. `CALL_ID` and `VERSION` come from `status`: a request names the call as you
saw it, so it never lands on a call that replaced it. `--key` gives the key of a change, for a
safe retry; without it a random one is used and printed. `--json` prints the answer as one JSON
object.

A number or DTMF digits given as `-` are read from the standard input, which keeps them out of
the process list:

```sh
echo 1002 | go-microsip call dial -
echo 1003 | go-microsip call transfer CALL_ID VERSION -
echo 12# | go-microsip call dtmf CALL_ID VERSION -
```

These need a read grant:

```sh
go-microsip devices list --grant-file FILE
go-microsip config show --grant-file FILE
go-microsip contacts search --grant-file FILE
go-microsip contacts search ana --limit 5 --grant-file FILE
go-microsip history list --filter missed --limit 20 --grant-file FILE
go-microsip history list --cursor o:20 --grant-file FILE
```

`--limit` is 1 to 100 (20 without it); a page that has more answers with a `next_cursor` to pass
as `--cursor`. `--filter` is `all`, `incoming`, `outgoing` or `missed`.

Exit statuses: 0 done or accepted, 2 invalid input or conflict, 3 waiting for approval or not
authorized, 4 not running or unavailable, 5 timeout, 1 internal error. `go-microsip --help`
lists every command.

## MCP (agents)

Configure your MCP client to start a local stdio server:

```json
{ "command": "/absolute/path/to/go-microsip", "args": ["mcp"] }
```

or, with the clients that have a command for it:

```sh
claude mcp add go-microsip -- go-microsip mcp
codex mcp add go-microsip -- go-microsip mcp
```

Use the absolute path of your binary or AppImage; go-microsip installs nothing and changes no
agent's configuration. To let the agent use a grant, start the server with its file:

```sh
go-microsip mcp --grant-file FILE
```

Tools: `status_get`, `operation_get`, `devices_list`, `contacts_search`, `history_list`,
`config_show`, `call_dial`, `call_answer`, `call_reject`, `call_hangup`, `call_dtmf`,
`call_mute`, `call_hold`, `call_transfer`, `dnd_set`. A change returns `approval_required` with
an `operation_id`; poll `operation_get` until the person decides. Every change takes an
`idempotency_key`. The standard output carries only MCP messages; input lines are limited to
1 MiB and tool calls to 10 per second. The server needs no display.

## What is recorded

The local database keeps each change for 24 hours (the operation, its state, a sanitized outcome
and a keyed hash of the request: never digits or whole numbers) and a sanitized audit trail for
90 days, with a destination as its last two characters. The design is the Rust application's,
re-implemented: see [ADR 0013](adr/0013-agent-cli-mcp.md) and design decision D10 of
[migrate-to-go-wails](../openspec/changes/migrate-to-go-wails/design.md).
