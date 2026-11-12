UNSOUND — revocation can silently leave a capability usable after the person presses Revoke. The supplied passing checks do not exercise that failure.

## Findings

1. **Major — failed revocation is reported as success.**  
   `internal/control/grants.go:322–325`, `Owner.Revoke`; `internal/gui/service.go:731–737`, `App.RevokeGrant`.

   If the revocation UPDATE fails—for example, SQLite returns `SQLITE_FULL` while extending its journal—the error is discarded and the window receives success. Reads can still work, so the existing capability continues returning contacts or history after the person attempted to withdraw permission.

   **Fix:** propagate the failure to the page and explicitly warn that the grant remains usable. Fail closed for that capability in the current owner, or stop agent control. Add a test where revocation fails but grant lookup still succeeds. This defect is inherited from Rust, not justified by parity.

2. **Minor — closing the owner does not consistently close its request lifecycle.**  
   `internal/control/owner.go:172–175,288–320,527–555`; `internal/control/endpoint_linux.go:125–130`.

   `Endpoint.Close` leaves existing connections open, and `Owner.Handle` does not check `closed`. An already-connected client can therefore submit another granted request after `Owner.Close`.

   There is also a definite loop bug: close an owner with a pending approval, then call `DenyAll`. `decide` refuses to remove the approval because the owner is closed, while `DenyAll` loops until the list is empty. A late window callback can consequently hold the owner mutex and block event forwarding during shutdown. Its application-level trigger is a shutdown race, hence minor under the requested calibration.

   **Fix:** reject requests after close and make `DenyAll` terminate on a closed owner; clear or finalize pending requests during close.

3. **Minor — agent changes to DND do not reach the window’s indicator.**  
   `internal/control/owner.go:626–635`, `execute`; `internal/core/app/phone.go:371–385`, `SetDND`; `frontend/src/store.ts:42–62`; `frontend/src/state.ts:289–293`.

   Start with DND off, then approve an agent’s `dnd on`. The backend changes it, but no DND event updates `ui.dnd`: the window still shows off. Pressing its toggle then sends “on” again rather than turning it off.

   **Fix:** publish the shared DND state to the window after either frontend changes it. Test the window’s state after a control-socket DND request, not only the socket’s status.

4. **Minor — pagination silently stops at 1,000 records.**  
   `internal/control/grants.go:130–169`, `grantedRead`; `internal/core/app/phone.go:64,511–517,547–552`.

   Both listings first fetch the window’s fixed maximum of 1,000 records, then paginate that truncated slice. With 1,001 history entries, the page at `o:980`, limit 20, reports `next_cursor: null`; the remaining entry is unreachable.

   **Fix:** let the application layer fetch enough records for the requested offset, page and look-ahead, or provide actual paged queries. Add a test exceeding 1,000 entries.

5. **Minor — an invalid bootstrap is rejected only after telephony starts.**  
   `cmd/service.go:58–78`, service startup; `internal/core/app/phone.go:115–155`, `OpenPhone`.

   With a configured password, starting `service` using a revoked or consumed bootstrap calls `OpenPhone`, which starts the engine and queues registration, before `NewServiceOwner` rejects the bootstrap. This can briefly register and unregister even though startup ultimately exits 3.

   Rust consumes the bootstrap before starting its engine. The receipt’s “a revoked one starts nothing” is established only for the owner constructor, not the service command.

   **Fix:** validate and consume the bootstrap before starting telephony. Test the rejected-service path for absence of registration traffic. I found no call-placement or capture bypass in this path.

6. **Minor — the approval delay measures receipt, not visibility.**  
   `frontend/src/state.ts:502–507`, approval event handling; `frontend/src/App.vue:171–227`.

   An incoming-call dialog hides agent approvals. If an approval arrives while the phone rings and the ringing dialog remains for several seconds, Allow is already armed when the approval finally becomes visible. That contradicts the documented one second **on screen**.

   **Fix:** start or reset the arming timer when that approval dialog becomes visible, including after an incoming-call dialog disappears.

7. **Minor — malformed trailing JSON can pass framing validation.**  
   `internal/control/protocol.go:512–517`, `ReadMessage`.

   Append an extra `}` before the newline of an otherwise valid request. `decoder.More()` returns false on a closing brace, so the malformed line is accepted. `More` is not an end-of-document check. Authorization still applies to the decoded request; this is not a consent bypass.

   **Fix:** after decoding the message, require another decode to return `io.EOF`. Add trailing `}` and `]` cases alongside the existing trailing-object test.

8. **Minor — the documentation overstates what pausing and expiry stop.**  
   `docs/agent-cli-mcp.md:39–45,82–86`; `internal/control/owner.go:442–445`; `internal/control/grants.go:117–127`.

   After **Deny all and pause**, an existing read grant still returns contacts/history: only new mutations check the pause. Already accepted DTMF also continues. After service-grant expiry, `operation get` still works, despite “answers only status.”

   Additionally, “anything else still asks” excludes unknown, revoked or expired capabilities: those are rejected outright, rather than falling back to a prompt.

   **Fix:** document these distinctions explicitly, or broaden the implementation to match the promise. The continued reads during pause still have an active read grant, so this is not an additional authorization bypass.

9. **Minor — JSON mutations lose their automatically generated retry key.**  
   `cmd/control.go:96–112`, `ask`; `docs/agent-cli-mcp.md:113–118`.

   Run `call dial 1002 --json` without `--key`. A random key is generated, but only the response is encoded; it contains no idempotency key. Unlike human output, the caller cannot recover the key for a safe retry—particularly important after a timeout.

   **Fix:** expose the generated key appropriately, or document and require an explicit `--key` for safely retryable JSON mutations.

10. **Minor — the receipt calls mock-backend checks a form walkthrough.**  
    `frontend/src/grants.test.ts:6–53`; `frontend/scripts/prints.sh:167–173`; receipt, Task 6.5.

    The unit test calls `createMock()` methods directly. The screenshot script renders Agent access and opens its audit log; it does not fill and submit the grant form. A broken `create` button or omitted form field could survive these checks.

    **Fix:** describe this as mock-backend testing plus rendered-screen checks, or actually drive creation, export, revocation and clearing through the form. The receipt correctly leaves the built-window walkthrough to 6.7.

## Unrecorded differences from Rust

The receipt and cited parity rows should additionally record:

- **Pagination:** Go truncates both collections at 1,000; Rust fetches according to the requested offset and page.
- **DND state ownership:** Rust updates the UI state that also supplies control status; Go changes the phone without notifying the page.
- **Rejected service startup:** Go starts telephony before bootstrap authorization; Rust authorizes first.
- **Shutdown:** Go’s existing connections can continue calling its closed owner.
- **Parsing:** Go accepts the trailing delimiters above. Its ordinary JSON struct decoding also accepts case-insensitive envelope-field aliases that Rust’s contract rejects.

Finding 1 is shared with Rust. The already-recorded differences—runtime-directory election, immediate grant execution, independent DTMF pacing, oversized-line disconnection and client-side timeout—are accurately identified.

## Checked and found supported

- **Main consent path:** mutations require approval or a matching grant. Scope, exact dial/transfer destination, ringing caller, quota and epoch checks are present. Personal reads require their separate scopes.
- **Idempotency:** same-epoch payload comparison uses `hmac.Equal`; replay returns the existing operation rather than executing again. Retained earlier-epoch keys are rejected. Execution requires a successful durable ledger update first.
- **Approvals and DTMF:** late approval is rejected; call identity/version is rechecked before execution. DTMF is paced, checks that its call remains established, and persists counts rather than digits.
- **Transport:** private directory/socket handling, stale-socket ownership checks, symlink refusal and kernel UID checks at both ends support same-user isolation. Ordinary Rust clients/owners cannot speak this endpoint’s protocol. This is not protection from a hostile process already running as the user.
- **Secrets:** capabilities use cryptographic randomness, are hashed in the database, and leave through exclusive private-file creation. Readers check file ownership, permissions, identity and size. The reviewed control responses, page grant objects and audit construction do not disclose capabilities or SIP passwords. Configuration output is explicitly selected, not a serialization of credentials.
- **Concurrency:** `changed` callbacks run outside the owner mutex; the examined normal window callback paths do not create a lock cycle. Deferred endpoint/owner/phone closes are ordered correctly, subject to finding 2.
- **Evidence:** the three lab walkthroughs and supplied logs support the stated approval, denial, replay, CLI/MCP mapping, service-grant and audit scenarios. VM step 6 supports coexistence, distinct endpoints and unchanged profile files—not telephony in those VMs.
- **Documentation checks:** the receipt accurately distinguishes executed control commands from commands merely parsed against the command tree. Neither that test nor the lab establishes every documented command on every packaged artifact. The stated Windows, real-headless-session, oversized-MCP-line and built-form limitations are appropriately retained.
- I found no non-fictional credential or personal home path in the reviewed change and named logs.

**Not checked:** no builds, tests or applications were run; no screenshots or packaged binaries were independently validated. The omitted engine/SIP/audio implementations and MCP SDK were not available for a full downstream verification.