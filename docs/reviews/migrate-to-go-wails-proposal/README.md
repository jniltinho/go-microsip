# Codex review of the `migrate-to-go-wails` proposal

Independent review by `codex exec` (codex-cli 0.167.1), read-only sandbox, run on a copy that held only the change's `proposal.md`, `design.md`, `tasks.md` and `specs/` (no repository access, no secrets, no personal paths, nothing from `base/`). Date: 2026-11-11. This is a review of planning artifacts; it is not runtime or call evidence.

| Round | Prompt SHA-256 | Verdict | Report |
| --- | --- | --- | --- |
| 1 | `0a3a3d983463dc9770ed1e79ad2492ca3573400c9fb0e0502655795f09d707d7` | REJECT | [codex-round1.md](codex-round1.md) |
| 2 | `0e2234b250a1354ff6c31dad54f60ce97963eb251ca43ee38dfcc7e7faa47572` | REJECT | [codex-round2.md](codex-round2.md) |
| 3 | `fd36c1605c263c5e9e4baa8b1df27fcd034996701ce34347fbe96e6c7982c6f1` | APPROVE WITH CHANGES | [codex-round3.md](codex-round3.md) |
| 4 | `022a8f06106f804d23440ee4e74c5adfd7922da56ff2b2c1f8f7f7cd9a5f4cd2` | APPROVE | [codex-round4.md](codex-round4.md) |
| 5 | `d9c7cd783095240fd37e77c4aa616bd0303470de61c0c080b3db4dae6784f9b3` | APPROVE WITH CHANGES | [codex-round5.md](codex-round5.md) |

## Resolutions

| Finding | Resolution |
| --- | --- |
| 1 Import snapshot and commit protocol | Design D9 rewritten: both instance locks held, snapshot with WAL sidecar, journaled commit and recovery; task 3.10 tests each interruption. Confirmed resolved in round 3. |
| 2 G0 security evidence | Design D3 and tasks 2.3 and 2.5 require negative proofs (wrong host, certificate outside the bundle, replayed and tampered SRTP/SRTCP). Confirmed in round 2. |
| 3 Parity inventory by requirement | Baseline pinned at `v0.1.0-rc.3`; matrix part A has a row per requirement and scenario. Confirmed in round 2. |
| 4 Rust `vendor/` and Go vendor mode | `GOFLAGS=-mod=readonly` during coexistence; task 2.1 checks both workflows. Confirmed in round 2. |
| 5 M0 artifact set | Task 2.10 builds the full milestone set. Confirmed in round 2. |
| 6 Mode dispatch before imports | Lazy GUI construction; headless `version`, `doctor` and MCP handshake checked in G0. Confirmed in round 2. |
| 7 Packaging after the gate | Post-cutover rebuild and re-check (task 7.7), per-step rollback. Confirmed in round 2. |
| 8 Real-time acceptance | Numeric threshold, frozen-window and unplug checks on Linux and Windows. Confirmed in round 2. |
| 9 Front-end layering | Front ends limited to `core/domain`, `core/ports`, `core/app`; violation tests. Confirmed in round 2. |
| 10 "Pure-Go" wording | Reworded; native dependencies stated. Confirmed in round 2. |
| 11 TOML not installed by import | Configuration file is part of the journaled commit; TOML-only fixture. Confirmed in round 3. |
| 12 Staged SQLite WAL | Seal step: checkpoint, close, no sidecar, sync. Confirmed in round 3. |
| 13 Automation at later milestones | Tasks 4.8, 5.8 and 6.7 run desktop and browser automation on exact artifacts. Confirmed in round 4. |
| 14 Credential crash window | Reference journaled before the keyring write; kill injected at that boundary. Confirmed in round 4. |
| 15 (round 5) cgo-disabled scenario included the GUI | Scenario limited to core, CLI, MCP and control, as the reviewer proposed; cgo allowed only in the audio adapter and the Linux webview layer. Applied after round 5; not re-reviewed. |

Round 5 covered the owner decisions folded in after round 4: `gophone` as an optional lab peer only, the Ubuntu 26.04+/Debian 13 baseline, and cgo allowed for audio. Library capability, version and licence claims were marked unverified by the reviewer in every round and are gated by the G0 spike.
