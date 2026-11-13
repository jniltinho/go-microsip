SOUND WITH CHANGES

## Status of round 11’s findings

| # | Status | Assessment |
|---|---|---|
| 1 | **Resolved** | `registrarOnly` again checks requests on every transport. A foreign dialog-selected TCP peer can no longer deliver INVITE, BYE or NOTIFY through those handlers. Stream responses remain unfiltered. There is still no second-host TCP regression test. |
| 2 | **Resolved** | `register` rewrites Contact only before an authentication retry. An unchallenged 200 no longer changes the retained successful Contact. The NAT/contact-matching case has no dedicated test. |
| 3 | **Resolved** | The challenger now keeps bindings, deletes only the named Contact and rejects a distinct replacement while the old binding remains. The wrong-port removal described in round 11 would now prevent registration and fail the test. |

## New findings

### 1. Minor — the documented TCP/TLS request boundary omits transaction-layer exceptions

**Area:** signaling documentation.  
**Files/functions:** `README.md:232–239`; `docs/reviews/migrate-to-go-wails-m5/README.md:115`; `docs/receipts/go-m5-parity-2026-11-12.md:239`; `Endpoint.NewEndpoint`, `registrarOnly`; pinned `TransactionLayer.handleRequest`.

The README’s new statement that TCP/TLS requests are taken from the permitted addresses “and no others” is stronger than the implementation. The restored handler check cannot reject a matching CANCEL already consumed by the transaction layer. The receipt’s older, unqualified “nothing … is parsed” statement now applies only to UDP accounts.

**Concrete scenario:** a previous call’s Contact caused a TCP connection to B, outside the registrar allowlist. That connection remains open after the call. While a later incoming INVITE from A rings, B sends a CANCEL carrying the matching transaction information. `handleRequest` finds the INVITE transaction, writes its response through that transaction’s connection and delivers the cancellation before `registrarOnly` runs.

This was reachable **before round 7 too**. I am not reporting it again as a new runtime vulnerability; the finding is the stronger documentation claim.

**Fix:** distinguish UDP’s pre-parser protection from TCP/TLS application-handler admission. Explicitly retain the matching-CANCEL exception on streams. Do not restore byte filtering on TCP/TLS.

### 2. Minor — “refused once” understates what DNS refresh loses

**Area:** recovery documentation.  
**File/functions:** `docs/reviews/migrate-to-go-wails-m5/README.md:111`; `Endpoint.fromRegistrar`, `askAgain`, `registrarOnly`.

The record says a request from a newly selected registrar address is “refused once.” Refresh is asynchronous, can fail, and does not save or replay the refused request. Several requests can be refused before the address is learned.

**Concrete scenario:** the endpoint resolves A, but the library registers over TCP at B. Registration succeeds because responses bypass the handler check. B then sends an incoming INVITE. The handler terminates that request’s transaction and starts another lookup. Even if the lookup immediately adds B, the discarded INVITE is not replayed; reliable-transport request retransmission does not rescue it. That incoming call can be lost, not merely delayed by the lookup.

**Fix:** say requests are refused **until a successful lookup includes their source**, and that already refused stream requests are lost. DNS refresh authorizes future traffic; it does not recover the earlier signaling action.

## Checked and found supported

### Parser, transaction and application reachability

Here, “foreign” means outside the endpoint’s accumulated resolved-address set, or at another port.

| Transport | Parser | Transaction layer | Application handlers |
|---|---|---|---|
| **UDP** | Foreign datagrams are discarded first, on both listener and library-created sending sockets. | Nothing from those discarded datagrams reaches it. | Nothing reaches call handling. |
| **TCP** | All SIP bytes received on an opened connection reach parsing. There is no listener, but Contact/Route can select an additional peer. | Responses and requests reach transaction handling. Matching responses can complete transactions; matching CANCELs can act before middleware. | Registered diago handlers refuse foreign-source requests. Transaction effects can nevertheless reach the application indirectly, notably cancellation. |
| **TLS** | As TCP, after a successful TLS handshake. | As TCP. | As TCP. TLS additionally requires the configured trust roots and fixed registrar `ServerName`; an unrelated Contact host’s ordinary certificate is insufficient. |

Unhandled methods can also reach the pinned server’s default 405 path; middleware wrapping diago’s registered handlers is not a universal pre-transaction firewall.

**Compared with before round 7:**

- UDP is stronger: previously foreign bytes reached parsing and transaction matching, despite handler rejection.
- TCP/TLS retain the old handler boundary and the old pre-handler transaction exceptions. These changes do not newly introduce the matching-CANCEL exposure.
- The accepted address set can now grow through DNS refresh; before these rounds it remained the initial set.

A response on a connection opened to a dialog’s Contact is therefore parsed and can satisfy a matching transaction on TCP/TLS. Restoring request admission does not block that legitimate response path.

For an unknown address on the registration’s own connection:

- REGISTER responses remain usable.
- Ordinary requests reaching middleware are discarded until that address is learned.
- The lookup has a five-second deadline and a cooldown, but there is no guaranteed overall recovery time.
- A discarded INVITE can mean a missed call. A discarded BYE can leave local call state awaiting another ending event or user action. Refresh alone repairs neither transaction.
- Before round 7, subsequent handler-bound requests from that address remained refused for the endpoint’s lifetime.

A packet forged with the registrar’s permitted source address and port remains outside what this non-TLS source check can distinguish.

### Contact rewriting and removal

`third_party/diago/register_transaction.go` supports the repair:

- **Challenged initial registration:** `req` is `Origin`. `ReplaceHeader(contact)` changes that request before `DoDigestAuth` sends it. The retry and retained Origin therefore carry the same Contact.
- **Unchallenged 200 behind NAT:** the connection-populated Contact actually sent remains in Origin and is later named by unregister. Via’s observed public address no longer silently replaces it.
- **Outgoing INVITE Contact:** constructed independently through `contactHDRFromTransport` and the dialog/connection setup—not copied from registration Origin.
- **Contact in incoming-call 200 answers:** likewise comes from that dialog’s separately constructed Contact header, not the rewritten registration Contact.
- **Renewal:** `Qualify` calls `doRequest` with Origin. Even another 401/407 uses digest authentication without invoking `register`’s NAT rewrite. It does not change Contact between renewal and removal.
- **Removal:** preserves Contact, replaces Expires with zero, and stops and joins renewal first.

Thus removing the post-200 rewrite does not remove a shared NAT address on which INVITE construction depended. It also does not establish general NAT reachability: the README’s PBX/topology limitations still apply.

The supplied header implementation confirms that replacement updates both the ordered header and its cached reference. I found no new duplicate Contact or Expires path here.

### The strengthened registrar test

`challenger.serve` now does substantially more than record request order:

1. Records the authenticated registration’s Contact.
2. Deletes only an exact matching binding.
3. Rejects a different Contact while another binding exists.
4. Checks the observed register/remove/register order and one remaining binding.

For the distinct Contacts exercised by these TCP endpoint replacements, the round 11 counterexample no longer passes: removing the wrong port leaves the old binding, and the replacement receives 403.

This remains a deliberately small fixture. Its wildcard handling is not a complete implementation of a real registrar, and the test does not cover unrelated phones, NAT rewriting, renewed challenges or final shutdown removal. Those limits do not defeat the specific regression assertion it now makes.

The record’s explanation that the tests’ registrars “always challenge” is inaccurate as a blanket statement: `registration_test.go` and `shutdown_test.go` contain unchallenged-200 fixtures. What remains absent is a **NAT-changing, binding-comparing unchallenged-200 test**.

### Registration and call lifecycles

I traced the requested endpoint paths, actor `connect`, `dropAccount`, `shutdown`, `release`, late-result cleanup and the marked diago changes.

Supported by inspection:

- Installed-endpoint replacement waits on the predecessor’s closure channel, bounded at six seconds.
- Removal precedes cancellation of the UDP serving context in ordinary replacement and shutdown.
- Generation checks prevent stale successful candidates from being installed.
- Shutdown separately budgets call termination, account closure and sound cleanup.
- Call release stops local audio and closes media to unblock readers.
- Crossing-answer cleanup distinguishes a remotely ended dialog from one locally ended with a failure cause.
- The marked media changes preserve the earlier malformed-packet, source/authentication-drop, jitter and equivalent-early-SDP protections.

**I found no additional, previously unreported defect with a concrete ordinary one-address Asterisk/FreePBX scenario meeting the requested major threshold.** I have not counted the expressly excluded limits again.

### Documents and recorded runs

Apart from the wording findings above:

- The review record identifies the withdrawn answers rather than presenting them as current fixes.
- It now correctly says the six-second replacement limit stops the **wait**, not necessarily the predecessor’s cleanup.
- The parity receipt distinguishes failed first runs, subsequent passing runs and unrepeated Windows/FreePBX work.
- The retained Asterisk report contains 79 scenarios and zero failures; the final AppImage log contains 30 passing subtests and the quoted audio counts.
- Those retained full runs are attributed to **`34c38e3`**, not these round 12 changes. Their summaries do not independently prove artifact/commit identity.
- I found no new passage presenting a failed or unrepeated run as a passing run of this patch.

**Cutover rehearsal:** it clearly confines the moves, replacement Makefile, flag change, builds and reversions to a disposable local clone. It explicitly leaves task 7.5 unchecked and excludes remote-clone, moved-tree lab, VM and CI validation. Nothing states that those clone-only edits were performed in the repository. The clone’s rename counts, history scan and build results cannot be independently reconstructed from this extract.

### Publication scan

I found no apparent private operational SIP account, PBX name, credential or personal home path.

The extract does contain fictional accounts and passwords, telephone-shaped test literals, lab addresses, generic user paths and public upstream author identities. The SQLite fixture’s readable contents agree with its documented fictional configuration. I have not established that every telephone-shaped fixture value is unassigned.

**Not checked:** no builds, tests, scripts, applications, VMs, devices or calls were run; omitted history, clone artifacts and binary identity were not verified.  
**This review is not evidence that a call works.**