Paths are relative to `src/`. This is a static review of the spike, not independent confirmation of its reported passes.

## BLOCKING

1. **Plaintext-offer guard is bypassable** — `internal/g0/endpoint.go`, `offersSRTP`, `Serve` (140, 246–249).  
   The substring checks accept plaintext `m=audio ... RTP/AVP` when an unrelated attribute or video section contains ` RTP/SAVP ` and `a=crypto:`. Downstream diago acceptance is **unverified**, but the endpoint’s required 488 gate already fails. **Fix:** parse SDP and require the supported secure profile and valid SDES parameters on the selected audio section; enforce the requirement on answers and renegotiations too.

2. **Registrar restriction ignores the source port** — `internal/g0/endpoint.go`, `fromRegistrar` (230–242).  
   A request from the registrar’s IP but an unrelated port passes: configuring `PBX:5060` also permits `PBX:40000`. The foreign-INVITE probe tests another IP, not this bypass. **Fix:** validate the configured peer’s IP, port and transport, using the authenticated connection for TLS; add a same-IP/wrong-port rejection test.

3. **Audio threshold tests can miss the failures they claim to exclude** — `internal/platform/audio/{pulse_linux_test.go,wasapi_windows_test.go}`, `TestPulseLoopback`, `TestWASAPI`.  
   They count executed ticker iterations rather than elapsed 20 ms deadlines, merely log capture drops, and measure the longest **capture** miss—not the required longest playback gap. `toneBlocks` scores only received samples, so omitted intervals can disappear from its denominator. **Fix:** measure timestamped playback continuity and expected frames against elapsed time, fail on capture drops and playback gaps over 60 ms, and inject a known stall to prove those assertions fail.

## MAJOR

4. **The “refused-488” scenario does not assert 488** — `internal/g0/scenarios_controls.go`, `plainOfferRefused` (294–324).  
   Any dial error passes once `RefusedPlainOffers` increments; `Serve` increments before calling `Respond` and ignores its error. A failed response or a different rejection code can therefore pass. **Fix:** assert the received SIP status is exactly 488 and surface response failures.

5. **Decoder exclusion and stream survival remain unverified** — `internal/g0/guard_test.go`, `next`, `TestSRTCPReplayAndTamperAreDropped`; `third_party/diago/media_session.go.diff`, `ReadRTP`/`ReadRTCP`.  
   `next` implements its own empty-packet skipping instead of exercising the real RTP-session/decoder path; the SRTCP test ends after the tampered packet without proving subsequent genuine traffic succeeds. The supplied tree lacks those actual third-party consumers, so their behavior is **unverified**. **Fix:** include the patched source and test through the real receive/decoder pipeline, asserting zero malicious deliveries and continued valid RTP/RTCP processing.

6. **The report cannot close the full G0 gate** — `internal/g0/scenarios.go`, `Scenarios`; audio conformance tests; `openspec/tasks.md`, 2.8–2.11.  
   The JSON demonstrates reported short synthetic SIP/media probes, not five-minute device-backed lab calls, simultaneous five-second UI/event-consumer stalls, or same-host Rust comparisons. VM checks run a separate audio test executable, not calls through the packaged application; the relevant tasks correctly remain unchecked. **Fix:** retain “pending” status and record those measurements, artifact hashes and the owner’s decision before approving G0.

7. **Playback failures silently kill the Windows worker** — `internal/platform/audio/wasapi_windows.go`, `render` (273–325).  
   Errors after startup terminate rendering without recording or exposing a playback failure; capture can continue normally while subsequent playback frames accumulate and drop. **Fix:** publish a playback-error/lost state and test render-device invalidation after successful startup.

8. **VM success can conceal a broken shipped AppImage** — `build/vm/check.sh`, `try` (65–87).  
   Both AppImage attempts may fail while an optional standalone cgo binary succeeds, producing overall `vm-check: PASS`. That does not validate the packaged artifact. **Fix:** require the shipped AppImage to pass its declared launch configuration; keep fallback runs diagnostic only.

9. **VM private keys are eligible Docker build-context contents** — `build/go.mk`, `go-image` (32–33).  
   The context is `build/`, which contains `.vagrant/.../private_key` files and has no context-local ignore file. This contradicts task 2.1’s key-exclusion requirement. **Fix:** add an allowlisting `build/.dockerignore`, excluding VM state. Key contents were not read.

No concrete SPSC-ring race or WASAPI unsafe-slice lifetime violation was identified by inspection; `TestRing` is serial and does not establish concurrent correctness.

Not checked: builds, test execution, network, or anything outside `src/`; third-party runtime/ABI behavior remains unverified.

**Verdict: UNSOUND as evidence for a G0 go decision.**