# SOUND WITH CHANGES

The revision is substantially better, but is not approval-ready. The discard barrier, executable test plan and release acceptance criteria still need changes.

**Unsigned distribution is accepted as decided.** I am not recommending signing. “Resolved” below means resolved **in the plan**, not implemented or verified at runtime.

References to design sections, tasks and specs mean files under `openspec/changes/add-macos-universal/`.

## Round-1 findings

| Finding | Status | Assessment against the revised text |
|---|---|---|
| F01 | partly | D12, the final-release requirement and task 6.3 now require macOS gates. Task 3.6 correctly requires passing measurements. However, 6.4 does not adequately test registration of the new gates, and 7.3 requires a **filled**, not explicitly successful, hardware sheet. See N12–N13. |
| F02 | partly | D4 and tasks 2.4/3.3 now address device-side stale audio. But “resets or flushes” is not a sufficiently defined AudioQueue barrier; generation assignment and callback races remain unspecified. See N01. |
| F03 | resolved | D4 preserves 8 kHz mono `int16`, puts any necessary conversion inside the adapter, and includes conversion cost in the spike. |
| F04 | partly | D4 explicitly measures the complete callback bridge under contention and GC, with callback reuse and lifetime checks. But task 2.4 supplies only “the Mac,” currently x86_64, whereas D4 requires both architectures before proceeding. |
| F05 | resolved | The requirement was deliberately narrowed to the existing `CaptureLost` contract. It no longer promises speaker-loss events or device names the port cannot supply. Task 3.4 matches it. |
| F06 | partly | Permission states, pending authorization, restriction and revocation now have work assigned. Missing are an explicit cancellation/quit test while authorization is pending and a presentation-level regression test: the existing Windows test checks an adapter error, not the window’s notice. |
| F07 | partly | Encoding, reference validation, size refusal and output secrecy are specified. The newly explicit keychain pathname is another command-language input; empty-value handling and the exact subprocess/output protocol still need definition. See N04. |
| F08 | partly | The deadline, application-generated reference and caller behavior are corrected. Explicitly naming the login keychain does not by itself establish fail-closed behavior of the CLI. See N05. |
| F09 | partly | Sharing the endpoint and preserving lock/cleanup/peer-check ordering is appropriate. D6 overstates some existing Linux file checks, and the Darwin ACL policy lacks an explicit task-4.5 acceptance control. See N08. |
| F10 | resolved | D6 specifies the complete encoded pathname’s 103-byte limit, short fixtures, multibyte cases and kernel PID or the existing unknown value `0`. |
| F11 | partly | Verified external trust, strict checking and allowlisted retained output are substantial improvements. Task 1.4 scans only “the log,” not every output and artifact channel forbidden by the spec. See N06. |
| F12 | partly | The revised hint honestly says Local Network permission is only a possible cause. But no task implements or tests the private-address/public-address distinction. See N11. |
| F13 | partly | The signing branch is gone and feasibility work precedes publication integration. However, task 2.3 requires application audio before the audio spike exists, and microphone feasibility also needs an explicitly instrumented prototype. See N02. |
| F14 | resolved | D8 correctly distinguishes raw URL events from parsed numbers, reuses buffering and shared close behavior, and covers cold/warm launches, service-owned profiles, active-call quit and bounded shutdown. Tasks 5.1–5.3 deliver that work. |
| F15 | resolved | D9 now covers the full notification lifecycle, lazy native initialization and refusal without process failure. Task 5.4 includes headless-command controls. |
| F16 | resolved | D1 and tasks 6.2/6.3 explicitly name package/format coverage, Darwin dependency discovery, upload lists, manifests, checksums and release-required files. |
| F17 | resolved | Slice-specific signatures are described correctly. D1/2.1 require byte preservation and structural validation; D1 also requires native arm64 signature verification. |
| F18 | partly | The missing report fields and no-input behavior now have tasks. The proposed OS-name seam alone does not make Windows path semantics testable on Linux, and the cross-platform `doctor` changes contradict the blanket unchanged-behavior claim. See N09–N10. |
| F19 | partly | A macOS-specific requirement and owner-approval task now exist, and unsupported checks remain explicit. The formal exception should expressly qualify the migration spec’s unconditional navigation-blocking requirement; proposal “Modified Capabilities: None” does not settle that conflict. |
| F20 | partly | Hidden-call, wake, contention and firewall work is added. Explicit acceptance still lacks hidden **idle incoming-call** operation, audio-service restart and an actual Bluetooth profile/rate transition—not merely use of a Bluetooth headset. |
| F21 | resolved | D11 and task 4.7 now extend system-access detection narrowly and provide positive and negative import controls. |

## New findings

These include problems introduced by the proposed fixes, not findings against nonexistent macOS implementation.

| Id | Severity | Section | Concrete problem | Fix |
|---|---|---|---|---|
| N01 | blocker | D4; 2.4, 3.3 | A callback can pass an old-generation check, pause, then write after `Discard` clears the ring. Reading the current generation when a delayed callback arrives also mislabels old audio as new. Furthermore, AudioQueue **flush** delivers buffered data rather than establishing a discard boundary. | Define the capture cutover: stable buffer-generation ownership, synchronized generation check and ring publication, treatment of partial buffers and converter history, callback retirement, and fail-closed reset failure. Add adversarial race tests, not just one delayed buffer. |
| N02 | major | D3/D10; 1.4–1.5, 2.3–2.5, 6.1 | Tasks 1.4–1.5 use `headless.sh` before 6.1 removes its unavailable `timeout` dependency. Task 2.3 requires two-way application audio before 2.4 supplies the spike and 3.2 implements the adapter. The unchanged Darwin backend currently reports no audio service. | Move portability work forward. Define a minimal packaged audio/permission prototype before the signaling-and-media measurement; distinguish prototype work from production adapter completion. |
| N03 | major | D4/D10; 1.3, 2.4, 2.7 | D4 requires callback/ABI measurements on both architectures, but 2.4 names one x86_64 VM. Hosted smoke checks do not supply that audio evidence, and real Apple Silicon acceptance appears only in group 7, after the early decision gate. | Assign an arm64 audio-capable machine before 2.7, or explicitly stage the decision and keep the unmeasured architecture blocked. Record which fixtures supply each rate/channel combination. |
| N04 | major | D5; 4.2–4.3 | Hex makes the password inert, but not the login-keychain pathname. A home pathname containing spaces, quotes or backslashes can change parsing; a newline can introduce another command. An empty hex value also needs an explicit empty argument. Apple’s interactive parser is not a shell parser. [Apple parser source](https://github.com/apple-oss-distributions/Security/blob/main/SecurityTool/macOS/security.c#L828-L931) | Specify argument serialization for **every** stdin operand, reject line-breaking characters, and bound the complete encoded command. Define ASCII-hex storage/readback, empty values, separate stdout/stderr handling and exact completion/status rules. Exercise these against the real tool too. |
| N05 | major | D5; 4.3 | Naming the login keychain alone is insufficient proof that another keychain cannot be used. Apple’s published helper can return `NULL` after an open failure; search/update paths then pass that value onward. This creates a potential default-search fallback, including during `-U` updates. [Keychain helper](https://github.com/apple-oss-distributions/Security/blob/main/SecurityTool/macOS/keychain_utilities.c#L71-L150), [update path](https://github.com/apple-oss-distributions/Security/blob/main/SecurityTool/macOS/keychain_add.c#L45-L70) | Add a native negative control: unusable login keychain plus a matching decoy item in another keychain. Get/set/delete must fail without reading or changing the decoy. Require fail-closed selection; use the stated framework fallback if the CLI cannot guarantee it. |
| N06 | major | D10; 1.4; distribution SSH requirement | An implementation can keep its selected retained log clean while leaking through Make’s console output, subprocess stderr, temporary files under `artifacts/`, copied remote reports or screenshots. Task 1.4’s controls inspect only “the log.” | Extend controls to outer stdout/stderr and the complete artifact tree, including failures and interruption. Prevent raw diagnostic retention, suppress command tracing/echo containing target values, and validate selected result values before emitting them. |
| N07 | major | 4.2 | Tests implemented with `keyring_darwin.go` cannot execute in the Linux build image: that file is excluded there, and a cross-built Darwin test executable cannot run on Linux. A stand-in executable does not change this. | Put command construction/parsing and runner controls in a portable helper tested in Docker, with thin Darwin wiring; execute Darwin-specific tests on the Mac. Name that split in the task. |
| N08 | major | D6; 4.4–4.5 | “Only folder and peer credentials differ” omits Darwin-specific ACL and pathname validation. Applying 103 bytes in shared code would reject Linux paths currently accepted. D6 also describes all existing files as ownership/mode checked, but Linux `Listen` does not inspect an existing lock file’s owner/mode, and `Dial` does not check socket mode bits (`endpoint_linux.go:85,148–154`). | State the actual inherited contract precisely. Keep Darwin-only validation separate and explicitly test ACL exposure. Any shared security hardening must be acknowledged and regression-tested rather than described as unchanged behavior. |
| N09 | major | D7; 4.1 | Passing `"windows"` into a selector does not change Linux `filepath.IsAbs` or `filepath.Join`. A Linux table using real drive-letter or UNC paths can reject or construct them incorrectly despite selecting the Windows branch (`resolve.go:49–66`, `app.go:60–78`). | Test the portable selection separately from native path operations, and run native Windows path controls. Preserve APPDATA versus LOCALAPPDATA, XDG precedence, relative-path refusal and explicit configuration behavior. |
| N10 | minor | Proposal non-goals/impact; D7; 3.7, 4.1 | The plan simultaneously promises unchanged Linux/Windows behavior and requires new paths/output fields and changed empty-input reporting **on every system**. Existing `audio.available` is true after successful enumeration even with no inputs (`cmd/doctor.go:39–51`). | Declare the intended cross-platform diagnostic changes as explicit exceptions. Specify JSON/text compatibility and whether overall, capture and playback availability are separate fields. |
| N11 | major | Integration “A PBX on the local network” | The private-address timeout hint is a new mandatory behavior with no delivering task. Existing registration events expose a failure category, not the actual resolved signaling destination; `user@domain` is not necessarily that destination (`service.go:982–988,1036–1044`). | Add implementation and tests at a defined boundary. Cover private/public addresses, hostnames resolving to each, ordinary timeout versus rejection, and absence of the hint on Linux/Windows. Do not infer privacy denial. |
| N12 | major | D12; 6.3–6.4 | An unchecked checkbox is already rejected by `check-gates.sh:14–17`, even without macOS in the required-change list. Thus 6.4’s proposed negative case can pass while the actual F01 regression remains. The existing self-test also overrides the change list, bypassing production defaults. | Test notes with **no open checkbox but an omitted macOS task label**, using the production required-change list; add a complete positive case. Test a self-consistent legacy artifact set lacking macOS. Land these controls with the transition. |
| N13 | major | D12; 7.2–7.3 | A sheet can be “filled” with an arm64 sound failure or `not run`, yet satisfy 7.3’s literal completion criterion. Conversely, unconditional notification-button rows conflict with D9’s permitted unsigned-notification refusal. | Require successful results for mandatory hardware rows on each architecture. Define narrowly permitted `not applicable` outcomes for optional features, with evidence of the specified fallback. A filled failure sheet must not clear the final gate. |
| N14 | minor | Distribution CI requirement and “Green run” scenario | The requirement permits an unavailable hosted architecture, but “Green run” still unconditionally requires both arm64 and x86_64 jobs. | Qualify the scenario by hosted availability, consistently with the requirement and the documented alternative coverage. |

### D4: AudioQueue assessment

**The 8 kHz queue format is a reasonable spike candidate.** It matches the existing port. Actual conversion, variable callback sizes, drift and latency still need measurement; AudioQueue buffers must not be assumed to arrive as exactly 160 samples.

**The discard design is not yet complete enough to approve.** Apple distinguishes a flush that delivers buffered data from a reset that removes scheduled buffers and may invoke callbacks. Reset therefore introduces callback/re-enqueue and synchronization cases, not merely a ring clear. [AudioQueueFlush](https://developer.apple.com/documentation/audiotoolbox/audioqueueflush(_:)), [AudioQueueReset](https://developer.apple.com/documentation/audiotoolbox/audioqueuereset(_:))

Task 2.4 should additionally force:

- A callback paused between validation and ring publication.
- A partially filled buffer spanning the cutover.
- Callback delivery during reset and attempted re-enqueue.
- Reset failure, repeated discard, and discard racing close.
- Fresh post-barrier audio eventually passing, while every marked stale sample remains absent from RTP.

The current delayed-buffer test is useful, but insufficient.

### D5: Remaining leakage and parsing uncertainty

Direct stdin execution, hex encoding and prohibition of verbose/raw-output logging address the main argv/log exposure. They do **not** provide application-isolated storage; D5 correctly acknowledges same-user readability.

Specify one mutating command followed by EOF. Interactive mode reports the last command’s result, so an appended blank command or exit command can replace an earlier failure status. Keep result parsing operation-specific rather than expecting every operation to return one payload line. [Apple interactive loop](https://github.com/apple-oss-distributions/Security/blob/main/SecurityTool/macOS/security.c#L1094-L1137)

The CLI fallback concern in N05 comes from published source; I have **not** established that the installed macOS 15 tool exhibits it.

### D10: Target confidentiality

**Confidentiality is not yet demonstrated by the planned controls.** The external verified trust file and allowlisted retained log are sound choices, but the specification prohibits disclosure through *any* log or artifact. Task 1.4 must test that larger boundary, especially now that `doctor` will print target-user paths.

### D12: Publication and transition

If task 6.3 is implemented exactly as written, the final-release checker will require macOS task labels, and the producer/upload/required-file lists will transition together. I found no additional filename-list omission in that proposed atomic change.

Two qualifications remain:

1. N12’s control does not prove that enforcement was connected; N13 allows insufficient evidence to be labelled complete.
2. After the transition, an incomplete macOS gate blocks the **entire final release**, including Linux and Windows files. That follows from the existing single-set publisher. Pre-releases remain possible; independent Linux/Windows final publication does not.

The checker verifies checklist syntax and labels, not the truth of hardware receipts.

## Checked and found supported

- The unsigned decision is consistently stated; local-network and notification limitations are measured rather than universally assumed.
- Desktop-launched packaged permission checks are correctly distinguished from SSH/headless execution.
- D8 reuses existing link buffering and quit/close behavior rather than duplicating them.
- D9 now reflects the existing notification port’s lifecycle.
- Package coverage, notices, uploaded artifacts and release-required files are explicitly assigned.
- The publication scanner already extracts all ZIP files.
- I found **no design task-number citation pointing to an unrelated revised task**. For example, D3’s 7.4 is quarantine verification, D5’s 4.3 is native keyring acceptance, and D12’s 7.3 is the hardware sheet.

## Not checked

- No builds, tests, project scripts, application binaries, SSH connections, VM operations or calls were executed. No files were changed.
- Actual AudioQueue/purego behavior, permissions, keychain behavior, runner availability and preliminary observations remain unverified.
- The extract does not supply the frontend implementation or complete pinned toolkit source. Additional ai-memory retrieval tools were unavailable.

**This is a static review, not evidence that anything builds, runs or carries a call.** The principal remaining risks are stale microphone audio crossing the discard boundary and final-release checkmarks overstating native acceptance.