UNSOUND

## Status of round 10’s findings

| # | Status | Assessment |
|---|---|---|
| 1 | **Resolved** | TCP/TLS response bytes are no longer discarded by the source filter. This fixes the reported registration/framing defect, but disabling request admission too introduces finding 1 below. No controlled DNS-change test exercises the repair. |
| 2 | **Resolved in part** | Replacement of an installed endpoint now waits boundedly for its `Close`. Wildcard removal is gone. In-flight registrations remain outside that ordering, as the record acknowledges; the retained Contact also has the exception in finding 2. |
| 3 | **Resolved** | The README accurately describes the UDP allowlist as the union of addresses returned during this endpoint’s lifetime, at the configured port. |

## New findings

### 1. Major — dialing a dialog-selected TCP peer now authorizes its incoming requests

**Area:** signaling admission.  
**Files/functions:** `internal/core/sip/endpoint.go:267–280,567–590`, `NewEndpoint`, `registrarOnly`; `third_party/diago/dialog_client_session.go:417–457`, `Ack`; `reference/sipgo-v1.7.0/dialog_client.go:96–166`, `buildReq`; `third_party/diago/dialog_session.go:67–106`, `dialogHandleReferNotify`.

An outbound connection is not necessarily a connection to the configured registrar. Dialog Contact and Record-Route information can select another host or port. The library starts a SIP reader on that connection, and the changed middleware admits every request arriving through it.

**Concrete scenario:**

1. A TCP account registers at A.
2. A proxy forwards an outgoing call’s successful response with Contact B, without a Record-Route keeping A in the path.
3. The softphone’s ACK opens a TCP connection to B.
4. B sends a matching in-dialog NOTIFY containing `Content-Type: message/sipfrag` and `SIP/2.0 200 OK`, although the user requested no transfer.
5. With no transfer callback installed, `dialogHandleReferNotify` calls `Hangup`. B can answer the resulting BYE and end the local call.

Before round 7, the handler’s source check would have refused that NOTIFY from B. It now reaches the handler. This requires a contacted network peer, not a hostile local process, administrator or forged registrar source address.

The connection is also not bound to one dialog for admission: a peer knowing another live dialog’s identifiers can submit requests for that dialog.

**Fix:** separate acceptance of stream responses from authorization of incoming requests. Preserve the registrar-only request boundary while accommodating the registrar’s actual DNS-selected connection; do not automatically authorize every peer selected by dialog routing. Add a TCP test with a different Contact destination and an unsolicited matching NOTIFY.

The README heading “Signaling from the PBX alone” and the record’s “there is no other source … to refuse” justification need this qualification.

### 2. Minor — unregister can retain a Contact that was changed after the successful request

**Area:** binding removal.  
**File/functions:** `third_party/diago/register_transaction.go:108–189,265–274`, `register`, `Unregister`.

`register` changes `Origin`’s Contact using the first response’s Via `received`/`rport` **before** deciding whether an authenticated retry is needed.

**Concrete scenario:** an IP-authorized registrar accepts the first REGISTER without a 401. It stores Contact C1 and returns 200 with NAT-derived address C2 in Via. The library replaces the retained Contact with C2 but sends no further registration. Unregister subsequently removes C2, not C1. A registrar honoring the supplied Contact can leave C1 registered; with one contact allowed, the next save can then be refused.

The old wildcard removal did not depend on this equality. The changed targeted removal does. I rate this minor because it concerns the narrower unchallenged/NAT-rewritten registration path, not the ordinary challenged lab setup.

**Fix:** preserve the Contact actually sent successfully. Apply the response-derived Contact when it will be sent in the authentication retry, rather than changing the retained successful request after an unchallenged 200. Test this with a registrar that stores and compares Contacts.

### 3. Minor — the new test proves request-handling order, not binding removal

**Area:** regression evidence.  
**File/functions:** `internal/core/engine/challenge_test.go:86–103,157–180`, `challenger.serve`, `TestAnAccountSavedAgainRegistersAfterTheOldBindingIsRemoved`.

The fixture has no binding table. It accepts every correctly authenticated registration and records a removal after sleeping. The assertion checks only that the removal contains `1001@` and no `*`.

**Concrete counterexample, by inspection:** an unregister naming the wrong port, such as `<sip:1001@127.0.0.1:9;transport=tcp>`, would satisfy these assertions. A real registrar could retain the original binding and reject the replacement, while this test reports success.

**Fix:** record the registered Contact, remove only its matching binding, enforce the one-contact limit, and assert the final binding belongs to the replacement. Keep the delay: the existing order assertion usefully detects the original concurrent replacement.

## Checked and found supported

### TCP/TLS receive paths and their limits

In the production paths inspected:

- `Endpoint.Serve` calls only `HandleFunc` for TCP/TLS. Neither that nor constructing the UA/server starts a listener.
- REGISTER uses the account transport. Outgoing INVITE sets it explicitly.
- In-dialog request construction preserves the original INVITE transport, on both client and server dialogs. Contact/Route can change the **destination**, but their transport parameter does not override that explicit transport. The server counterpart is also present in the [pinned upstream implementation](https://raw.githubusercontent.com/emiago/sipgo/v1.7.0/dialog_server.go).
- The size-based transport switch is commented out. Oversized UDP writing returns an error; it does not silently switch transports.
- I found no production path here that creates a UDP **SIP** socket for a TCP/TLS account. `bindHost` briefly opens and closes a UDP socket for route selection without sending; call-media UDP sockets are separate.
- TCP/TLS reads therefore come from peers of connections the library opened—not arbitrary senders able to reach a listening port. **Those peers need not all be the registrar**, which is finding 1.
- TLS retains its separate protection: connections use the configured roots and the configured registrar’s fixed `ServerName`. An unrelated Contact host cannot simply present its own valid certificate. The demonstrated TCP scenario is not an arbitrary-host TLS bypass.

Once requests reach these connections:

| Request | Effect |
|---|---|
| New INVITE | Can reach incoming delivery and ring an idle, non-DND phone. Answer still requires the user; this does not demonstrate automatic answering or microphone transmission. |
| BYE | A matching dialog is ended. The cache matches dialog identifiers, not ownership of the receiving connection. |
| REFER | Dialog matching is required, and this application leaves `OnRefer` unset. Both dialog implementations refuse it with 406; it does not automatically place another call. |
| NOTIFY | Dialog matching and the basic sipfrag checks apply. There is no requirement for an outstanding REFER before the nil-callback hangup path. During a transfer, the callback can instead influence its reported result. Out-of-dialog NOTIFY does not match and cannot do this. |

The handler alone already failed to protect matching CANCELs before round 7 because transaction matching happens first. I do **not** count that as a newly weakened handler behavior. INVITE, BYE and NOTIFY admission is concretely weaker now.

A datagram forged with the registrar’s permitted address and port remains outside what the non-TLS source check can distinguish; it is not a finding.

### Ordered replacement

| Path | Ordering found |
|---|---|
| Saving an account with an installed endpoint | `dropAccount` publishes `leaving` before `connect` captures it. The new endpoint waits for closure, six seconds, or cancellation. |
| Retry after failed initial registration | The failed candidate is closed before its failure is posted and a retry scheduled. It is not installed in `a.endpoint`. |
| Retry after failed renewal, without a call | The installed endpoint is dropped; the replacement follows the same wait. |
| Lost address, without a call | Uses the registration-failure/retry path above. |
| `reregister` while a call retains the endpoint | Reuses that endpoint. It creates no replacement and does not use `leaving`. |

**Visible timing:** “Registering” is published before waiting. With an old PBX that reads but never answers unregister, ordinary removal consumes approximately five seconds before the new registration starts. Its ten-second registration timeout starts afterward. Thus the visible attempt can take roughly fifteen seconds, or sixteen if the six-second wait wins, plus endpoint construction/DNS time—not ten seconds overall.

**Second save while waiting:** because no replacement endpoint is installed yet, the second `dropAccount` cancels that attempt without replacing `leaving`. The next attempt still captures the older endpoint’s channel. Each closing goroutine captures its own `gone` and closes it once; I found no double-close path.

**Limits of the channel:** it can remain unclosed if `Close` stalls. The six-second timeout bounds the new waiter, not the old cleanup. After proceeding on timeout, a later installed endpoint can supply a newer `leaving` channel; this is not a join of every outstanding cleanup. Documentation should say “stops waiting,” not imply the old removal necessarily stopped.

**Late initial registration:** an uninstalled candidate is not represented by `leaving`. Its stale successful result is closed asynchronously at `connect`’s posted function, without ordering that removal against the successor. A remotely accepted request whose local attempt returns an error may also lack an installed `e.reg` to remove. The record correctly acknowledges the in-flight gap; I have not counted that disclosed limitation again.

With targeted removal, **same user and host but another port is not the same Contact** under RFC/PJSIP matching. Consequently, that late removal no longer automatically erases a distinct successor binding. It can still leave an older binding blocking registration; identical complete Contacts, including port reuse, are not protected by the generation counter.

**Shutdown and goroutines:** shutdown advances the generation, bounds its own installed-endpoint wait, and cancels a replacement waiting in `connect`. It does not join an earlier `leaving` cleanup. The new select itself has an exit and does not leak merely because the PBX is silent. Overall leak-freedom is not established: initial `net.LookupIP` has no account cancellation, and synchronous transport writes/renewal joining are not made deadline-aware by this select. Those existing limitations remain.

### Removal Contact and header construction

For the usual challenged registration:

- The connection-derived Contact is populated before the first send.
- The first response’s NAT rewrite is used in the authenticated retry.
- Digest authentication reuses that request.
- Renewal’s `doRequest` reuses `Origin` without subsequently changing its Contact.
- `stopRenewal` cancels and joins renewal before unregister mutates the request.

Thus the retained Contact normally matches the last registration sent. It is **not unconditional**, because of finding 2. Renewal also does not continually reconstruct Contact from each new connection’s address.

The requested syntax is valid: with no Contact `expires` parameter, the Expires header supplies its lifetime; zero requests removal of that particular binding. This follows [RFC 3261 §§10.2–10.2.2](https://www.rfc-editor.org/rfc/rfc3261.html#section-10.2.2).

Asterisk PJSIP explicitly falls back from Contact expiry to the Expires header, preserves zero, and removes a matching Contact. This supports the syntax for Asterisk and FreePBX’s PJSIP registrar—not runtime proof against either installation. See [Asterisk 22.11.0 registrar source](https://raw.githubusercontent.com/asterisk/asterisk/22.11.0/res/res_pjsip_registrar.c).

Do not generalize that protection to every historical Asterisk driver: [legacy `chan_sip`](https://raw.githubusercontent.com/asterisk/asterisk/20/channels/chan_sip.c) handles expiry zero as unregistering the peer, rather than providing PJSIP’s multi-contact distinction.

I found no new duplicate-header-producing path: construction adds one Contact and one Expires; NAT handling replaces Contact; unregister removes Expires before adding zero; authentication does not add either. The extract omits the general header-container implementation, so this is a construction-path conclusion, not an independent audit of those primitives.

No inspected lab scenario or test PBX requires wildcard removal. The Python registration probe already sends a specific Contact with header-level expiry zero. The new challenger and shutdown PBX recognize removal from Expires, not `*`.

### DNS refresh

The five-second deadline applies to `askAgain`’s production lookup. It no longer waits indefinitely for that lookup API to return. The atomic gate still prevents overlap and resets after the lookup plus cooldown.

This does not bound the initial lookup in `NewEndpoint`, cancel refresh on endpoint close, or establish TCP/TLS DNS-change recovery by test.

### Documents and recorded evidence

The changed documents overstate two conclusions:

- An outbound connection alone does not establish “PBX alone” admission.
- “The registration’s own contact” needs the successful-wire-Contact qualification above; the new test does not establish actual deletion.

The in-flight ordering gap is disclosed accurately.

I found no changed passage presenting a failed or explicitly unrepeated full run as a pass. The receipt names the full runs as being on **`4b802ed`, before these changes**, and retains the first walkthrough failure separately from the later pass. Those runs are not evidence for this patch. The supplied latest report lists 79 scenarios and zero failures; the final walkthrough log lists 30 passing subtests, but those summaries alone do not establish commit identity.

### Publication scan

I found no apparent operational SIP credential, private PBX hostname or personal home path. The database fixture is documented as fictional, and its readable contents are consistent with that.

Telephone-shaped literals do exist in tests and mock contacts, including Brazilian-looking numbers. Their fixture context is clear; I have not verified that they are unassigned. Public upstream author names/emails and project identifiers are also present, chiefly in attribution material—not an apparent newly published private account.

**Not checked:** no builds, tests, lab scenarios, applications, VMs, devices or calls were run; artifact identity and omitted library internals were not independently established.  
**This review is not evidence that a call works.**