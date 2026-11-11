# Vendored diago (go-microsip)

This folder is `github.com/emiago/diago` at tag `v0.42.0` (MPL-2.0, see `LICENSE.txt`), copied without its `examples/`, `icons/`, `testdata/`, CI files and `*_test.go` files (the upstream tests need the folders that were left out). `go.mod` replaces the upstream module with it. The patched behavior is tested from this project, in `internal/g0`.

## Changes to upstream

The G0 spike of `openspec/changes/migrate-to-go-wails` found three gaps between diago's media layer and the softphone's security rules. They are closed here with the smallest change that does it:

| File | Change |
| --- | --- |
| `media/gomicrosip_guard.go` | New file: `StrictRemoteAddr`, `SRTPReplayWindow`, the drop counters and their helpers. Off by default, so upstream behavior is unchanged unless they are set. |
| `media/media_session.go` | SDES receive contexts are created with `srtpReceiveOptions()` (replay protection). `ReadRTP` and `ReadRTCP` drop a packet from another address, and with `StrictRemoteAddr` drop and count a packet that fails authentication instead of returning an error. Each change is marked `go-microsip:`. |

Nothing else differs from the tag. To update: copy the new tag the same way, re-apply the marked changes, and re-run `go test ./internal/g0/...`.
