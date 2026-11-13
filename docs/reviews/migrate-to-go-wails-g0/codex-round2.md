Paths below are relative to `src/`; conclusions are from static inspection.

## Round-1 findings

1. **PARTIALLY RESOLVED** — `internal/g0/endpoint.go:offersSRTP` now isolates the first audio section and `Dial` checks answers, but key validation remains superficial: `endpoint_test.go:TestOffersSRTP` explicitly accepts `inline:QUJD`, which decodes to only three bytes; renegotiation enforcement is **unverified**.

2. **RESOLVED** — `internal/g0/endpoint.go:fromRegistrar` checks both the parsed IP and configured port, with same-IP/wrong-port rejection covered by `endpoint_test.go:TestFromRegistrar`.

3. **PARTIALLY RESOLVED** — `internal/platform/audio/helpers_test.go:verdict` uses elapsed time and checks drops in both directions, but its new gap threshold, observation timestamps and unconditional slack still permit false passes.

4. **PARTIALLY RESOLVED** — `internal/g0/endpoint.go:Serve` counts only successful responses, but `scenarios_controls.go:plainOfferRefused` searches error text for `" 488 "` rather than asserting the received response’s numeric status.

5. **PARTIALLY RESOLVED** — `internal/g0/guard_test.go:receiver` now uses the real receive APIs and `TestSRTCPReplayAndTamperAreDropped` checks subsequent genuine traffic, but the omitted third-party consumer implementations remain **unverified**.

6. **NOT RESOLVED — intentionally pending** — `openspec/changes/migrate-to-go-wails/tasks.md` correctly leaves 2.1, 2.8, 2.10 and 2.11 unchecked; `internal/g0/scenarios.go:Scenarios` does not replace those measurements or the owner’s decision.

7. **PARTIALLY RESOLVED** — `internal/platform/audio/wasapi_windows.go:render` now publishes `playLost`, but `wasapi_windows_test.go:TestWASAPI` merely logs, rather than asserts, `PlaybackLost()` after device invalidation.

8. **RESOLVED** — `build/vm/check.sh:try` and its surrounding gate require an AppImage self-test to succeed in the desktop session; standalone cgo success cannot satisfy the gate.

9. **RESOLVED** — `build/.dockerignore` excludes everything from the context used by `build/go.mk:go-image`, including VM state and keys.

## New major problems in the revised checks

- **MAJOR — `internal/platform/audio/helpers_test.go:heard`, `verdict`, `maxGap`: the playback-gap check still admits prohibited gaps.**  
  `maxGap` is 100 ms, so an 80 ms interruption can pass while remaining below the overall loss allowance. Additionally, every buffered block is timestamped when drained, not when played: good blocks surrounding silence in one batch receive effectively identical timestamps. **Fix:** measure consecutive missing/bad sample duration and timestamp delivery at the observation boundary; add controls proving 60 ms passes and 80 ms fails, including batched delivery.

- **MAJOR — `internal/platform/audio/helpers_test.go:captureFrame`, `verdict`: slack and aggregate counting weaken the stated 0.5% limit.**  
  At 500 expected frames, the capture check allows four missing frames (0.8%) and the speaker count allows seven (1.4%); the “edge” allowances are not restricted to edges. Late frames subsequently delivered in bursts also count as successful. **Fix:** explicitly exclude measured pipeline boundaries, then score each expected deadline without unconditional extra allowances; add boundary tests for loss and delayed/burst delivery.

## Focused checks

No consumer-skip race was identified in `audio.go:Capture` under its single-reader contract: only the consumer advances `tail`, discarding slots it will not read while retaining samples outside the producer’s free region.

`Dial` callers start their audio senders only after successful return, so no application-side plaintext-send window is demonstrated; media activity inside third-party `Invite`, including early media, is **unverified**, and the “before any audio was exchanged” comment is therefore not established.

Not run: builds, tests or network access; no files changed, and third-party runtime guarantees remain unverified.  
**Verdict on spike-code evidence: SOUND WITH CHANGES.**