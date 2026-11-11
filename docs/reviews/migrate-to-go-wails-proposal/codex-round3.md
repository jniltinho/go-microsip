## Previous findings

1. **RESOLVED** — `design.md` D9 step 1 and `tasks.md` 3.10 now require holding the Rust instance lock throughout the snapshot and Go’s lock throughout import, with Go’s store closed.

11. **RESOLVED** — `design.md` D9 step 6 and recovery now include both the database and TOML configuration in the journaled commit/rollback, with a TOML-only settings fixture in `tasks.md` 3.10.

12. **RESOLVED** — `design.md` D9 step 5 explicitly requires checkpointing and closing the staged database, confirming sidecar removal and syncing files before commit, with corresponding verification in `tasks.md` 3.10.

13. **PARTIALLY RESOLVED** — `tasks.md` 5.8 and 6.7 now require exact packaged Linux/Windows desktop checks, but M4 still lacks the browser-automation run and keyboard/per-screen-per-skin evidence required by `specs/milestone-distribution/spec.md`, “Automated interface checks.”

## New major finding

14. **MAJOR — Credential creation has an unjournaled crash window.**  
    **Location:** `design.md` D9 steps 4–6 and recovery; `tasks.md` 3.10.  
    **Problem:** Step 4 writes the password to the keyring before recording its fresh reference in the staged database; no durable reference record is required before that write. A kill between those operations can therefore leave a password that recovery cannot reliably identify and remove, contradicting the promised absence of staged credentials. Tests only after whole steps do not exercise this boundary.  
    **Concrete fix:** Persist and sync the allocated credential reference in the existing import journal before writing the keyring entry; add interruption injection immediately after the keyring write and before the database update, verifying cleanup on restart.

Not checked: implementation, builds or network sources; library capability, version and licence claims remain **unverified**.

APPROVE WITH CHANGES