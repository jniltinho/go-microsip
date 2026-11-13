What this change does: Replaces the Rust softphone with a Go/Wails application while preserving telephony, security, data and agent-control behavior.

## BLOCKING

1. **Import lacks a consistent snapshot and commit protocol** — `design.md`, D8–D9; `tasks.md`, 3.10.  
   Copying a SQLite database while Rust is running can omit uncheckpointed WAL data or produce an inconsistent snapshot; replacing Go’s files while its database remains open is also unsafe. A filesystem swap cannot atomically include the promised keyring update.  
   **Fix:** Specify a consistent SQLite snapshot, quiesce destination access, stage credentials under fresh references, and define crash recovery; test concurrent source writes and failures between commit steps.

2. **G0 security evidence does not establish its acceptance criteria** — `design.md`, D3; `tasks.md`, 2.3–2.5.  
   Successful encrypted audio, rejection of one untrusted certificate and absence of a UDP listener do not prove SRTCP protection, replay rejection or replacement—not supplementation—of system CA roots. G0 could therefore approve without demonstrating its stated security properties.  
   **Fix:** Require tampered and replayed RTP/SRTCP rejection, certificate-name rejection, and a system-trusted certificate rejected when outside the configured custom roots.

3. **The parity inventory can omit baseline requirements** — `proposal.md`, “Rust-era changes superseded”; `tasks.md`, 1.5 and 7.4; `specs/implementation-migration/spec.md`, “Honest status of superseded work.”  
   The proposal retains the superseded changes’ requirements, but task 1.5 inventories code symbols and configuration tables rather than those requirements. Completing that matrix does not establish every behavioral requirement, while “kept only where go-microsip proves them” permits the baseline to shrink.  
   **Fix:** Pin the baseline documents and candidate revision; give every requirement/scenario a matrix row requiring evidence or an explicitly approved scope change.

4. **The claimed collision-free root layout overlooks Go vendoring** — `design.md`, D1; `proposal.md`, “Unchanged until cutover.”  
   Keeping Rust’s root `vendor/` beside a modern `go.mod` triggers Go’s automatic vendor-mode selection, potentially making Go resolve dependencies against the Rust vendor tree.  
   **Fix:** Explicitly bypass that directory in every coexistence build/check, for example with `GOFLAGS=-mod=readonly`, and verify both Rust and Go workflows.

5. **M0 has no task producing the complete milestone set** — `specs/milestone-distribution/spec.md`, “Milestone artifact set”; `tasks.md`, 2.10–2.11.  
   Every milestone requires four artifacts, checksums, notices and a recorded commit, but M0 only explicitly builds the Linux binary, AppImage and Windows executable. Its closeout does not verify the ZIP, manifest, notices or checksums.  
   **Fix:** Add the same complete `go-dist` closeout and artifact-version verification used for later milestones.

## MAJOR

6. **Mode dispatch cannot precede Go imports** — `design.md`, D5; `tasks.md`, 2.9.  
   Imported Go packages initialize before `main`; runtime dispatch cannot happen “before importing” GUI-initializing packages. Testing only `version` without a display also leaves the required doctor and MCP paths unproven.  
   **Fix:** Require lazy GUI construction, audit initialization effects, and exercise doctor plus a stdio MCP handshake without a display during G0. Wails initialization behavior is **unverified**.

7. **Final packaging changes follow the runtime gate** — `tasks.md`, 7.2–7.9; `design.md`, “Migration Plan.”  
   Runtime evidence and sign-off precede changes to packaging, default builds and release workflows; download/checksum verification does not establish that the resulting release still runs. Rollback is also described as one revert although cutover spans tasks and repository renaming is external.  
   **Fix:** Revalidate the final release artifacts from the post-cutover commit before publication, and document rollback for every cutover commit and external rename.

8. **Real-time acceptance remains incomplete** — `specs/modular-core/spec.md`, “Real-time audio path stays bounded”; `tasks.md`, 2.8 and 3.8.  
   Recording jitter and underruns supplies no pass/fail threshold. No task explicitly verifies the required five-second frozen-window scenario, and device enumeration does not establish safe behavior when capture disappears.  
   **Fix:** Add bounded acceptance thresholds, frozen-frontend and hot-unplug tests, with Linux and Windows evidence.

9. **Layering checks omit the frontend boundary** — `design.md`, D2; `specs/modular-core/spec.md`, “One command and event contract for every front end.”  
   The listed import rules allow CLI, MCP or GUI packages to import SIP, media or storage internals directly, despite the specification prohibiting this.  
   **Fix:** Add frontend-to-internals restrictions and deliberate violating-import tests; expose necessary queries through the core-owned contract.

## MINOR

10. **“Pure-Go” conflicts with mandatory native components** — `proposal.md`, “What Changes”; `design.md`, D4.  
    Mandatory cgo audio and native webview dependencies make “pure-Go” misleading for builders and distributors.  
    **Fix:** Say “Rust-free Go implementation with native audio/webview dependencies,” distinguishing core portability from application packaging.

Not checked: implementation, builds or network sources; third-party version, capability and licence claims remain **unverified**.
REJECT