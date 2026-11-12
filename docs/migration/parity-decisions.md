# Parity gate: the decisions that are the owner's

Change `openspec/changes/migrate-to-go-wails`, task 7.4. The audit of the [parity matrix](parity-matrix.md) left three kinds of rows that evidence alone cannot close: differences go-microsip has on purpose, behavior the Rust-era text requires and go-microsip does not have, and regressions that were measured. Each is listed once here with the options and a recommendation. **Nothing here is decided: the "Decision" column is empty until the owner fills it.** The state of the gate is in the [parity receipt](../receipts/go-m5-parity-2026-11-12.md).

How to answer: by item, with `accept` (the difference stays and the row closes as an accepted difference), `implement` (it becomes work before the gate), `drop` (the requirement is left out of this release, in writing) or anything else in words. "As recommended" for a whole group is an answer too.

Effort is a guess of the work with its tests and runs: S is hours, M a day or two, L more.

## 1. Already decided, to be confirmed in writing for the gate

These were the owner's instructions or acceptances during the migration. The gate asks for them in one place. Recommendation for all: **confirm**.

| # | Difference | Where it was decided | Rows | Decision |
| --- | --- | --- | --- | --- |
| P1 | The names: `go-microsip`, the `GO_MICROSIP_` prefix, Rust data only through the explicit import | approval of 2026-11-11, design D8 and D9 | open points, B7 |  |
| P2 | The window is a web page in the system's web view (Wails v3, Vue), not a native toolkit | approval of 2026-11-11, design D5 and D14 | A93 |  |
| P3 | Layout B (Home, Calls, Contacts, Keypad at the bottom) instead of the MicroSIP-like arrangement; no side-by-side comparison with MicroSIP | owner's choice of 2026-11-11 and review of 2026-11-12 | A94, A136, A137 |  |
| P4 | Seven skins and System; five of them round their corners, Windows 98 and Matrix are square. The approval of 2026-11-11 said "square corners in every skin"; the later review changed it | design D7, [review](../prints/go/REVIEW.md) | A131, A132 |  |
| P5 | No MSI on Windows: a portable `.exe` and `.zip`. With it: no Start Menu entry, and the link registration stays in the registry when the folder is deleted | approval of 2026-11-11 | A28, A29, A44 |  |
| P6 | The command line is Cobra with subcommands (`doctor`, `mcp`, `version`) instead of options | owner's instruction of 2026-11-11 | open points, B3 |  |
| P7 | The independent reviewer is Codex | `AGENTS.md`, design D12 | A112 |  |
| P8 | The application offers itself for `sip:` and `tel:` links at every start, on Linux and on Windows, as a candidate and never as the default; no switch for it and no removal | owner's decision of 2026-11-12 (M3) | A87, A42, A44 |  |
| P9 | On Windows agent control uses a Unix-domain socket in the user's folder, not a named pipe | design D10, "for the owner to confirm at the parity gate" | A13 |  |

## 2. Differences not decided yet

| # | Difference | Options | Recommendation | Effort | Rows | Decision |
| --- | --- | --- | --- | --- | --- | --- |
| D1 | Linux needs the desktop's GTK 4 and WebKitGTK 6.0; the Rust candidate was one self-contained executable | accept; or bundle the libraries in the AppImage | **Accept.** It follows from P2 on the supported distributions, both of which bring the libraries with their desktop (checked in the VMs). Bundling WebKit makes the AppImage several times larger and ties it to one build | none | A103 |  |
| D2 | One executable on Windows, no console one: commands answer with their output redirected; what an attached console shows was not checked | accept; or build a second console executable | **Accept**, and look at an attached console in the manual checklist of 7.2 | none | A16, B3.21 |  |
| D3 | The single instance on Windows is the toolkit's, not a pipe of our own | accept; or write our own | **Accept.** It was driven in the VM (a second start hands its link over and ends) | none | A36 |  |
| D4 | Icons are the page's own line drawings (inline SVG), not a reviewed icon font | accept; or adopt an icon font | **Accept.** Nothing is fetched, every state carries an icon and a label in every skin (audited), and there is no font to review | none | A134, A135 |  |
| D5 | Screens are checked by audits that fail a run (contrast, state icons, focus rings, scale), not by comparison with approved baseline pictures | accept; or add baselines | **Accept.** Seven skins make baselines costly to keep, and the audits measure what a baseline only resembles | none | A139, A140 |  |
| D6 | Nothing forbids the `unsafe` package or Windows system calls outside the platform packages (the Rust rule confined `unsafe` to one crate) | accept; or extend the layering test | **Implement.** One more rule in `internal/archtest`: `unsafe` and `golang.org/x/sys/windows` only in the files that have them today | S | A50, A51 |  |
| D7 | A database that cannot be opened does not stop the window or `run` when a file or the environment gives the account: they warn and register. Only the service refuses. The Rust candidate did the same | accept; or refuse to start | **Accept**, with a test that starts over a database that cannot be opened and a line in the design | S | A74 |  |
| D8 | Three properties of the Rust SRTP adapter that the Go stack (pion/srtp) does not give: the receiver bound to one SSRC, transmission closed before the packet index runs out, a bounded bank of unauthenticated sources | accept; or patch the vendored stack | **Accept.** The index space is far beyond any call, and the softphone takes media only from its PBX. Recorded as a known limit | none | open points |  |
| D9 | Small protocol differences: a DTMF key is 7 packets of 100 ms, not 8 of 120 ms; a result older than a day answers `not_found`, never `result_expired`; six error codes the Rust code defined and never used; values such as `rtp` where Rust printed `plainrtp` | accept; or match Rust byte for byte | **Accept.** No client depends on the Rust wire format (design D10), and the lab's PBXs read the keys | none | A9, A10, open points |  |
| D10 | Storage: the backup before a migration is made with `VACUUM INTO`; an empty password reference clears it instead of failing; the doctor prints strings quoted once | accept | **Accept** | none | open points |  |
| D11 | No variable that turns on engine diagnostics (the Rust `RUST_MICROSIP_TRACE`); warnings go to the standard error and `--ui-trace` records the window | accept; or add `GO_MICROSIP_TRACE` | **Accept** for now: add it when a user's problem needs it | S if wanted | B7.5 |  |
| D12 | The build image pins its base by digest but takes distribution packages and some tools without a snapshot or checksums | accept for the candidate; or pin everything | **Accept for the release candidate**, listed as a limitation; pin with task 7.6 if a final version is cut | M later | A26, A27 |  |
| D13 | Windows 10 is not run, and WebView2 is not part of every Windows 10 | say Windows 11 only; or Windows 10 with the WebView2 runtime | **Say Windows 11**, and Windows 10 "with the WebView2 runtime, not tested" | none | A34, A35 |  |
| D14 | The measured regressions: a usable window after 0.5 to 0.8 s instead of about 0.1 s, four to five times the memory on Linux, a `.exe` half as large again ([measurements](../receipts/go-m5-measures-2026-11-12.md)) | accept | **Accept.** They are the cost of P2; nothing cheap changes them | none | task 7.3 |  |
| D15 | The settings form does not say which source holds a field (flag, environment, file, database); `go-microsip doctor` does | accept; or mark overridden fields in the form | **Accept**: the doctor is the tool for it | M if wanted | A64, A66, A68 |  |
| D16 | One account only: a second enabled account cannot be written in the configuration, so there is no error for it | accept | **Accept** | none | A116, A118 |  |
| D17 | Plain RTP is what an account uses unless SRTP is chosen, and the warning is the "Unencrypted (RTP)" badge on every call, not a standing notice | accept; or a standing warning | **Accept.** The badge is on every call screen, and SRTP accounts refuse a plain answer | S if wanted | A59 |  |

## 3. Behavior the Rust text requires and go-microsip does not have

| # | Missing | Options | Recommendation | Effort | Rows | Decision |
| --- | --- | --- | --- | --- | --- | --- |
| G1 | **No sound when a call rings**, and no ringback tone while an outgoing call rings: go-microsip plays nothing of its own, the call shows in the window and as a notification. The Rust text asks for a ringing output that can be chosen | implement a ring and a ringback on the output device; or also a device of its own for ringing; or drop | **Implement the ring and the ringback** on the call's output device; drop the separate ringing device. A softphone that does not ring is missed when the window is not looked at | M | A55 |  |
| G2 | **No local audio test** (hear the speaker, see the microphone's level without a call); the settings name it as not available | implement; or drop | **Implement.** It is how a person finds the wrong headset before a call | M | A57, A58 |  |
| G3 | **A device chosen in the settings applies at the next start**, not at the next call or during one; a microphone lost in a call is reported and the call stays silent | apply at the next call; or also during a call; or accept | **Apply at the next call** (and say so in the settings); drop the change during a call | S to M | A55, A56 |  |
| G4 | **No notification for a missed call**, on either system; no flashing taskbar button on Windows | implement the missed-call notification; the flash too; or drop | **Implement the missed-call notification**; drop the flashing button | S to M | A85, A39, A41 |  |
| G5 | **Contacts cannot be imported from CSV in the window**: the database layer has the preview and the import, nothing calls them | offer it in the window; or as a command; or drop | **Implement in the window**: the hard part exists and is tested | M | A79, A80 |  |
| G6 | **No STUN and no keepalive**: calls work with a PBX that anchors the media and answers to where packets come from, as the lab's and FreePBX do | implement STUN and keepalive; or document the supported topology | **Drop STUN for this release and document the topology.** A keepalive for UDP registrations is worth having if a user sits behind a home router: decide after 7.2 | S to document, L to implement | A61, A62 |  |
| G7 | **No notice that names Windows' privacy setting** when the microphone is blocked there | implement the notice; or drop | **Implement the text**, and try it on a real machine in 7.2 | S | A21 |  |
| G8 | **No speaker and microphone volume** in the window; the settings name it as not available. The Rust text does not ask for it | implement; or drop | **Drop**: the system's mixer does it, per application, on both systems | none | none |  |
| G9 | **No SBOM for the Go artifacts** (the Rust candidate shipped one); the third-party notices list 82 components | generate one at release; or drop | **Implement with the release work** (tasks 7.6 and 7.10) | S to M | A27, A28, A108, A114 |  |
| G10 | **No access list of their own on the Windows folders and grant files**: they take the user's profile permissions | set an owner-only list; or accept | **Accept for now and test with a second Windows user in 7.2**; implement if that user can read them | M if needed | A23, A47, A48 |  |
| G11 | **A transfer that FreePBX accepts and then fails leaves a call with nobody in it**: the softphone says the transfer failed and waits for the person to hang up | end the call when the PBX took the other party away; or accept | **Accept**: the softphone cannot tell an emptied call from a silent one; say it in the documentation | none | task 7.1 |  |
| G12 | **History is kept for 90 days, with no setting** (as in the Rust candidate) | accept; or add a setting | **Accept** | S if wanted | A83 |  |

## 4. What needs no decision

Evidence still to produce, in the lab or on real machines, is listed in the [parity receipt](../receipts/go-m5-parity-2026-11-12.md) under "Evidence still to produce". The largest part is the manual checklist of task 7.2 and the switch of CI and the release gates to Go (tasks 7.5 and 7.6).

## What the recommendations add up to

If every recommendation is taken: 9 confirmations, 16 differences accepted (D7 with a test), 5 gaps dropped or accepted (G6, G8, G10, G11, G12), and work before the gate on D6, G1 to G5, G7 and G9. G1 and G2 are the two that a person using the softphone would miss first. No date is given: the efforts above are guesses.
