# macOS: the first run in a desktop session (2026-11-13)

Change `add-macos-universal`, tasks 1.3 and 4.6, and measurements toward 2.3 and 5.3 (both stay open). Commit `ae72ad5`, built in the image `go-microsip-build:13`. The Mac is the owner's macOS 15.8.1 VM, x86_64. The owner logged in at its console; every command of this run went over SSH with a key, and nothing was typed at the console by the run. The screen was read through the VM's own screen capture.

The file checked:

```
552b255a57b7aa7e24f543d45c70c384a366ebf9e5766cbefe95758b76937a05  go-microsip-0.2.0-rc.1-macos-universal.zip
```

## The check target

`make go-mac`, on that file:

```
mac: macOS 15.8.1, x86_64
pass unpack
pass version: go-microsip 0.2.0-rc.1 (ae72ad5c9cc1)
pass headless (version, doctor, MCP handshake, password-argument refusal, configuration)
pass self-test (the window opens and the page calls the backend)
mac: all checks passed
```

## The command line and the MCP server against a running window (4.6)

Started normally from the bundle's executable (not as a self-test), the window stays running in the desktop session: the screen shows it with its title, the application's menu, its menu-bar item and its Dock icon. Its only log line is the warning that this platform has no navigation guard.

`go-microsip status`, exit 0:

```
{ "call": null, "dnd": false, "epoch": 1, "mode": "gui", "registration": "disabled" }
```

`go-microsip call dial 600`, with nothing approved beforehand, exits 3:

```
waiting for approval in the go-microsip window
operation: op-1-…
```

`operation get` then says `awaiting_approval`, not final. The window shows "Agent request: A local program (process …) asks to: call 600", a countdown from a minute, and Deny, Allow, and Deny all and pause. A target that is not a number or a SIP user (`600@pbx.invalid`) is refused at once with exit 2 and reaches no question.

What became of three such requests:

- The third was left alone for its whole minute: `awaiting_approval` from the second second to the sixtieth, then `expired`, final, with the outcome's reason `expired`.
- The first two ended as `succeeded` with `sent: true`, between 25 and 45 seconds after the request for the one that was timed. The run approved nothing and sent no key or click to the Mac. The likeliest reading is that somebody at the screen pressed Allow: the owner was at the VM at the time, and the third request shows that one left alone expires. It is the owner's to confirm; the code path was not traced in this run. With no account configured no call followed: `status` stayed `"call": null`.

The MCP server, `go-microsip mcp`, with `initialize`, the initialized notification and one `tools/call` of `status_get`:

```
{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"logging":{},"tools":{"listChanged":true}},"protocolVersion":"2025-06-18","serverInfo":{"name":"go-microsip","version":"0.2.0-rc.1"}}}
{"jsonrpc":"2.0","id":2,"result":{…"structuredContent":{"data":{"call":null,"dnd":false,"epoch":1,"mode":"gui","registration":"disabled"},"error":null,"operation_id":null,"request_id":"mcp-…","schema_version":1,"status":"ok"}}}
```

After a termination signal the window ended within half a second, and `status` answered `go-microsip is not running; nothing was started (engine_unavailable)` with exit 4.

The ending recorded in `macos-ssh-2026-11-13.md` (a normal start that ends at once with status 0) did not happen in the desktop session. It was seen only with nobody logged in, and its cause is still not established.

## One instance (5.3, in part)

A second start while the first window runs exits 0 within its time limit and prints nothing; the first keeps running and is the only process of the program. Whether the first window comes to the front was not observed: it already was in front. A second user of the Mac was not tried: the Mac has one usable account.

## Sound: the output queue from Go (2.3, not done)

In the desktop session the system's own player works (`afplay` exits 0; over SSH with nobody logged in it could not start a queue). The probe `coreaudio_probe_darwin_test.go`, cross-built, with no cgo:

```
1 devices: 0 with an input, 1 with an output
quiet, 2 s: 99 callbacks, 891 allocations in the whole process, longest wait 21.540289ms, slowest callback 75.22µs
busy, 5 s, 664 forced collections: 250 callbacks, longest wait 27.801045ms, slowest callback 4.216864ms
--- PASS: TestProbeCoreAudioFromGoWithoutCgo
```

So an 8 kHz mono output queue starts, and its callback, made on AudioToolbox's thread, enters Go through `purego` every 20 ms, with the collector forced 664 times in five seconds as well. This is one criterion of 2.3 measured for output, on a virtual sound device, for seven seconds. Capture, the discard cutover, the five-minute threshold, the delays and every other criterion of D4 are not measured.

## What `doctor` says in the session

```
window backend: webkit
display: false
audio: unavailable (no audio service is reachable)
```

`audio: unavailable` is expected until task 3.2 replaces the fallback device list. `display: false` in a desktop session is a finding: the report reads the display variables of Linux, which macOS does not have. It is not covered by a task yet.

## What the VM gets for the checks (1.3)

The owner's answers, each as given or done on this day:

- **A capture device:** a USB headset of the host, chosen by the owner, handed to the running VM. The owner granted their own user access to the device on the host; the run changed no permission. It lasts until the VM is shut down or the headset is unplugged, and the host has no use of the headset meanwhile: the VM tool has no setting for it on a macOS VM.
- **A desktop session:** a login by the owner before a run. No password is typed or stored by a check.
- **The macOS 26 VM:** not installed, and not used.

How the headset reached macOS: handed over with the controller's last free port in use, the emulator put it behind an emulated hub, and macOS never listed it there. With the hub removed and the headset on the controller's port itself, macOS lists it at once:

```
EKSA PC Adapter: Default Output Device, Output Channels: 2, Current SampleRate: 48000, Transport: USB
EKSA PC Adapter: Default Input Device, Input Channels: 1, Current SampleRate: 16000, Transport: USB
```

On it, the system's player and its speech exit 0, with the owner listening on the headset. The probe again, now with a real device as the default output:

```
3 devices: 1 with an input, 2 with an output
quiet, 2 s: 99 callbacks, 893 allocations in the whole process, longest wait 21.472369ms, slowest callback 70.341µs
busy, 5 s, 644 forced collections: 250 callbacks, longest wait 31.954109ms, slowest callback 13.873983ms
--- PASS: TestProbeCoreAudioFromGoWithoutCgo
```

The input runs at 16 kHz and the output at 48 kHz: the pair of different rates in and out that 2.3 asks for is at hand.

## Capture from Go, and who is asked for the microphone (2.3, not done)

A second probe in the same file, run only when asked for by a variable, records four seconds from the default input through an 8 kHz mono input queue and plays them back. At the working-tree state that follows `2ad1fcf`:

- **Started over SSH:** macOS asks nobody. The queue starts and calls back, 208 times with 320 bytes each, 32000 samples, and every sample is zero.
- **Started in the Mac's Terminal:** macOS asks "Terminal would like to access the microphone". While the question is open the start of the queue does not return: the probe sat in `AudioQueueStart` until its own one-minute limit ended it, and no callback came. The owner then pressed Allow.
- **With Terminal allowed,** the system's input volume at 16 of 100: 32000 samples with a peak of 1. With the input volume raised to 90 by the run, and the owner speaking:

```
captured, 4 s: 202 callbacks of 320 to 320 bytes, 32000 samples, longest wait 373.57822ms, peak 3534, rms 343
played back: 32000 of 32000 samples
--- PASS: TestProbeCaptureFromGoWithoutCgo
```

What this shows: the input callback enters Go without cgo, the queue converts the device's 16 kHz to 8 kHz mono and delivers whole 20 ms frames, and what was captured can be handed to an output queue. The owner spoke during the capture and confirmed hearing the recording played back on the headset.

What it does not show, and what to look at in the prototype: one wait of 374 ms between two callbacks (424 ms in the run before), against 20 ms expected. Where in the four seconds it fell was not recorded, so a device that starts late and a gap in the middle of the capture are not told apart. The question asked here was Terminal's, not the application's: task 2.4 is untouched.

The input volume was left at 90 and Terminal keeps the permission the owner gave. Two Terminal windows were left open on the Mac's desktop.


## What the run left

The application's folder under Application Support did not exist before the run; it and the temporary folders were removed. The login keychain was not touched. The headset stays with the VM, the input volume at 90, and Terminal allowed the microphone, as said above.
