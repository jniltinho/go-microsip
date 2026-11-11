What this change does: Replaces the Rust softphone with Go/Wails while preserving behavior and user data. This review checks the revised plan, not implementation.

## Round-1 findings

1. **PARTIALLY RESOLVED** — `design.md` D9 and `tasks.md` 3.10 add staging and recovery, but check rather than explicitly hold the Rust instance lock throughout copying, allowing Rust to restart mid-snapshot.

2. **RESOLVED** — `design.md` D3 and `tasks.md` 2.3–2.5 now require wrong-host/out-of-bundle certificate rejection and tampered/replayed SRTP and SRTCP rejection.

3. **RESOLVED** — `proposal.md`, “Rust-era changes superseded,” and `tasks.md` 1.5 now pin the baseline and require every requirement/scenario to have evidence or written approval for removal.

4. **RESOLVED** — `design.md` D1 and `tasks.md` 2.1 force module mode during coexistence and require both Go and Rust workflows to pass.

5. **RESOLVED** — `tasks.md` 2.10 now produces the complete M0 artifact set, including ZIP, manifest, notices, checksums and version verification.

6. **RESOLVED** — `design.md` D5 and `tasks.md` 2.9 replace impossible import-order dispatch with lazy GUI construction and headless version, doctor and MCP checks.

7. **RESOLVED** — `design.md`, “Migration Plan,” and `tasks.md` 7.7–7.10 require post-cutover artifact checks with matching publication hashes and document commit/external-rename rollback.

8. **RESOLVED** — `design.md` D4 and `tasks.md` 2.8, 4.4 and 6.3 provide numerical audio thresholds plus frozen-window and capture-unplug checks across Linux and Windows.

9. **RESOLVED** — `design.md` D2 and `tasks.md` 3.1 add frontend-to-internals restrictions and deliberate violating-import tests.

10. **RESOLVED** — `proposal.md`, “What Changes,” and `design.md` D1 distinguish Rust-free product code from mandatory native audio/webview dependencies.

## New blocking or major findings

11. **BLOCKING — Imported TOML is never installed.**  
    **Location:** `design.md` D9, steps 2–5; `tasks.md` 3.10.  
    The protocol stages the configuration file but commits only the database. A profile whose account or TLS settings reside in TOML therefore cannot satisfy the promised settings import.  
    **Fix:** Include configuration in the journaled commit/rollback and test a profile whose account and TLS settings exist only in TOML.

12. **BLOCKING — Staged SQLite updates can remain outside the committed file.**  
    **Location:** `design.md` D9, steps 3–5; `tasks.md` 3.10.  
    Migrations and the fresh credential reference can remain in the staged database’s write-ahead log (`-wal`), yet commit renames only the main database and never requires a successful checkpoint and close. A crash can consequently lose those updates.  
    **Fix:** Explicitly checkpoint and close the staged database before committing; test WAL-resident migration and credential updates with interruption injection.

13. **MAJOR — Later UI milestones lack the required built-application automation.**  
    **Location:** `tasks.md` 5.2–5.8 and 6.3–6.7; `specs/milestone-distribution/spec.md`, “Automated interface checks.”  
    Only M2 explicitly schedules desktop automation; later skin and integration milestones do not repeat the required built-app/browser checks, allowing dev-mode screenshots to substitute for packaged-webview evidence.  
    **Fix:** Add those checks and receipt links to M3/M4 closeout, using each milestone’s exact artifacts and covering the Windows integration UI.

Not checked: implementation, builds or network sources; library capability, version and licence claims remain **unverified**. Import correctness remains the principal blocker.  
REJECT