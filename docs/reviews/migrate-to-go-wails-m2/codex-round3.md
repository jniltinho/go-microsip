## Earlier findings

1. **RESOLVED — Linux guard-installation failure now closes the window and fails the command.**  
   In `src/internal/gui/gui.go`, the `WindowRuntimeReady` handler records the failure, displays `errUnguarded` and calls `ui.Quit()`. After `ui.Run()` returns, `Run` returns that error; deferred phone cleanup still runs. Linux declares the guard mandatory, and `probeVerdict` rejects an expected-but-uninstalled guard.

   The ordinary startup failure path is not directly tested. That limits runtime evidence, but the previous fail-open behavior is corrected in source. The explicitly accepted Windows limitation remains separate.

2. **RESOLVED — saving over an unreadable configuration source stores nothing.**  
   `config.Draft` now distinguishes source-loading failure (`loaded == false`) from an incomplete configuration. `Phone.SaveSettings` returns the loading error before constructing or storing edits, including theme-only edits. `Phone.Settings` exposes no account from the empty failed-load draft.

   `TestPhoneSavesNothingOverAnUnreadableConfiguration` checks rejected account/theme saves, empty storage and successful resolution after repairing the file. `TestSavingOverAnUnreadableConfigurationStoresNothing` checks the bound-service path. The missing automatic display of the loading error remains a disclosed usability limitation, not the previous persistence defect.

## New problems

1. **MAJOR — saved DND can appear enabled while the engine accepts incoming calls. Must fix before closure.**  
   **Files/functions:** `src/internal/core/app/phone.go:105–123`, `OpenPhone`; `Phone.Settings` and `Phone.Register`; `src/frontend/src/store.ts:33–38`, `loadSettings`.

   **Scenario:** On an empty profile, enable DND, then quit before configuring an account. On restart, `Resolve` fails because the account is incomplete, so `OpenPhone` never sends `SetDND{On: true}`. Nevertheless, `Settings` reads the saved DND value from the draft, and the page displays DND as enabled. Completing the account and selecting Save and register sends only `Register`; DND remains off in the engine. An incoming call can therefore ring while the window promises otherwise.

   **Suggested fix:** Initialize the engine’s DND setting independently of complete account resolution, using successfully loaded UI settings. Add a regression starting with an incomplete account and saved DND enabled, then completing registration and verifying incoming-call rejection. The existing DND test checks persistence, not restoration in this startup path.

2. **MAJOR — invalid higher-precedence values can silently become live defaults after Save and register. Must fix before closure.**  
   **Files/functions:** `src/internal/core/config/resolve.go:204–213`, `parse`, and `SQLiteEdits`; `src/internal/core/app/phone.go:227–275`, `Phone.Settings` and `Phone.SaveSettings`.

   **Scenario:** A syntactically valid TOML file supplies an otherwise complete account but contains `transport = "UDP"` rather than `"udp"`. Parsing reports an error and substitutes UDP in the draft. Because the sources loaded successfully, `loaded` is true. `Settings` ignores the draft error and returns the fallback account as valid.

   The user types a password and selects Save and register. The proposed UDP transport equals the draft’s fallback, so the comparison finds no override. The subsequent first-save path skips the TOML-held transport and reports success. The page then registers over UDP, although the effective configuration still fails resolution and will fail again after restart.

   **Suggested fix:** Validate the proposed effective configuration against the actual source layers before committing an account save. Do not treat a parser fallback as an unchanged higher-precedence value. Preserve incomplete-account completion and repair of SQLite-owned values, but reject invalid file/environment-held fields. Add a bound-service regression asserting rejection and unchanged storage.

## Receipt assessment

**The detailed wording mostly matches the supplied checks, but it is not proof of every original task requirement.**

| Task | Assessment |
|---|---|
| **4.1** | Accurately distinguishes guarded Linux confinement from the accepted, unverified Windows navigation limitation. Passing guarded runs do not exercise the newly added ordinary-startup guard-failure shutdown path. |
| **4.2** | The current count is correctly stated: 18 reducer/validation tests and 9 skin tests. The receipt honestly identifies a simulator, not the recorded-event backend originally requested. |
| **4.3** | The narrowed keyboard and scaling descriptions match the scripts. They do not establish every action’s keyboard reachability, real 150% desktop scaling, overlap freedom or general readability. The detailed caveats should govern the broader “checked” summary. |
| **4.4** | The current freeze check requires successful stopping and kernel confirmation for every discovered web process. Transfer rendering and phase-order limitations are accurately disclosed. The two configuration/startup cases above are not covered. Also, wrong-password testing is one separate UDP/1002 subtest, not repeated per account as the scenario table’s heading suggests. |
| **4.5** | Correctly describes hostile contact text exercising reachable rendering paths, and the raw-HTML prohibition as a source-scanning test rather than a lint plugin or CI gate. |
| **4.6** | Token consistency, live switching, persistence and GNOME desktop following have corresponding checks. KDE and Windows desktop following remain explicitly unverified. |
| **4.7** | Correctly distinguishes the accepted `xdotool` AppImage walkthrough from the unsuccessful `agent-desktop` attempt. One numerical correction: the AppImage paragraph says 1201 produced **996 of 995** pieces; its linked log says **996 of 996**. |

Task **4.8 remains open**. The Windows log establishes neither the required call walkthrough nor passing audio; its audio check failed.

Not checked: builds, test execution, omitted engine/store/toolkit implementations or screenshots. No source files were modified and no network was used.  
Verdict on M2: SOUND WITH CHANGES