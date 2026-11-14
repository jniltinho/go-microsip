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


## The prototype of the adapter (2.3, not done)

Written after the captures above, in the working tree that follows `3ec84a2`: `coreaudio_darwin.go` behind the build tag `coreaudio` (device list, a playback queue, capture queues), the capture cutover of D4 in `cutover.go` and the conversion in `decimate.go`, both without a build constraint so their tests run in the build image with the race detector.

**What a capture queue hands over.** The spread of its callbacks, measured by the probe on the headset (a 16 kHz input whose device hands over 512 frames, 32 ms, at a time):

| the queue's rate | callbacks in 4 s | waits between callbacks |
|---|---|---|
| 8 kHz | 208 | 174 under 5 ms, 30 of about 128 ms |
| 16 kHz | 212 | 144 under 5 ms, 67 of 30 to 100 ms (longest 64 ms) |
| 48 kHz | 213 | 80 under 5 ms, 131 of 30 to 100 ms (longest 32 ms) |

So a capture queue hands over about 1024 frames of its own format at a time, whatever they last: at 8 kHz that is 128 ms in one burst, more than the engine keeps waiting (120 ms), and a first version of the prototype with an 8 kHz capture queue lost a quarter of a call's frames to it (355 of 500 captured, 17920 samples dropped). The emulator's USB transfer buffers do not set it: the same with them an eighth of their size. Whether a Mac that is not a VM does the same was not measured. It is not the device's buffer: asked for 160 frames at a time, the device hands over 160, and the queue's bursts stay at 120 to 130 ms. A callback's buffer was 128 to 640 bytes at 16 kHz and 0 to 1920 at 48 kHz: never assumed to be one frame.

The prototype therefore opens capture at 48 kHz and brings it down to 8 kHz itself, one sample in six behind a low-pass (D4's "conversion goes into the adapter"). Its tests, in the build image: tones of 300, 1000 and 3000 Hz come out at their level, tones of 5, 7, 12 and 20 kHz at under a hundredth of it, and the output is the same sample for sample for any size of buffer.

**The call test on the Mac**, the engine's side every 20 ms taking a frame from the headset's microphone and playing it, ten seconds, a discard halfway, started from the Mac's Terminal:

```
inputs: 1, outputs: 2, default devices among them: 2
one second played: 0 samples dropped, 480 of silence          (an output alone: no capture queue exists)
Open returned after 21ms
10 s: 477 frames captured, 23 ticks without one, first after 240ms, longest gap between two 40ms, peak 3618
discard: returned after 2µs, first frame 200ms later
dropped: 0 captured samples, 320 played; 800 samples of silence played
--- PASS: TestCoreAudioCall
```

An input and an output that do not exist are refused. The 23 ticks without a frame are the 240 ms before the first one and the 200 ms after the discard.

**The cutover's tests**, with the race detector: a callback held between its check and its publication keeps a discard waiting, and what it published is gone after the discard; a buffer of a retired queue is dropped and not handed back; callbacks delivered while the old queue is disposed of publish nothing; a queue that cannot be reopened loses capture and keeps nothing running; two hundred discards leave one queue; a discard racing Close leaves none; fresh audio flows after the barrier and nothing marked does; an opening still under way when Close returns cannot publish, and its queue is disposed of when it returns. These use queues made of memory. Four deliberate faults in the cutover were each caught: the lock released between check and publication, no check of the instance, a closed stream that keeps its queue (caught only after the test was made to wait for the opening to begin), a failed reopening not reported.

**In the bundle.** `make go-macos GO_MACOS_TAGS=coreaudio` builds the application with the prototype (at `4d298e9` with the packaging change of the next commit in the working tree):

```
18cd98c927e79f71e22702ca8feaf34fa1c52aaed4377bb3208863eaf7ae9f99  go-microsip-0.2.0-rc.1-macos-universal.zip
```

`make go-mac` passes on it (version, headless, self-test), and its `doctor` on the Mac says `audio: available (1 inputs, 2 outputs)`, `{"available": true, "inputs": 1, "outputs": 2}` in JSON, where the build without the tag says `unavailable`. No call was placed with it: the first call is the measurement of 2.4 and 2.5, which wants a clean privacy state and the owner at the screen.

## The first call through the bundle (2.4 and 2.5, both in part)

The bundle with the prototype, built at `0ffd793`:

```
7dfabd1cd8751b1ce8b6e0d4dcbcccc33ec8d90bc3b5e2b67d4e6d54452b051d  go-microsip-0.2.0-rc.1-macos-universal.zip
```

It was started with `open`, so that macOS takes it for an application of its own and not for a child of the SSH session, with a fictional account of the disposable lab PBX (`1001`, UDP, its password in a private file). The PBX ran in the lab's default profile in a container on the host, which the VM reaches at the container's own address: an address outside the Mac's subnet. The application's privacy state was clean: this bundle had asked macOS for nothing before.

**Registration.** The window shows the account as registered and `status` says `"registration": "registered"`; the PBX lists its contact. macOS asked nothing: no Local Network question for a PBX outside the Mac's subnet. A PBX on the Mac's own subnet was not tried; that half of 2.5 is open.

**The call and the microphone question.** `600` typed in the window and Enter (by the run, through the VM's keyboard; the owner's own attempt before it left no trace in the window or at the PBX, and what went wrong there is not known). The call is established at once: `"phase": "established"`, and the PBX has the channel in `Echo`. macOS then shows:

> "go-microsip" would like to access the microphone.
> go-microsip sends your voice to the person you are on a call with.
> Don't Allow / Allow

It names the application and carries the bundle's own declaration. While the question is open, for about half a minute: the call stays established; the window says "The microphone stopped; the call continues without it"; the PBX counts 517 RTP packets received in the call's first 11 seconds, which the engine sends as silence when it has no captured frame, so the stream is kept up and nothing recorded is in it (no capture queue has started: its start does not return while the question is open, as seen above with Terminal).

**Allowed.** The owner pressed Allow and confirmed hearing themself through the PBX's echo. The window then shows "Connected" and "Unencrypted (RTP)" without the warning, and the menu bar shows macOS's microphone-in-use mark. At 90 seconds of the call the PBX counted 4397 packets received and 4397 sent, none lost in either direction, jitter 0.015 and 0.004: 48.9 packets a second received where 50 would be every frame, so about one frame in forty-five was not sent. Why was not looked at.

Not measured of 2.4: Don't Allow, a call ended while the question is open, the application quit while it is open, and a second call after each answer. Not measured of 2.5: a PBX on the Mac's subnet, and the settings afterwards.

Not done, of what 2.3 asks: the adversarial cases on a real queue; a device at 44.1 kHz and a stereo one; contention and collection over the whole capture path; open and close repeated; five minutes with the interface stalled and the negative control; the delays from capture to RTP and from RTP to the speaker; anything on arm64. The owner, at the headset, confirmed hearing themself during the echo test; no signal was injected or measured by a fixture.

## What the run left

The application's folder under Application Support did not exist before the run; it and the temporary folders were removed. The login keychain was not touched. The headset stays with the VM, the input volume at 90, and Terminal allowed the microphone, as said above.
