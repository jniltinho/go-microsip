## Earlier findings

1. **RESOLVED — an incoming call between saving and registering no longer changes the displayed live identity.**

   `setRegistration` (`src/internal/core/engine/actor.go:125–135`) names the engine’s account and emits when either that identity or the state changes. `register` refuses an account switch during a call before replacing the account.

   `App.forward`, `Subscribe` and `uiEvent` preserve that identity. `accountText` (`src/frontend/src/state.ts:204–211`) prefers it for every non-disabled registration state, including registering.

   The three named regressions support these changes. I found no remaining supplied production path that substitutes the saved account while displaying **Registered** or **Registering**. This does not imply instantaneous delivery of asynchronous events.

## New problems

1. **MAJOR — startup without an account silently ignores saved audio-device selections. Must fix before closure.**

   **File/function:** `src/internal/core/app/phone.go:116–135`, `OpenPhone`; downstream use in `src/internal/core/engine/actor.go:547–550`, `established`.

   **Scenario:**
   - Before configuring an account, select a non-default headset microphone and speaker in Settings.
   - Quit and restart, still without an account. The saved device IDs load successfully into `draft`, but `Resolve` fails because the account is incomplete.
   - `OpenPhone` copies device IDs only when `cfg != nil`, so the engine starts with empty IDs.
   - Complete the account through the window and make a call.

   The engine calls `Audio.Open("", "")`, which means the system defaults, while Settings continues to show the selected headset. This can transmit the laptop microphone or play a private call through speakers. The promised restart has already happened; completing registration never updates these engine options.

   **Suggested fix:** Initialize valid, successfully loaded audio-device settings independently of account completeness, rather than gating them on full account resolution. Add a regression that saves device selections without an account, restarts, completes registration and verifies which IDs reach `Audio.Open`.

   The existing test saves a microphone before account setup but checks only settings readback—not device use after restart.

## Receipt assessment

**Substantially accurate as a qualified evidence record, but not proof of every original requirement.** The round 6 log mismatch is corrected: the supplied logs now contain **52 keyboard checks** and **140 scale checks**, including Matrix.

| Task | Assessment |
|---|---|
| **4.1** | Supports the described guarded Linux probe and TCP-listener check. Ordinary guard-installation failure shutdown remains source-reviewed, not directly exercised. Windows navigation/popup confinement is explicitly absent and owner-accepted. |
| **4.2** | Sources contain **31 frontend tests: 20 state/validation and 11 skin tests**. The receipt correctly distinguishes the simulated backend from recorded-event replay. |
| **4.3** | Corrected counts match the logs. The detailed caveats remain essential: selected keyboard operations, some seeded focus, simulated 150% scaling, and bounded clipping checks—not proof of every action, overlap freedom or general readability. |
| **4.4** | Both call logs contain 17 passing subtests supporting the described workflows and kernel-confirmed freeze. Transfer-rendering and phase-order limitations are accurately disclosed. They do not cover the audio-selection failure above. |
| **4.5** | Correctly describes hostile contact text and the raw-HTML source-scanning test, rather than claiming a network display-name test or lint-plugin enforcement. |
| **4.6** | Live switching, restart persistence, token consistency and GNOME System-following have corresponding checks. Other desktop/platform limitations remain disclosed. |
| **4.7** | The supplied AppImage log supports the walkthrough. The owner-accepted `xdotool` substitution is clearly distinguished from the unsuccessful `agent-desktop` attempt. |

Task **4.8 remains open**. The Windows self-test is not its required call walkthrough; Debian and Windows audio failures remain disclosed release risks.

Not checked: builds, test execution, screenshots or omitted implementations. No source files were modified and no network was used; the wrong-device startup path blocks closure.

Verdict on M2: SOUND WITH CHANGES