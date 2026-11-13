## Earlier findings

1. **Problem 1 — RESOLVED for the reported delayed-callback failure.**

   In `internal/platform/audio/audio.go`, `captured`, `discardBegin` and `discardEnd` now serialize delivery and discard boundaries. The ring is cleared, and deliveries remain rejected until confirmation—not merely until 100 ms expires.

   - **Pulse:** Given the supplied synchronous read-loop behavior, an earlier capture callback cannot resume after the flush reply: it must finish before that reply is processed. Deliveries before the subsequent latency confirmation are also rejected. Afterwards, the adapter skips the configured sample count.
   - **WASAPI:** The capture thread confirms only after finishing its current packet and observing an empty device queue. An outstanding packet cannot bypass that thread’s confirmation.
   - **Engine:** The atomic privacy word and `microphone.next` cover the actor/sender orderings:
     - A sampled hold returns no microphone frame; a sampled mute returns silence.
     - A completed interval between ticks changes the generation and forces discard.
     - A transition during capture changes the second reading and rejects the frame.
     - A transition during discard leaves another word change to handle before microphone output resumes.
     - A transition **after successful post-capture validation** can still allow **one already-captured frame: 160 samples, or 20 ms of pre-transition audio**, to leave later. This bounds its content, **not its transmission delay**.

   `TestDiscardDropsEverythingCaptured` covers delivery 150 ms after discard and the subsequent counted tail. `TestPulseLoopback` checks discard against a real virtual sound service and microphone recovery.

   **Remaining limitation:** Those tests do not establish that the reported latency plus the sample-count allowance covers every physical device’s internal recording pipeline. That is explicitly disclosed; it is not the surviving arbitrary callback-delay race from round 10.

2. **Problem 2 — RESOLVED.**

   In `third_party/diago/media/rtp_jitter_buffer.go:441–461`, `resetPlayoutTimer` requires both `ready > delayPackets+1` and `ready == queued`. A queued packet beyond a gap therefore prevents acceleration of the prefix before it.

   For the reported `1,2,3`, then `4,5,6,7,9,10` case, the prefix before missing packet 8 no longer triggers catch-up. Packet 8 retains its approximately 140 ms deadline relative to packet 1’s release. The added “one further on” regression supplies it at approximately 125 ms and requires ordered delivery with no lost or late packets.

## New problems

None found in the requested scope under the stated one-account, one-call-at-a-time, one-process-per-database design. I found no additional concrete closure blocker.

## Receipt assessment

The wording matches the inspected assertions **when read with its stated limitations**:

- Privacy tests exercise the decision helper and adapter separately, not an engine-plus-real-adapter hold/mute call.
- The Pulse loopback test establishes a functioning discard/recovery path on virtual devices. Its continuous tone does not independently distinguish pre-discard from post-discard recordings.
- The physical-device tail allowance remains an assumption awaiting hardware validation, not a demonstrated universal privacy guarantee.
- The jitter regression now covers both the near and farther buffered gap.
- Simulated importer interruptions, clean-close history recovery, SQLite page-limit exhaustion, shutdown observation at event-channel closure, and untested power-loss behavior are accurately distinguished.
- The configuration/store/importer test counts match **19/24/14**.

Not checked: builds, test execution, network, omitted dependencies, fixtures or execution logs. Physical-device privacy validation and the receipt’s other recorded validation tasks remain outstanding.

Verdict on M1: SOUND