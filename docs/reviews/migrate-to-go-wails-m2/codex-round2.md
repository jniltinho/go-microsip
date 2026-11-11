## Earlier findings

1. **RESOLVED — incomplete configurations retain override protection.**  
   `src/internal/core/app/phone.go:255–270` now compares the proposed account against the draft before using the first-save path. `SQLiteEdits` refuses changes to higher-precedence fields even when the account is incomplete.  
   `TestPhoneKeepsOverridesOfAnIncompleteConfiguration` covers the file-held username, environment-held transport, unchanged storage after refusal, and successful completion with matching values. A separate draft-loading failure remains below.

2. **RESOLVED — invalid expiration and device edits are rejected before persistence.**  
   `src/internal/core/config/resolve.go:370–381` enforces expiration of 1–86400 seconds and device constraints. `Phone.SaveSettings` also validates device-only patches using `config.ValidDevice`.  
   The core test rejects expiration zero while preserving the previous value. `TestThePageDrivesThePhone` rejects expiration zero and a newline-containing device through the bound service, then checks that neither was saved.

3. **RESOLVED — refused shutdown permits another Quit attempt.**  
   `App.Shutdown` in `src/internal/gui/service.go:233–255` clears its flag on rejection and handles an already-stopped engine immediately. `quitNow` in `src/frontend/src/state.ts:399–405` clears the page’s flag when sending fails.  
   The frontend regression verifies refusal followed by successful retry. The service test covers normal shutdown and another request after stopping. The backend enqueue-rejection/reset path is still not directly tested; that is a coverage gap, not a demonstrated remaining failure.

4. **PARTIALLY RESOLVED — the self-test is corrected, but normal Linux startup still fails open.**  
   Linux declares `guardExpected = true`; unsupported platforms declare false. `probeVerdict` now rejects an expected-but-uninstalled guard, and `TestProbeVerdict` explicitly covers that distinction.

   However, `src/internal/gui/gui.go:168–176` only logs an error when installation fails during ordinary startup. The window continues without navigation protection. CSP does not block top-level navigation, so the original library/widget-discovery failure scenario still leaves an unconfined Linux window.

   **Before closure:** fail window startup when an expected guard cannot attach, or obtain explicit owner acceptance and document this Linux degradation. The recorded Windows exception does not establish acceptance for Linux.

## New problems

1. **MAJOR — saving after a configuration-loading failure can persist an invalid password-source setting. Must fix before closure.**  
   **Files/functions:** `src/internal/core/app/phone.go:255–270`, `Phone.SaveSettings`; `src/internal/core/config/resolve.go:352–356`, `Draft`; `SQLiteEdits`; `src/internal/core/config/config.go:222–223`, `Stored.Value`.

   **Scenario:** The default TOML file contains a syntax error. The window opens without an account, as designed. The user supplies a valid account and password and selects Save and register.

   Here, `Draft` returns an almost-zero `Stored`, not the defaults produced by `build`. `SaveSettings` treats this loading error like an incomplete account and calls `SQLiteEdits` with `before == nil`. That writes every set field—including `account.password_env = ""`, because `Stored.Value` marks that field as set even when empty.

   Even after the malformed default file is removed, the next `Resolve` rejects the empty environment-variable name from SQLite. The save has introduced another configuration failure instead of merely leaving the original file error unresolved.

   **Suggested fix:** Distinguish failure to load configuration sources from a successfully loaded but incomplete account. Reject account saves when those sources could not be loaded, without changing storage. Add a regression through `App.SaveSettings` using malformed TOML and assert that the database remains unchanged.

## Receipt assessment

**Mostly aligned now, but not an unconditional closure proof.**

| Task | Assessment |
|---|---|
| **4.1** | The passing guarded Linux self-test and explicitly unverified Windows navigation are described accurately. The receipt still needs the ordinary Linux guard-installation failure disclosed or fixed, as above. |
| **4.2** | Correctly distinguishes reducer tests from the simulated development backend. That backend is not recorded-event replay, despite the original task wording. |
| **4.3** | The narrower action list and mock-versus-real scaling distinctions are useful and substantially accurate. Some checks still seed focus programmatically—for example, the contact row at `frontend/scripts/keyboard-check.sh:99`—so “keyboard alone” should not imply an uninterrupted keyboard-only walkthrough. These checks do not prove every action, overlap freedom, or real 150% desktop scaling. |
| **4.4** | The current freeze code checks signal success and kernel-reported stopped state for every discovered web process. The receipt correctly limits transfer assertions to the engine’s final snapshot and the page leaving the call, and describes phase-name ordering rather than call-ID/version agreement. |
| **4.5** | Accurately describes hostile contact text exercising the reachable rendering paths, and the raw-HTML prohibition as a source-scanning test rather than a lint plugin or CI gate. |
| **4.6** | Token-contract tests, live switching, restart persistence and GNOME desktop-following have corresponding checks. KDE and Windows desktop-following remain explicitly unverified. |
| **4.7** | The wrapper exercises the supplied AppImage, and the test now counts each expected history direction/outcome label. The accepted `xdotool` substitution is clearly distinguished from the unsuccessful `agent-desktop` attempt. |

The “26 tests” sentence is historical relative to this extract: the current sources declare 18 reducer/validation tests and 9 skin tests. Updated assertions should also have a revision-attributed passing run before their guarantees are attached to earlier measurements.

Task **4.8 remains open**. The Windows self-test and port observations do not establish the required Windows call walkthrough; its supplied audio run failed.

Not checked: builds, test execution, omitted implementations or screenshots. No files were modified and no network was used.

Verdict on M2: SOUND WITH CHANGES