## Earlier findings

1. **RESOLVED — saved do-not-disturb is applied without an account.**

   `OpenPhone` now calls `startup` with the loaded draft independently of account resolution. `startup` sends `SetDND{On: true}` before any registration command.

   `TestSavedDoNotDisturbIsAppliedWithoutAnAccount` covers command selection/order and restarting the same account-less profile. It does not exercise an incoming call’s rejection; that limit is accurately disclosed.

2. **PARTIALLY RESOLVED — invalid file-held values are rejected, except when saving produces no edits.**

   `Phone.Settings` no longer offers the erroneous draft account. The new effective-configuration validation also rejects the minimal file in `TestPhoneRefusesAnAccountOverAnInvalidFileValue`.

   However, `Phone.SaveSettings` returns success at `src/internal/core/app/phone.go:315–316` when `len(edits) == 0`, **before** that validation.

   **Concrete scenario:** Start with a fully specified TOML account, including default-valued `display_name`, `server`, `port`, `media`, `expires`, `password_env`, and `[ui] theme`/`dnd`. Change only `transport` to `"UDP"`. In the window, enter the same account with valid lowercase UDP and a password. Every set field is file-held and agrees with the draft’s fallback, so both `SQLiteEdits` passes produce no writes. Saving reports success; the page then registers using UDP, while the file remains invalid and prevents registration after restart.

   **MAJOR; must fix before closure.** Run the account’s effective-configuration validation before the empty-edits return. Extend the regression with this fully file-held, no-write case.

## New problems

1. **MAJOR — manual registration bypasses a configured CA-bundle loading failure. Must fix before closure.**

   **Files/functions:** `src/internal/core/app/phone.go:106–117`, `OpenPhone`; `Phone.Settings`; `Phone.Register` at lines 193–218; `src/internal/gui/service.go`, `App.Register`.

   **Scenario:** A valid TLS account specifies an absolute `ca_file` that is missing or fails its ownership/permission checks. `OpenPhone` suppresses automatic registration but still starts the engine with `CA: nil`. `Settings` subsequently offers the account and configured password because neither `Draft` nor `Resolve` reads the CA bundle.

   Clicking **Menu → Register** reaches `Phone.Register`, which queues registration without checking the failed trust initialization. Registration therefore uses the engine’s default trust configuration rather than the requested bundle. A system-trusted server outside the intended custom trust roots can be accepted. This contradicts `config.ReadCAFile`’s explicit prohibition on falling back when a configured bundle fails.

   **Suggested fix:** Retain the engine’s trust-initialization result and refuse TLS registration whenever a requested bundle was not loaded. Require successful engine reinitialization/restart rather than merely rechecking the file. Add a bound-service regression proving that manual registration cannot bypass a missing or insecure CA bundle.

## Receipt assessment

**The detailed qualifications mostly match the checks, but the checked tasks are not proof of every original requirement.**

| Task | Assessment |
|---|---|
| **4.1** | Linux’s guarded probe supports the described confinement checks. Windows navigation/popup protection is absent and explicitly accepted as a limitation. Ordinary Linux guard-installation failure shutdown remains source-reviewed, not directly exercised. |
| **4.2** | The count matches: 18 reducer/validation tests and 9 skin tests. The receipt correctly identifies the development backend as a simulator, not recorded-event replay. |
| **4.3** | The detailed keyboard and scaling caveats are accurate. The tests do not prove every action’s keyboard reachability, overlap freedom, general readability, or real 150% desktop scaling. |
| **4.4** | The checks support the listed call workflows and kernel-confirmed five-second freeze. Transfer-state rendering and call-ID/version agreement are correctly excluded. Neither remaining configuration failure above is covered. |
| **4.5** | Correctly distinguishes hostile contact text from a network display name, and the raw-HTML source scan from a lint plugin or CI gate. |
| **4.6** | Token consistency, live switching, persistence and GNOME desktop-following have corresponding checks. Other desktop/platform limitations are disclosed. |
| **4.7** | The supplied AppImage is exercised; the owner-accepted `xdotool` substitution is clearly distinguished from the unsuccessful `agent-desktop` attempt. |

One numerical mismatch remains: receipt line 62 says the binary produced **996 of 996** pieces for both accounts; `go-m2/window-call.log` records **996 of 995** for both. The AppImage figures and separate wrong-password wording now agree with their log.

Task **4.8 remains open**: the Windows self-test is not the required call walkthrough, and its supplied audio check failed.

Not checked: builds, test execution, omitted engine/toolkit implementations or screenshots. No source files were modified and no network was used.  
Verdict on M2: SOUND WITH CHANGES