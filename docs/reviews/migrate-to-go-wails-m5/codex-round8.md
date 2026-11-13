UNSOUND

## Status of round 7’s findings

| # | Status | Assessment |
|---|---|---|
| 1 | **Resolved** | The source check now precedes parsing and transaction matching on both UDP socket paths and the TCP/TLS readers. The matching-CANCEL bypass is closed. |
| 2 | **Resolved** | Quoted commas, semicolons and escaped quotes no longer manufacture a separate SIP cause. The added negative cases exercise this. |
| 3 | **Resolved in part** | The switch prevents the reported duplicate cleanup, but `Ended` can also mean a local ACK retransmission failure—not completed remote teardown. See finding 1 below. |
| 4 | **Resolved in part** | Connection order is preserved correctly. The new fixture does not actually test reversing two existing connections. See finding 3. |
| 5 | **Resolved** | Missing and certificate-free bundles both return `ErrNoCABundle` and become nonretryable certificate failures. Both cases are tested. |
| 6 | **Resolved** | Unconsumed markers now expire after one minute. The callback is safe after endpoint closure, although expiry and closure have no dedicated test. |
| 7 | **Resolved in part** | With a fresh marker, observation follows a successful PBX response write. The global marker can belong to an earlier fixture. See finding 4. |

## New findings

### 1. Major — local `Ended` state can suppress cleanup still owed to the peer

**Area:** crossing-answer cleanup.  
**Files/functions:** `internal/core/sip/endpoint.go:491–501`, `DialEarly`; `third_party/diago/dialog_client_session.go:379–384`, `waitAnswer`; pinned sipgo `WriteAck`, `WriteBye` and `endWithCause`.

`Ended` does not invariably mean that a BYE was sent or received. Sipgo also enters that state when replaying an ACK fails. Its `WriteBye` subsequently returns without sending anything when it sees `Ended`. [Pinned dialog client source](https://raw.githubusercontent.com/emiago/sipgo/v1.7.0/dialog_client.go), [pinned dialog state source](https://raw.githubusercontent.com/emiago/sipgo/v1.7.0/dialog.go)

**Concrete failing execution:** an incompatible answer starts the library’s ACK/BYE cleanup. The first ACK write succeeds locally but its datagram is lost. That cleanup goroutine is delayed before BYE. A retransmitted 200 triggers an ACK replay whose write fails during a temporary route outage, setting local state to `Ended`. The route recovers and cancellation becomes visible before cleanup resumes. Both the library’s BYE and the endpoint’s new `Ended` branch now send nothing. The peer still has the call.

This requires an unusual failure sequence, but it leaves a call up on one side, so it meets the requested major threshold.

**Fix:** distinguish successful/remote teardown from local error termination. Permit bounded cleanup for the latter without reviving dialogs already ended by BYE. Add a controlled ACK-replay failure test.

### 2. Major — the new response filter can reject the registrar selected by the transport

**Area:** registration and signaling availability.  
**Files/functions:** `internal/core/sip/endpoint.go:233–238,259–266,566–579`, `NewEndpoint`, `fromRegistrar`; `reference/sipgo-v1.7.0/sip/transport_layer.go:403–409,662–695`, `ClientRequestConnection`, `resolveAddrIP`.

The endpoint records one DNS answer. Sipgo independently resolves the hostname when selecting a request’s destination. The new filter also applies to **responses**, so those two resolutions must now agree.

**Concrete scenario:** a load-balanced registrar hostname returns address A during `NewEndpoint` and address B during REGISTER’s transport lookup. Both use the same local interface and the configured port. REGISTER reaches B, but B’s challenge or 200 is discarded because `e.peers` contains only A. Registration fails despite a reachable registrar. Subsequent changing DNS answers can repeat this or break renewal.

This is a new response-path failure, not the previously documented limitation where successive answers require different local interfaces.

**Fix:** make destination selection and filtering share the same authorized resolution result, preserving the hostname for SIP addressing and TLS verification. Alternatively, authorize the transport’s selected destination before its reader starts. Do not learn trusted peers from unsolicited incoming packets. Test successive disjoint DNS answers on the same route.

### 3. Minor — the connection-order regression test passes with the old implementation

**Area:** SDP regression coverage and review evidence.  
**File/function:** `third_party/diago/gomicrosip_samemedia_test.go:10–12,47–49`, `TestSameMedia`.

The baseline has one connection, A. The purported reversal fixture has two connections, B then A. Even the old unordered comparison distinguishes `{A}` from `{A,B}`.

**Concrete scenario:** restore the old set comparison for connection lines. This new case still passes, although the original defect—A,B comparing equal to B,A—returns.

**Fix:** compare descriptions containing the same two connection lines in opposite orders. The production fix itself is correct. The review record’s statement that this test contains the reported reversal case is too strong.

### 4. Minor — the PBX’s 202 marker can survive into another test fixture

**Area:** transfer-test synchronization.  
**Files/functions:** `internal/core/app/run_test.go:43,116–119`, `sent`, `labWith`; `internal/core/app/parity_test.go:56–59`, `TestATransferIsNotASuccessOn202Alone`.

The marker is process-global and keyed only by TCP port. It is never cleared.

**Concrete scenario:** repeat the test in one process and let a later fixture reuse the earlier fixture’s port. If the new REFER handling stalls or its response write fails, the old marker immediately satisfies `soon`. The two-second observation can finish without this fixture having sent 202—the original evidence gap returns.

**Fix:** clear that marker when creating the fixture and remove it during cleanup, or use a fixture-owned completion channel. A fresh-process run follows the intended synchronization.

## Checked and found supported

### Read-filter placement and transaction protection

The supplied reference sources support the following paths:

| Path | What happens before parsing |
|---|---|
| UDP listener | `TransportUDP.Serve` enters `readListenerConnection`, which invokes the filter. |
| UDP socket opened for sending | `createConnection` opens an unconnected `ListenPacket` socket; `readUDPConnection` enters the same filtered reader. |
| Dialed TCP connection | `CreateConnection` starts `readConnection`, which filters each read before `parseStream`. |
| Dialed TLS connection | TLS completes its handshake, then uses the same filtered TCP reader. |
| Accepted TCP/TLS connection | The inherited `Serve`/`initConnection` path also reaches that reader. The softphone does not start these listeners. |

References: `transport_udp.go:51–64,85–121,145–185`; `transport_tcp.go:81–90,98–160,181–237`; `transport_tls.go:40–89`.

An empty UDP result skips that datagram. An empty stream result skips that read without resetting the stream parser. An arbitrary content-dependent filter could therefore damage stream framing. **This filter is address-dependent:** a connection’s remote address is fixed, and the endpoint’s peer list is not changed while reading. It admits every read on an allowed connection or none on a refused connection. Another sender’s UDP traffic cannot become a read on the registrar’s established TCP/TLS connection.

With `AcceptAnySource == false`, bytes from an address/port outside the stored registrar set do not reach the parser or transaction layer through these paths. This includes matching CANCELs, other requests, retransmissions and responses. Supplied Via, Contact or Source-like header values cannot override the socket address used by the filter.

A packet actually forged with the registrar’s own source address and port remains outside this protection, as the receipt states. Plain UDP cannot authenticate that packet.

### Legitimate traffic and address handling

The predicate does not distinguish SIP methods. REGISTER, INVITE, BYE and REFER responses, and incoming NOTIFY, pass when their actual source matches an allowed address and the configured port.

Qualifications:

- Every address in the initial DNS answer is accepted—not merely its first address. Independent later resolution is finding 2.
- A UDP registrar answering from another port is refused, including its responses. That is stricter than the previous request-handler-only check. The implementation requires the configured source port; it does not establish compatibility with alternate-source-port servers.
- Ordinary bracketed IPv6 addresses parse, and `net.IP.Equal` handles IPv4-mapped equivalence. This does not establish working IPv6 accounts; IPv6 registrars are already documented as unsupported.
- A zoned host such as `fe80::1%eth0`, as preserved in an address string, fails `net.ParseIP` here. Zone handling is not implemented.
- TCP/TLS responses normally retain the fixed remote endpoint of the connection the softphone dialed.

### Work before refusal, and closure

For UDP, the filter runs **before** the per-source pool insertion and before parsing. A refused datagram cannot install its source as a reusable pool entry. Socket reading, buffer handling and optional SIP-debug logging already happened.

For streams, connection establishment—and pool registration—precedes filtering. Accepted connections are also pooled before their first filtered read. TLS handshaking precedes the filter. Thus this is a pre-parser filter, not a pre-connection admission filter.

The counter uses `atomic.Uint64`; the predicate reads fields that `Close` does not modify. Existing callers set `AcceptAnySource` before serving. I found no introduced filter/counter data race with `Close`. An already-running callback may finish during closure; the counter is not a synchronized final accounting snapshot.

The counter now counts refused UDP datagrams or stream reads, not necessarily SIP requests.

### Foreign-CANCEL test

The test targets the right socket. Through the loopback fallback, diago leaves its outbound UDP bind port at zero. Sipgo opens a separate unconnected sending socket. The fake registrar captures REGISTER’s actual source in `softphone`; INVITE and both CANCELs are sent back to that address—not to the separately advertised listener.

The socket is not kernel-connected to the registrar, so the intruder’s different source port is not rejected accidentally by UDP itself.

Without the read filter, processing the intruder’s packet should fail the test: its Via branch and sent-by match the live INVITE, and sipgo processes matching CANCEL before middleware. The registrar’s own CANCEL supplies a useful positive control for the same transaction key.

Nevertheless, **700 ms of quiet is a bounded observation, not proof that the foreign datagram was read and refused**. Successful UDP sending does not prove delivery. The legitimate CANCEL proves matching and normal cancellation, not delivery of the earlier foreign packet. Observing the refusal counter would strengthen that particular assertion. I did not execute the mutation or the test.

### Other answers

- `outsideQuotes` preserves escapes while ignoring escaped quotes inside quoted strings. An unterminated quoted text keeps the remaining delimiters inside that tail; a trailing backslash does not cause an out-of-bounds access. It is a separator scanner, not complete Reason-header validation.
- For ordinary crossing cancellation, the default dialog-state branch still permits ACK then BYE even without an established state. Confirmed cleanup avoids another ACK. Finding 1 concerns the additional meaning of `Ended`.
- Ordered connection comparison fixes the production SDP issue. The corrected “no keys” fixture now actually removes keys.
- The expiry callback only deletes from a `sync.Map`. Running after `Close` neither sends on a closed channel nor accesses released sockets. It retains the endpoint until expiry, but does not create a minute-long waiting goroutine.
- The 202 marker is written only after `tx.Respond` returns success. With a fresh marker, the observation follows the PBX’s successful write—not necessarily the client’s consumption of that response.
- The early-media comment now correctly says duplex sound is opened and that this scenario does not inspect outgoing silence.

### Review record, receipt and privacy

The source-filter placement claim is supported. The unconditional interpretation of ended-dialog cleanup needs finding 1’s qualification; the claimed reversed-connection fixture needs finding 3’s correction; transfer synchronization needs finding 4’s isolation.

I found no instance where the changed documents relabel a failed or explicitly unperformed run as a pass. Earlier failures and the requirement for quiet-machine reruns remain visible. The new five-run claim and the historical listener-only failure cannot be independently authenticated from this extract, because their underlying run artifacts are omitted.

I found no apparent operational account, private PBX hostname, real subscriber credential or personal home path. Full-length example telephone strings and contact names occur in fixtures/mock data; they appear synthetic. Public project/author identifiers, upstream licence emails and generic example home paths are present.

**Not checked:** nothing was built or run; omitted artifacts, real calls, devices and quiet-machine reruns were not verified.  
This review is not evidence that a call works.