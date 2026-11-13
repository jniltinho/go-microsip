## Earlier findings

1. **RESOLVED — configuration validation precedes the empty-edit return.**

   `Phone.SaveSettings` now validates the proposed effective configuration at `src/internal/core/app/phone.go:339–359`, before returning success for an empty edit at lines 361–363.

   `TestPhoneRefusesAnAccountWhenTheFileHoldsEverything` covers the fully file-held account with invalid uppercase `"UDP"`. The no-write case can no longer bypass validation.

2. **PARTIALLY RESOLVED — bundle-loading failures are blocked, but malformed CA settings can bypass the new check.**

   The original missing/insecure-bundle path is corrected. `OpenPhone` records the loading failure in `Options.Untrusted`; `actor.register` refuses TLS registration with a certificate-failure event before connecting. It passes successfully loaded CA bytes to the endpoint. The two named regressions cover the missing-bundle refusal and a subsequently configured absolute CA path.

   **A default-root path remains reachable.** `Phone.Register` at `src/internal/core/app/phone.go:225–230` discards `Draft`’s validation error. In `config.parse`, an invalid relative `ca_file` becomes the fallback `""`, while `Draft` reports `loaded == true`.

   **Concrete scenario:**
   - Configure a TLS account through the window without a custom bundle, using a typed session password.
   - Unregister, then add `ca_file = "lab-ca.pem"` to the configuration file.
   - Select Menu → Register.

   The cached password avoids `Resolve`. The invalid CA setting produces `draft.CAFile == p.caFile == ""`, so registration is queued with the engine’s existing `CA: nil` default-root configuration. Thus a system-trusted server outside the intended bundle can still be accepted instead of the invalid configuration being refused.

   **MAJOR; must fix before closure.** Reject TLS registration when the current draft has a validation error, rather than comparing its fallback CA value. Add a regression with a relative CA path and a cached or explicitly supplied password. Preserve the valid incomplete-account setup path.

## New problems

1. **MAJOR — saving an account during a call makes the window identify the wrong live account. Must fix before closure.**

   **Files/functions:** `src/frontend/src/components/SettingsView.vue:178–187`; `UiState.dispatch`, `src/frontend/src/state.ts:357–372`; `Phone.Register`; `actor.register`, `src/internal/core/engine/actor.go:166–168`.

   **Scenario:** An account configured through the window is registered and has an active call. Open Settings, change its username/domain and password, and select **Save and register**.

   The settings are persisted and `ui.account` is replaced. `Phone.Register` reports successful enqueueing, so the form closes and the new session password is retained. The actor then refuses the registration with `busy`, preserving the old account and its registered state.

   The status bar consequently shows **Registered** beside the new account, although the engine still uses the old one. Ending the call does not apply the saved account; subsequent calls still use the old identity. The temporary busy notice does not correct this mismatch.

   **Suggested fix:** Refuse account-saving actions during a call before persisting or changing the displayed account, while leaving appearance settings available. Do not treat enqueueing as confirmation of an account switch. Add a regression proving that a refused account change leaves the live identity consistent with the engine.

## Receipt assessment

**The detailed qualifications mostly match the evidence, but the checked tasks are not unconditional proof of their original wording.**

| Task | Assessment |
|---|---|
| **4.1** | The guarded Linux self-test supports the stated probe results and absence of a TCP listener. Windows navigation/popup protection remains explicitly absent and owner-accepted. Ordinary Linux guard-installation failure shutdown is source-reviewed, not directly exercised. |
| **4.2** | The source contains 18 reducer/validation tests and 9 skin tests. The receipt correctly identifies a simulator rather than recorded-event replay. |
| **4.3** | Keyboard and scaling limitations are substantially accurate. However, the keyboard table names **confirmation dialogs**, while `keyboard-check.sh` contains no confirmation-dialog walkthrough. Neither script proves every action’s reachability, overlap freedom, or real 150% desktop scaling. |
| **4.4** | The lab checks support the listed workflows and kernel-confirmed freeze. Transfer-rendering and phase-order limitations are disclosed. “Never Registered” is stronger than the wrong-password test’s final-state assertion. Neither blocking scenario above is covered. |
| **4.5** | Accurately distinguishes hostile contact text from network display names, and a raw-HTML source-scanning test from a lint plugin or CI gate. |
| **4.6** | Token consistency, live switching, persistence and GNOME desktop-following have corresponding checks. Other desktop/platform limitations are disclosed. |
| **4.7** | The supplied AppImage is exercised. The accepted `xdotool` substitution is clearly distinguished from the unsuccessful `agent-desktop` attempt. |

The ten-second audio figures now match their logs. The three-second skin-change row still mixes artifacts: the binary records **296/296 for both accounts**; the AppImage records **296/295 for 1001 and 296/293 for 1201**. Attribute those figures explicitly.

The TLS section’s unconditional bundle-or-refusal claim needs the remaining validation bypass fixed. Task **4.8 remains open**: the Windows self-test is not the required call walkthrough, and the supplied Windows audio check failed.

Not checked: builds, test execution, screenshots or omitted SIP/toolkit implementations. No source files changed and no network was used.  
Verdict on M2: SOUND WITH CHANGES