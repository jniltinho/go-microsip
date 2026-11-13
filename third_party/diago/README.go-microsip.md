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
| `dialog_media.go`, `dialog_client_session.go` | Found in M5 when early media was played (a 183 with SDP, then the answer): the answer's SDP was applied again to the session that was already receiving, which wrote it while it was read. `checkEarlyMedia` now leaves the session alone when the answer describes the same media as the 183 (`sameMedia`: the connection, media and attribute lines, as a set for each section; the origin, the session's name and its times are not compared). An answer that describes other media is still applied in place, as upstream does. |
| `media/media_session.go`, `media/gomicrosip_guard.go` | Found in M5 by the independent review's scenario and a test: one datagram on the RTP port that is not an RTP packet, from the right address, was returned as an error that the readers above kept, so the call never received again. `ReadRTP` drops and counts it (`DroppedMalformed`), as it does a packet from another address. Not done for SRTP, where the packet has passed authentication before it is parsed. |

Nothing else differs from the tag. To update: copy the new tag the same way, re-apply the marked changes, and re-run `make go-test` and `make go-lab`.
