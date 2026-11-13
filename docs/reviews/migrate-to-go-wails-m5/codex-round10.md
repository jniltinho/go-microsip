UNSOUND

## Status of round 9’s findings

| # | Status | Assessment |
|---|---|---|
| 1 | **Not resolved** | The pinning answer is withdrawn, not repaired. Removing `dest`, `ProxyHost` and `SetDestination` restores the pre-round-8 **outbound destination-selection path**: sipgo resolves and routes requests. The documented CANCEL limitation is real, particularly over UDP without a route fixing the next hop. I do not count that disclosed library limitation again. |
| 2 | **Resolved** | Waiting for the ended dialog’s context closes the ordinary state-before-cause publication window. This is bounded protection, not an unconditional guarantee: if the three-second deadline wins, the code still reads the cause without having observed context termination. |
| 3 | **Resolved in part** | The README no longer claims one registered address. However, it now describes addresses the name **has**, whereas the implementation accepts addresses it **has ever returned during this endpoint’s lifetime**. See finding 3. |

The CANCEL warning needs one qualification: not every request necessarily follows the account hostname. Dialog Contact/Route information can choose the next hop, and retransmissions reuse their transaction’s destination. The stated possibility of INVITE and CANCEL reaching different servers remains accurate.

## New findings

### 1. Major — DNS refresh cannot recover a discarded TCP/TLS REGISTER response

**Area:** registration and stream admission.  
**Files/functions:** `internal/core/sip/endpoint.go:265–271,583–625`, `NewEndpoint`, `fromRegistrar`, `askAgain`; `reference/sipgo-v1.7.0/sip/transport_tcp.go:164–255`, `readConnection`, `parseStream`; `internal/core/engine/actor.go:238–270`, `connect`.

`fromRegistrar` refuses the current read and starts asynchronous resolution. That can work with UDP retransmission. It does not recover a response already consumed and discarded from a TCP/TLS connection.

**Concrete scenario:**

1. Endpoint construction resolves the registrar to A.
2. sipgo’s independent lookup returns B, and the softphone successfully connects to B.
3. B sends its REGISTER challenge.
4. The filter discards it and starts `askAgain`.
5. Resolution returns B, adding it to `peers`, but TCP delivery has already completed. There is no SIP-level retransmission of that challenge to admit.
6. Registration expires after ten seconds.

A rotating resolver that repeatedly gives endpoint construction A and sipgo B can repeat this on every fresh endpoint, exhausting registration retries. This is the unresolved TCP/TLS portion of round 8’s response-filter defect—not the disclosed CANCEL limitation.

If resolution completes between reads of a split message, the prefix is discarded and the suffix reaches a fresh stream parser. That can produce a framing error and close the connection. It is not an additional demonstrated outsider authentication bypass, and refusing the whole connection would also lose this registration; silently deleting part of a stream is nevertheless not recovery.

**Fix:** validate/admit the actual outbound TCP/TLS peer before discarding its SIP bytes, or retain bounded pending stream data while validation completes. Reject an unverified stream as a connection, not as arbitrary byte fragments. Add controlled-resolution REGISTER tests over TCP and TLS; the new helper test does not exercise either path.

### 2. Major — replacement registration is not ordered after old binding removal

**Area:** account replacement.  
**Files/functions:** `internal/core/engine/actor.go:180–200,214–270`, `register`, `dropAccount`, `connect`; `third_party/diago/register_transaction.go:265–273`, `Unregister`; `internal/core/sip/endpoint.go:678–708`, `RegistrationFailure`.

The changed order preserves the old socket for unregistering, but `dropAccount` still returns immediately and `register` starts the replacement concurrently. The generation counter cannot order those requests at the PBX.

**Concrete scenario:** an idle user saves settings for the same account. The old endpoint’s removal is delayed—for example, by its lookup or authentication exchange—while the replacement REGISTER reaches the PBX first. With the lab’s actual `max_contacts=1`, `remove_existing=no` configuration, the replacement can receive 403 because the old contact remains. That becomes `Unauthorized`, so the engine does not retry. The old removal subsequently succeeds, leaving no registered contact.

There is also the opposite failure on a registrar that accepts the replacement first: the old endpoint sends `Contact: *`, `Expires: 0`, so its late removal can erase the new binding while the new engine state says Registered.

This asynchronous replacement race predates these lines; preserving the socket does not close it, and the generation checks do not protect remote bindings. Both operations follow from one account-save command; no hostile process or precise multi-failure coincidence is required.

**Fix:** sequence replacement registration after the previous binding-removal attempt, boundedly and off the actor, with a generation check before starting the replacement. Avoid allowing a late wildcard removal to erase a successor’s contact. Add a same-account replacement test that deliberately delays the old unregister.

### 3. Minor — the README still understates the accepted-address set

**Area:** documented signaling boundary.  
**Files/functions:** `README.md:232–236`; `internal/core/sip/endpoint.go:583–625`, `fromRegistrar`, `askAgain`; `docs/reviews/migrate-to-go-wails-m5/README.md:91`.

**Concrete scenario:** the name initially returns A, later returns B, and no longer returns A. After the refresh, both A and B remain accepted at the configured port. No subsequent DNS answer removes A.

Thus “an address the server’s name has” and “an address its name does not have … is not heard” remain inaccurate. The review record’s claim that the README now describes the implementation is too strong.

**Fix:** document the union of addresses returned since this endpoint was created, its retention until endpoint replacement, and the configured-port requirement. This does not require changing the intended allowlist policy.

## Checked and found supported

### `askAgain`, shared state and attacker influence

- **No identified production race on the three fields.** Readers of `peers` use `peersMu.RLock`; updates use its write lock. `asking` is atomic. `lookup` is initialized before use and is not subsequently reassigned in the production paths inspected. The account server is likewise fixed for that endpoint.
- **Lookups cannot overlap within one endpoint.** The compare-and-swap covers the lookup and a subsequent cooldown. Importantly, the deferred `AfterFunc` starts its one-second timer **after the lookup returns**. This is stricter than one lookup start per second.
- **An ordinary lookup error does not wedge the gate:** the deferred timer still resets `asking`.
- **A slow lookup holds the gate throughout its duration.** There is no application deadline or cancellation passed to `net.LookupIP`. A resolver that never returns can retain that goroutine and keep further refreshes disabled indefinitely. It does not produce an accumulating queue of lookups on that endpoint.
- **Packet contents cannot select the lookup name or supply an accepted address.** Resolution always uses `e.Account.Settings.Server`. Source addresses are compared numerically; SIP headers are not used to extend `peers`.
- An attacker able to send datagrams from an arbitrary address’s port 5060 gains, on an account configured for that port, **at most one lookup of the account’s own server name per lookup-duration-plus-one-second cycle**. The triggering packet remains refused. Wrong-port or invalid-address input does not start resolution.
- An address **never returned by resolution** is not admitted by this mechanism. An address returned previously remains admitted even after DNS stops returning it.
- **Growth is unbounded but deduplicated:** `peers` grows with unique resolved addresses, not with arbitrary packet sources. There is no TTL expiry or size limit; lookup and insertion costs grow with that retained set.
- Closing the endpoint neither cancels nor joins an outstanding refresh. It may finish and update the old endpoint’s private allowlist afterward. That does not reopen sockets or modify its replacement, but an unreturning resolver can retain the old endpoint.

A packet forged with the registrar’s own permitted address and port is outside what this non-TLS source check can distinguish. I do not count it as a filter defect.

### UDP registration timing

With the supplied UDP retry schedule and `T1 = 500 ms`, further requests normally occur around 0.5, 1.5, 3.5 and 7.5 seconds after the first transmission. `registerTimeout` is ten seconds.

A prompt refresh returning the actual responder’s address therefore permits a later response to the repeated request. It is **not guaranteed to be the second response**: resolution may take longer, return another subset, or consume time needed for authentication. The record’s unconditional “refused that once and accepted when repeated” is stronger than the implementation establishes.

Over TCP/TLS, a successfully dialed peer can indeed be absent from `peers`: dialing uses sipgo’s separate resolution. TLS certificate verification does not automatically add that peer to the endpoint’s allowlist. Finding 1 follows.

### Reordered teardown and generation checks

| Path | What remains alive during removal | Protection and bound |
|---|---|---|
| `dropAccount` | Incoming delivery, address watching and account-context work can continue until background `Close` finishes. | Generation advances first; stale incoming calls are refused, and stale registration/watcher results cannot update the actor. Cancellation is captured correctly. If `endpoint == nil`, cancellation runs immediately. Replacement network ordering remains finding 2. |
| `shutdown` | The same workers can run while `within(accountBy, ep.Close)` waits. | Generation advances before endpoint teardown. At `accountBy`—four seconds from shutdown’s start—the actor proceeds and cancels the account even if `Close` has not returned. |
| `release` | Workers can continue until its background `Close` finishes. | Generation advances and late results cannot reinstall the actor’s account or ring it. The captured cancellation runs afterward; the nil-endpoint branch cancels immediately. |

`Unregister` calls `stopRenewal` before modifying the registration request, canceling and joining the renewal loop. A renewal already underway can finish or be interrupted; the generation counter suppresses its later actor report.

The counter does **not** revoke an already transmitted registration or prevent an in-flight `Endpoint.register` from finishing its internal `e.reg` assignment. A fresh initial connection attempt has no installed `a.endpoint`; the immediate-cancellation branch still handles that case, and a late successful result is closed rather than installed.

`Close` gives unregister a five-second context. That is not a hard bound on every operation inside `Close`: joining renewal and context-insensitive transport writes are not made deadline-aware by creating that context. Consequently:

- `dropAccount` and `release` have no independent cutoff ensuring their deferred account cancellation runs if `Close` stalls.
- `shutdown` bounds its own wait independently; its closing goroutine may outlive that wait.
- **PBX silence alone still does not make shutdown wait indefinitely.** The account and device waits retain their absolute deadlines. The existing silent-PBX tests cover TCP, including shutdown during a call; they are not a dedicated test of this UDP socket-order correction.

TCP/TLS accounts have no listener for account cancellation to close. The UDP listener race therefore does not apply to them. Their cleanup still unregisters and ultimately closes the UA’s connections.

### Waiting before reading the cause

The new wait runs only inside an error cleanup path, with a canceled dialing context and a successful INVITE response. It does not add three seconds to an ordinary successful call.

Both normal `setState(Ended)` and `endWithCause` cancel the initialized dialog context immediately after publishing the state. Normal BYE completion therefore ordinarily makes this wait immediate; cancellation with a nil argument produces `context.Canceled`.

After observing `d.Context().Done()`, reading the cause is appropriately ordered. **After observing only `hctx.Done()`, it is not.** A publication stalled for the entire three seconds can still reach the nil-cause branch. The change removes the ordinary narrow window, but the documents should not imply unconditional publication synchronization. No deterministic test exercises this window.

### Documents and recorded runs

I found no changed passage clearly presenting a failed or explicitly unrepeated run as a pass. The 16 failed lab scenarios, six failed walkthrough subtests and one-off unit-test failure remain disclosed, separately from the later claimed passes.

The included report says 79 scenarios and zero failures; the final walkthrough log ends with 30 passing subtests. Those artifacts do not independently establish their commit identity. The record explicitly attributes the successful teardown runs to `fe82f91`, while pinning was still present; that is not evidence for the later DNS-refresh replacement.

The new resolver test establishes helper-level rejection, one successful mocked refresh, retention of the original address and rejection of an address never returned. It does not establish transaction recovery, stream handling, timeout recovery or post-close cancellation.

**The socket-replacement explanation is supported as a mechanism by the supplied library code:**

1. `server.go` closes the UDP listener when its context ends.
2. Transport lookup resolves the destination before consulting the connection pool.
3. Pool lookup can return an entry without checking whether its socket has already closed.
4. Reader cleanup removes that entry asynchronously.
5. Once the entry is absent, UDP connection creation can bind a replacement socket at the requested local address.

Thus both “reuse a closed, still-listed listener and fail” and “find it removed and open another socket” have source support. The additional claim that removing DNS delay selected the failing interleaving in those particular runs is a **timing inference**, not something the supplied source or summaries prove for every failure. It should not be read as deterministic behavior.

### Publication scan

I found no apparent operational SIP account, private PBX hostname, subscriber credential or personal home path. The extract contains lab accounts, `.invalid` names, test/private-network addresses, plausible telephone-shaped fixture values, generic home paths, and public project/upstream author identifiers and licence emails. The phone-shaped fixtures should not be described as independently verified unassigned numbers.

**Not checked:** no builds, tests, labs, calls or devices were run; omitted artifacts and missing parser/client-transaction implementations were not independently verified.  
**This review is not evidence that a call works.**