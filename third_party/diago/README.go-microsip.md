# Vendored diago (go-microsip)

This folder is `github.com/emiago/diago` at tag `v0.42.0` (MPL-2.0, see `LICENSE.txt`), copied without its `examples/`, `icons/`, `testdata/`, CI files and `*_test.go` files (the upstream tests need the folders that were left out). `go.mod` replaces the upstream module with it. The patched behavior is tested from this project, in `internal/g0`.

## Changes to upstream

The G0 spike of `openspec/changes/migrate-to-go-wails` found three gaps between diago's media layer and the softphone's security rules. They are closed here with the smallest change that does it:

| File | Change |
| --- | --- |
| `media/gomicrosip_guard.go` | New file: `StrictRemoteAddr`, `SRTPReplayWindow`, the drop counters and their helpers. Off by default, so upstream behavior is unchanged unless they are set. |
| `media/media_session.go` | SDES receive contexts are created with `srtpReceiveOptions()` (replay protection). `ReadRTP` and `ReadRTCP` drop a packet from another address, and with `StrictRemoteAddr` drop and count a packet that fails authentication instead of returning an error. Each change is marked `go-microsip:`. |

| `media/rtp_jitter_buffer.go` | Found in M1 by the engine's hold scenario: after a silence longer than the buffer's window (a held call, a peer that stops sending) the playout pointer had run ahead and every later packet was dropped as late, so the call stayed silent. After a whole window of consecutive losses the next packet restarts the stream. Marked `go-microsip:`; tested in `internal/core/media/jitter_test.go`. |
| `media/rtp_jitter_buffer.go` | Found in M1 by the call through the binary with real sound (`make go-call`): playout was scheduled one packet time after the previous release, so timer latency and a source running slightly fast accumulated until the window overflowed, and the call lost 240 ms of audio at a time (17 % in the lab). Playout follows a timetable, and catches up when more packets wait in sequence than the delay asks for and nothing is queued behind a gap, so a missing packet keeps its time to arrive. Marked `go-microsip:`; pinned by `TestJitterBufferKeepsUpWithAFastSource` and `TestJitterBufferWaitsForALatePacketBehindABurst`. |

Nothing else differs from the tag. To update: copy the new tag the same way, re-apply the marked changes, and re-run `make go-test` and `make go-lab`.
