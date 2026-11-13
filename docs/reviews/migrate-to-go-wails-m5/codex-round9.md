UNSOUND

## Status of round 8’s findings

| # | Status | Assessment |
|---|---|---|
| 1 | **Resolved in part** | A published ACK-retransmission failure now takes the ACK/BYE cleanup path. However, `Ended` becomes visible before its cause does; the new nil-cause branch retains a narrow version of the defect. Finding 2 below, **minor** under this round’s calibration. |
| 2 | **Resolved in part** | REGISTER, renewal, unregister, initial INVITE and authentication retries retain the numeric destination. The CANCEL actually used by this softphone does not. Finding 1 below. |
| 3 | **Resolved** | The test now compares A,B against B,A. Restoring the old unordered comparison would fail that assertion. |
| 4 | **Resolved** | `labWith` deletes the previous port’s marker before starting its server. An earlier fixture’s completed 202 no longer satisfies the later fixture’s wait. |

## New findings

### 1. Major — CANCEL still performs an independent destination lookup

**Area:** outgoing-call cancellation.  
**Files/functions:** `internal/core/sip/endpoint.go:474`, `DialEarly`; `reference/sipgo-v1.7.0/dialog_client.go:337–388,596–606`, `inviteCancel`, `newCancelRequest`; `reference/sipgo-v1.7.0/sip/transport_layer.go:395–449`, `ClientRequestConnection`.

The initial INVITE receives `e.dest`. But `inviteCancel` constructs a fresh CANCEL using the helper in **`sipgo/dialog_client.go`**, which copies the recipient, headers, source and local address—not the destination.

There is another `newCancelRequest` in **`sip/request.go`** that copies the destination. That is not the helper this dialog path calls.

With the endpoint’s default `RewriteContact == false` and an ordinary response without Record-Route, the fresh CANCEL reaches transport selection with the registrar hostname as its destination. Transport resolves it again.

**Concrete scenario:** the hostname initially returns A and B. Registration and INVITE are pinned to A. The user hangs up while A is ringing the destination. The CANCEL lookup selects B, another PBX/frontend without A’s transaction. B returns a failure, and the softphone finishes cancellation locally; A never receives CANCEL and continues ringing. An answer there can leave the other side in a call this softphone has abandoned. Both addresses may be in `peers`; no forged packet or filter bypass is needed.

This weakness existed in the library path before pinning. The new answer nevertheless leaves the claimed transaction-wide pinning incomplete.

**Fix:** preserve the INVITE’s numeric destination in the CANCEL constructor actually used, through the real dependency integration—not by editing the unused reference helper. Add a controlled changing-resolver test that checks where CANCEL arrives. Correct the README, receipt and review-record claims until that is true.

### 2. Minor — `Ended` and its cause are not published together

**Area:** crossing-answer cleanup.  
**Files/functions:** `internal/core/sip/endpoint.go:519–527`, `DialEarly`; `reference/sipgo-v1.7.0/dialog.go:114–121`, `endWithCause`.

`endWithCause` first swaps the state to `Ended`, then cancels the dialog context with the error. Between those operations, the endpoint can observe `Ended` with a nil cause and incorrectly classify it as completed teardown.

**Concrete execution:** during incompatible-answer cleanup, an ACK replay fails. Its goroutine publishes `Ended` and is descheduled before `d.cancel(err)`. The library’s cleanup sees `Ended` and skips BYE. With the call context canceled, `DialEarly` then sees `Ended` plus nil cause and skips cleanup too. Publishing the error afterwards does not send the missing BYE.

This requires the failure, cancellation and precise scheduling overlap; it is **minor** under the requested calibration.

**Fix:** when observing `Ended`, wait boundedly for the initialized dialog context’s termination before classifying its cause, or obtain a consistent termination result from the dependency. Exercise this publication window deterministically rather than relying on a timing-based call test.

### 3. Minor — the README describes a narrower inbound allowlist than the code implements

**Area:** documented signaling boundary.  
**Files/functions:** `README.md:232–236`; `internal/core/sip/endpoint.go:593–607`, `fromRegistrar`.

“Nothing is read that does not come from the address and port the account registered at” is not the implemented rule. The filter accepts **every address in the connection-time resolution**, at the configured port.

**Concrete scenario:** the name resolves to A and B, registration uses A, and a request arrives from B at the configured port. It passes the filter despite not coming from the address registration used.

**Fix:** describe the complete resolved-address set and configured port explicitly. Distinguish that inbound set from the single chosen outbound destination. This is a wording correction, not a recommendation to remove the intended multi-address allowance.

## Checked and found supported

### Out-of-dialog routing

The registration fix is effective for the following paths:

| Operation | Source-level result |
|---|---|
| Initial REGISTER | `ProxyHost: e.dest` becomes `Origin.SetDestination(e.dest)`. |
| REGISTER renewal | `Qualify` reuses `Origin`; rebuilding Via does not remove its destination. |
| Binding removal at `Close` | `Unregister` modifies that same request’s Contact and Expires, retaining its destination. |
| REGISTER retry after 401 or 407 | Both digest paths reuse the request and retain its destination. |
| Initial INVITE | `DialEarly` sets its destination after `NewDialog`. |
| INVITE retry after 401 or 407 | `WaitAnswer` reuses `InviteRequest`; rebuilding dialog headers and Via does not clear the explicit destination. |
| CANCEL | **Not pinned**, as finding 1 explains. |

For the pinned paths, `resolveRemoteAddr` parses the numeric address directly. TCP/TLS subsequently dial a numeric socket address. There is no further hostname lookup on those paths.

**ACK to a failure response needs a qualification:** the supplied `newAckRequestNon2xx` copies neither the explicit destination nor `raddr`. The extract shows the original connection being passed into `ClientTx`, but omits the client-transaction ACK sending implementation. Therefore I cannot establish whether that implementation supplies the original numeric remote address before writing the ACK. Neither a fresh lookup nor correct destination preservation should be inferred from the factory alone—especially for UDP.

Consequently, the review record’s **“the library resolves nothing for them” is already disproved by CANCEL**, and the failure-ACK portion is not fully established by the supplied sources.

### Requests inside a dialog

These requests are not pinned merely because the initial INVITE was pinned:

- **ACK to 2xx:** diago builds a fresh request to the response’s Contact.
- **BYE:** sipgo builds it using the INVITE response’s Contact.
- **REFER:** diago uses the current remote Contact.
- **Hold/resume re-INVITE:** diago likewise uses the current remote Contact.
- **Routing:** on the supplied client-dialog path, Record-Route supplies the route set and can determine the next hop instead of Contact. `RewriteContact` is false here.
- **Answer to NOTIFY:** this is `tx.Respond`, not a new Contact-addressed request. Transaction creation selects the return connection using the request’s actual source and the transport’s Via/rport rules.

A Contact or Route containing a hostname can therefore trigger another lookup. These requests can go to an address or port outside the inbound allowlist, and their responses can then be refused.

For example, registration through one PBX address followed by a Contact advertising another signaling address or port can make BYE or hold responses disappear. This is the documented alternate-address/port limitation, not another finding. Pinning does not eliminate it.

For ordinary single-address Asterisk/FreePBX signaling, with Contact/routing and packet sources using an allowed address and the configured port, I found no new pinning-related incompatibility. More complex advertised-address, proxy or split-source deployments cannot be covered by that statement.

The extract omits sipgo’s server-dialog implementation, so I could inspect diago’s incoming-dialog request construction but not independently verify all corresponding inherited routing methods.

### TLS identity, SIP addresses and connection reuse

**TLS verification remains against the configured server.** `TLSConfig` sets `ServerName: s.Server`. The TLS transport substitutes its numeric destination only when `ServerName` is empty, which it is not here.

`SetDestination` changes transport metadata, not the serialized SIP addresses:

- REGISTER’s request URI keeps `s.Server`; its To and From use the account’s address-of-record domain.
- INVITE’s request URI and generated To keep `s.Server`; From uses `s.Domain`.
- The pre-existing distinction between the account’s domain and its server remains unchanged.

For a registrar configured as an IP literal, the destination remains numeric. TLS consequently requires a certificate valid for that IP; pinning does not turn it into a different certificate identity.

**TCP/TLS reuse is not broken by replacing a hostname destination with an IP.** The transport resolves before pool lookup and keys connections by numeric remote address and port. The pool also records the local-address alias. Pinned requests therefore find the registration connection under the same key.

Incoming requests on that connection retain its actual remote endpoint and pass the filter when it is allowed. No TCP/TLS listener was added or required. A dialog Contact resolving elsewhere can select another connection; that is distinct from failure to reuse the correctly matched registration connection.

### IPv6, unavailable addresses and SRV

- **IPv6-only registrar:** `destFor` formats the selected IPv6 address correctly. That is not complete IPv6 support: local binding and media selection still follow the existing IPv4-oriented path. In particular, UDP can retain an IPv4 local socket while targeting IPv6. The helper’s IPv6 test does not establish a working IPv6 account.
- **Several addresses, first unavailable:** neither old transport selection nor `destFor` walks the address list after a connection or transaction failure. The old library selected one preferred IPv4 address, otherwise the first address. There was no general per-request address failover to preserve.
- **Repeated DNS selection:** before pinning, another request’s lookup could select a different address. That opportunistic reselection is now removed from the pinned paths. A new endpoint resolves again on reconnect. Re-registration while a call retains the existing endpoint does not.
- **SRV:** sipgo defaults to IP lookup first and falls back to SRV if that lookup fails. This endpoint already required its own successful `net.LookupIP` before reaching sipgo. Thus an ordinary SRV-only registrar was not supported previously, and a stable successful A/AAAA lookup did not use SRV. Pinning removes the later fallback opportunity; I found no established common SRV capability that these changes newly remove.

Overall, I found no reason that a conventional single-address Asterisk/FreePBX account should stop registering or calling merely because its destination is numeric metadata. The response filter can exclude previously working alternate-source deployments, as now documented. Multi-address cancellation remains the acceptance blocker described above.

### The cause test

For a normally initialized outbound dialog:

- `NewDialog` calls `Init`, which creates a nonnil context.
- That context is independent of the dialing context. Canceling the user’s dial does not itself give the dialog a cancellation cause.
- An uncanceled dialog has a nil cause, but unless its state is `Ended`, it still takes the cleanup path.
- Normal BYE teardown calls `cancel(nil)`, yielding **`context.Canceled`**, not a permanently nil cause.
- Failed ACK retransmission calls `endWithCause` with its wrapped write error.

Thus the intended distinction works once the result has been published. It is not universally true that “BYE means nil/Canceled and every library failure means another cause”: finding 2 exposes the publication gap; cancellation causes retain their first value; and the supplied server-side code also uses `endWithCause(nil)` for early transaction termination.

For the intended failed-ACK-repeat case, successful `WriteAck` restores `Confirmed`, allowing `WriteBye` to send using the fresh cleanup context. It does not uncancel the old dialog context or reopen the microphone. I found no additional harmful behavior in that stable execution.

### The two corrected tests

Both corrections address the reported false-pass mechanisms:

- `TestSameMedia` now uses the same two connection lines in opposite orders. Its positive self-comparison also prevents simply rejecting every two-connection description.
- `labWith` clears the exact stale marker before its new PBX can receive REFER. The marker is still written only after successful `Respond`.

The transfer observation establishes a successful PBX response write, not necessarily client consumption of that response. That distinction remains unchanged.

### Documents, runs and publication scan

The receipt and review record overclaim transaction-wide pinning, as finding 1 explains. The record’s “no cause” description should also say ordinary context cancellation rather than literal nil.

I found no changed passage that clearly relabels a failed or explicitly unperformed run as a pass. The earlier walkthrough failure, later pass, loaded-machine qualification and outstanding quiet-machine reruns remain visible. The named `3f64b5d` runs precede these round-8 answers; they are not evidence that the new branches work. The new resolver test checks address choice only, not changing DNS during real transactions.

The store changes preserve the URI-escaping checks using fictional paths. The Dockerfile change only corrects a comment’s script name.

I found no apparent operational account, private PBX hostname, subscriber credential or personal home path. The extract contains plausible example telephone numbers and mock contact names, fictional lab accounts, generic home paths, and public project/upstream author identifiers and licence emails. Their presence is not evidence of private subscriber data.

**Not checked:** no builds, tests, labs, calls or devices were run; omitted run artifacts and the missing transaction/server-dialog implementations were not verified.  
**This review is not evidence that a call works.**