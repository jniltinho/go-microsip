## Earlier findings

1. **RESOLVED — sound devices saved before an account exists.**

   `OpenPhone` (`src/internal/core/app/phone.go:138–142`) now reads both device IDs whenever the settings load, independently of account resolution, and passes them into the engine options.

   `TestSavedSoundDevicesAreUsedWithoutAnAccount` (`phone_test.go:364–390`) saves both selections, reopens the same account-free profile, and checks the startup IDs and settings readback. It does not establish actual device opening during a call; the source assignment nevertheless resolves the reported M2 startup defect.

## New problems

None found.

I found no additional concrete closure-blocking defect within the stated one-account, one-call, one-window, one-process-per-database design.

## Receipt assessment

**The latest receipt section substantially matches the supplied tests and logs, with its qualifications preserved.** It is not proof of every original requirement without exceptions.

| Task | Assessment |
|---|---|
| **4.1 — confinement** | Supports the guarded Linux probe and TCP-listener check. Guard-installation failure handling is source-reviewed, not directly exercised. Windows navigation/popup confinement remains absent and explicitly owner-accepted; CSP is not equivalent to that guard. |
| **4.2 — frontend contract and reducer** | The sources contain 31 tests: 20 state/validation and 11 skin tests, matching the latest count. The receipt correctly identifies the development backend as a simulation, not recorded-event replay. |
| **4.3 — screens, keyboard and scaling** | The logs contain 52 keyboard checks and 140 scale checks. They support the listed operations, not exhaustive keyboard reachability, overlap detection or readability. The mock’s simulated 150% scaling remains distinct from a real desktop run. |
| **4.4 — engine-driven window** | Both supplied window-call logs contain 17 passing subtests, including the two account workflows, wrong credentials and 14 skin/scale cases. The checks support the described call operations and kernel-confirmed freeze. Transfer-state rendering and call-ID/version ordering remain unverified as disclosed. |
| **4.5 — hostile text** | The literal hostile contact-name checks and zero-image assertions support the exercised rendering paths. The raw-HTML prohibition is a frontend-test source scan, not a lint plugin or CI gate; the receipt correctly distinguishes these. |
| **4.6 — skins** | Token consistency, live XP switching during a call, restart persistence and GNOME System-following have corresponding checks. KDE and Windows desktop-following remain unverified. |
| **4.7 — packaged application** | The supplied AppImage log supports the updated layout-B walkthrough. The receipt accurately distinguishes successful lab automation with `xdotool` from the unsuccessful `agent-desktop` attempt and records the owner’s acceptance. |

The historical tab-layout sections are superseded by the final layout-B section. Evidence remains attributed to its recorded artifact commits, not automatically to every later source revision.

**Task 4.8 remains open**, including the exact Windows executable’s call walkthrough and final artifact verification. This review does not replace those checks or establish passing Debian/Windows audio.

Not checked: builds, test execution, screenshots or omitted implementations. Windows confinement and Debian/Windows audio remain disclosed risks.
Verdict on M2: SOUND