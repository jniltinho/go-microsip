UNSOUND

## Round 6’s finding

**Resolved by inspection for the reported key-selection and direction cases.** `sameMedia` now preserves crypto-line order and compares each section’s last direction. `TestSameMedia` exercises reversed keys and conflicting directions. Independent attributes can still move or repeat.

The test named “no keys” changes `RTP/SAVP` to `RTP/AVP`; it does **not** remove the crypto lines. Key removal is handled correctly by the code, but that fixture does not establish it.

## New findings

### 1. Major — matching CANCELs bypass the registrar-only filter

**Area:** incoming signaling security.  
**Files/functions:** `internal/core/sip/endpoint.go:503–521`, `registrarOnly`; pinned sipgo’s `sip/transaction_layer.go:154–183`, `handleRequest`, and `sip/transaction.go`, `makeServerTxKey`.

The filter checks the INVITE’s source, not the matching CANCEL’s source. Sipgo matches and processes CANCEL **before** invoking the server middleware. Its transaction key uses supplied Via information, not the packet’s actual source.

**Concrete scenario:** a network observer learns the branch and sent-by values of a ringing UDP INVITE. It sends a matching CANCEL from a different address or port. The library cancels the ringing call without reaching `fromRegistrar`. Adding `Reason: SIP;cause=200` also makes the new callback classify it as answered elsewhere and suppress the missed-call notification.

This requires knowledge of the live transaction; it is not an arbitrary off-path branch-guessing attack. The cancellation bypass is inherited, but it is in the path specifically requested for this review.

**Fix:** enforce the source restriction before transaction matching. Checking the source inside `OnCancel` would protect the classification, but would be too late to prevent cancellation. Add a foreign-source CANCEL test against an already-ringing legitimate INVITE.

### 2. Minor — quoted Reason text can manufacture a SIP cause

**Area:** answered-elsewhere classification.  
**File/function:** `internal/core/sip/endpoint.go:139–154`, `completedElsewhere`.

Splitting indiscriminately on commas and semicolons treats quoted text as header structure. RFC 3326 permits quoted text parameters. [RFC 3326, section 2](https://www.rfc-editor.org/rfc/rfc3326.html#section-2)

**Concrete scenario:**

```text
Reason: SIP;cause=487;text="caller canceled;cause=200;diagnostic"
```

The parser finds the `cause=200` inside `text` and reports answered elsewhere. Likewise, quoted text inside a Q.850 value can introduce a fictitious comma-separated `SIP` value. A genuinely missed call then loses its notification and is recorded as answered.

**Fix:** split values and parameters only outside quoted strings, respecting escapes. Add both quoted-delimiter negative cases. Ordinary multiple values, spacing, case, quoted `"200"` and a standalone `Q.850;cause=200` are otherwise handled as intended.

### 3. Minor — cancellation can repeat cleanup of an already-ended dialog

**Area:** crossing-answer cleanup.  
**File/function:** `internal/core/sip/endpoint.go:453–464`, `DialEarly`; `third_party/diago/dialog_client_session.go:379–385`, `waitAnswer`.

The condition establishes that the context expired, not that cancellation caused this particular error.

**Concrete scenario:** the library rejects an incompatible SDP, successfully sends ACK and BYE, and then cancellation or the deadline becomes visible before `DialEarly` checks `ctx.Err()`. The new branch sends another ACK. Sipgo’s `WriteAck` changes the ended dialog back to confirmed, permitting another BYE. A resulting 481 need not restore its ended state, leaving a stale dialog-cache entry.

The remote call has already ended; this is a minor simultaneous-event defect, not the original stranded-call failure.

**Fix:** skip already-ended dialogs and avoid another ACK when already confirmed. Preserve ACK-before-BYE for cancellation paths that have not reached confirmed state. Add a canceled-context codec-rejection case.

### 4. Minor — SDP equality still hides an order-sensitive connection selection

**Area:** early-media comparison.  
**Files/functions:** `third_party/diago/dialog_media.go:1036–1047`, `sameMedia`; `third_party/diago/media/sdp/sdp.go:39–44,124–125`, `Value` and `ConnectionInformation`.

Connection lines remain an unordered set, while the consuming parser selects the **first** connection value.

**Concrete scenario:** an accepted, ambiguous SDP contains two different `c=` lines in one section, A then B. The final answer reverses them and the PBX uses B. `sameMedia` declares equality and retains A, whereas parsing that answer would select B. Audio can consequently disappear.

Like round 6’s multiple-key case, this needs noncanonical input that the implementation nevertheless accepts.

**Fix:** preserve connection-line order, or reject conflicting connection values before using the equality shortcut. Add a reversed-connection test.

### 5. Minor — a nonempty unusable CA bundle remains a retried network failure

**Area:** TLS failure classification.  
**File/functions:** `internal/core/sip/endpoint.go:58–65,592–621`, `TLSConfig`, `RegistrationFailure`.

The sentinel fixes the missing-bundle case only. A readable file containing no PEM certificate produces an ordinary error that falls through to `Network`.

**Concrete scenario:** `ca_file` names a readable text file containing “not a certificate”. The window reports a network error and retries even though retrying cannot repair the bundle.

**Fix:** classify this TLS-configuration error as a certificate failure too, and extend the classification test to the existing invalid-PEM fixture.

The receipt’s historical “retried for ever” wording is also inaccurate: the actor permits eight retries, not unlimited retries.

### 6. Minor — answered-elsewhere markers can outlive their calls indefinitely

**Area:** incoming-call bookkeeping.  
**File/functions:** `internal/core/sip/endpoint.go`, `elsewhere`, `registrarOnly`, `AnsweredElsewhere`; `internal/core/engine/actor.go`, `ring`, `watch`.

Only a ringing call’s watcher removes a marker.

**Concrete scenario:** the CANCEL callback records answered elsewhere while an incoming dialog is awaiting actor handling. The actor subsequently refuses it because DND or another call is active. No ringing-call watcher consumes that Call-ID. Cancellation before the library finishes constructing its dialog provides another unconsumed path.

Those entries remain for the endpoint’s lifetime. For one desktop user this is small memory retention, not a retained call, device or goroutine.

**Fix:** give markers call-owned lifetime, or provide bounded cleanup for dialogs that never reach the consuming watcher. Do not simply delete on transaction termination before that watcher can read them.

### 7. Minor — A126’s test does not establish its claimed observation after 202

**Area:** parity evidence.  
**Files/functions:** `internal/core/app/parity_test.go:36–65`, `TestATransferIsNotASuccessOn202Alone`; `internal/core/app/run_test.go:116–118`; `docs/migration/parity-matrix.md`, A126.

`TransferPending` is published before the REFER goroutine runs. The two-second observation starts from that publication, with no confirmation that the PBX received REFER or sent 202.

**Concrete scenario:** the PBX receives REFER but sends nothing—or processing stalls beyond two seconds. The test passes while the five-second request timeout has not expired. No 202 was established.

**Fix:** synchronize the observation with the fixture’s receipt of REFER and successful 202 response. Keep the documented two-second scope; this finding does not require an unnecessarily long timeout test.

A126’s “watched for two seconds after the 202” currently claims more than its assertion establishes. This is a test-precision finding, not a demonstrated transfer implementation failure.

## Checked and found supported

### Crossing-answer repair

- The success-response/context condition covers both sipgo cancellation returns: a final 2xx received while awaiting the canceled INVITE, and the early “non provisional response received during CANCEL” return.
- `ctx.Err() != nil` correctly includes a deadline, not just explicit cancellation. A failed authentication retry leaves a non-success challenge response and does not trigger this cleanup.
- The cancellation paths may not have formed sipgo’s local established state. That does **not** prevent cleanup: successful `WriteAck` sets confirmed state, which permits `WriteBye`. Requiring an already-established state would wrongly exclude these paths. This ACK/BYE sequence matches the protocol requirement. [RFC 3261, section 15](https://www.rfc-editor.org/rfc/rfc3261.html#section-15)
- Ordinary codec rejection without simultaneous cancellation does not enter the new branch.
- Handed-over early media is still closed through `d.Close`’s registered media callback. Actor cleanup subsequently stops and joins its pumps and releases devices; media closure is idempotent.
- Actor completion handles error, success while terminating, and completion after finalization separately. The final-state guard prevents duplicate final snapshots. A user-canceled attempt ends as canceled; an unanswered deadline ends as no answer.
- **Three seconds is not a hard bound on `Dial`.** The preceding cancellation exchange uses background waits; ACK’s transport write does not honor that context. Connection establishment, writing and cleanup can therefore exceed it.
- The crossing fixture genuinely provokes the old missing-ACK/BYE path. Its assertions establish method presence, canceled local outcome and no call audio stream—not exact ACK/BYE counts, strict ordering, or remote dialog validation.

### Answered elsewhere

The new cancellation callback is registered before the library’s callback. Sipgo chains callbacks in registration order, so the marker is normally stored before the library wakes the watcher. A transaction already canceled or terminated can refuse registration; the ignored return does not replay a lost CANCEL.

For the normal path, history becomes answered with no answer timestamp or duration, the missed notification is suppressed, and the ringing notification closes. The page clears the finished call and refreshes history.

The permanent history shows **“Incoming · Answered”**, not “answered elsewhere”; the new `endReasonText` does not change that because final snapshots are cleared. This loses the distinction, but it is the owner’s explicit, documented schema-preserving choice—not a claim that this softphone answered.

### Telephone-event fallback

The nil keys writer is not presently reachable through a queued key: each call starts with an empty channel, and the actor is its sender and checks `keyed`. Audio startup uses the same path for incoming calls.

Hold/resume retains the underlying RTP writer, whose session is updated by the library. Privacy gating and capture discard are independent of the DTMF wrapper.

The new test establishes opened, still-open devices and key refusal. It does **not** measure bidirectional audio or absence of telephone-event packets at the PBX.

### TLS and application tests

Missing bundles now reach the page as `certificate`, and certificate failures stop automatic retries. Network and timeout failures remain retryable. The README accurately describes the current explicit-bundle requirement; restoring system roots remains undecided.

`TestADotEnvInTheWorkingDirectoryIsNotRead` has a useful positive control: explicitly naming the same file configures the account.

The interrupted-history test requires recovery to make the copied open row visible. It does not kill a process, and the matrix candidly says so. The quit-history, contact persistence and startup-retention assertions support their stated narrower claims.

`engineEarlyMedia` requires substantial received PBX-tone audio, closed ringback streams and no answer timestamp. It does not inspect outgoing silence, and its comment that the microphone is not opened is wrong: it opens duplex sound. It also accepts any final reason, so it does not independently establish that the PBX caused the ending.

`engineAnsweredElsewhere` establishes both phones ringing, one answering, the other ending with the new reason, and the caller reaching established. It does not itself establish history, notification behavior or successful cleanup; those are separate checks.

### Matrix, receipts and privacy

- **A79/A80:** the store, page and built-window checks support preview, rejection details, duplicate handling, confirmation and atomic import.
- **A105:** the documented extraction modes and recorded non-FUSE runs support the stated claim.
- **A129:** the built-window certificate-failure claim has named passing evidence. The seven-second observation checks the first retry interval, not indefinite behavior by observation alone.
- **A126:** qualification needed as finding 7 explains.

The sign-off document is clearly a proposal. Neither it nor the receipt presents the undecided system-root change as accepted, or the gate as passed.

**The receipt is candid about the night’s failures.** It distinguishes failed runs from later passes, preserves the crossing failure, reports the Windows audio failure, and explicitly says heavy load does not prove every failure environmental. It requires quiet-machine reruns; it does not present those unperformed reruns as passes.

I found no apparent private operational account, production PBX, real dialed number, credential or personal home path. Public project/author identifiers and upstream licence emails are present; the operational examples appear fictional or lab-scoped.

**Not checked:** nothing was built or run; artifacts, real hardware, private calls and quiet-machine reruns were not independently verified. The original baseline delta specs are absent from this extract.  
This review is not evidence that a call works.