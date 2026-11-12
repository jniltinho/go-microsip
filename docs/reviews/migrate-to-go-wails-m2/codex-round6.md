## Earlier findings

1. **RESOLVED — TLS registration rejects an invalid configuration.**  
   `Phone.Register` (`src/internal/core/app/phone.go:234–244`) now returns `Draft`’s error before queuing TLS registration. An invalid relative `ca_file` can no longer masquerade as “no bundle”, including with a supplied password. `TestPhoneDoesNotRegisterOverTLSWithoutItsBundle` covers this regression and retains the restart requirement when the configured bundle changes.

2. **PARTIALLY RESOLVED — account changes are blocked during an already-observed call.**  
   `Phone.SaveSettings` and `Phone.Register` now refuse with `busy`; the page’s `saveAccount` action refuses before saving or replacing its account. The named core and frontend regressions cover this case, and appearance changes remain available. However, an incoming call between saving and registering still recreates the identity mismatch described below.

## New problems

1. **MAJOR — an incoming call can interrupt an account switch and leave the wrong identity displayed. Must fix before closure.**

   **Files/functions:** `src/frontend/src/state.ts:359–378`, `UiState.dispatch` / `saveAccount`; `src/internal/core/app/phone.go:213–253,299–307`, `Phone.Register` and `Phone.SaveSettings`.

   **Scenario:**
   - Account A is registered and idle. The user submits account B through Settings.
   - `SaveSettings` passes its idle check and saves B.
   - Before the subsequent `Register` request, an incoming call reaches the still-registered account A.
   - `saveAccount` continues after its `await`, unconditionally assigns B to `this.account`, and requests registration.
   - `Phone.Register` refuses with `busy`. The engine retains A and its registered status, but the header now identifies B.

   Ending the call does not apply B; later calls still use A. This needs only the supported single account, call, window and process.

   **Suggested fix:** Preserve the live identity when registration is refused, and coordinate account switching with authoritative engine acceptance—not merely persistence or command enqueueing. Add a regression that pauses the save response, delivers an incoming-call event, then resumes with registration refused. The current tests start with the call already present and miss this interleaving.

## Receipt assessment

**Mostly accurate with its qualifications, but the final layout-B evidence references need correction.**

| Task | Assessment |
|---|---|
| **4.1** | The Linux probe supports the stated checks. Windows navigation/popup protection remains absent and explicitly owner-accepted. Ordinary Linux guard-installation failure shutdown remains source-reviewed rather than directly exercised. |
| **4.2** | The current sources contain 30 frontend tests: 19 state/validation and 11 skin tests. The receipt correctly identifies a simulator rather than the originally requested recorded-event backend. |
| **4.3** | The final section claims **52/52 keyboard** and **140/140 scale** checks, but its linked logs contain **37** and **120** passing checks, respectively; the scale log has no Matrix runs. Supply the matching post-merge logs or narrow the claim. “Every action” also exceeds the scripts’ coverage: they seed focus in places and omit several actions. Mock zoom is not a real 150% desktop test; the checks do not establish general overlap freedom or readability. |
| **4.4** | Both supplied layout-B call logs contain 17 passing subtests and support the described workflows and kernel-confirmed freeze. Transfer-rendering and phase-order limitations remain correctly disclosed. They do not cover the account-switch race above. |
| **4.5** | Correctly distinguishes hostile contact text from network display names, and the raw-HTML source scan from a lint plugin or CI gate. |
| **4.6** | Seven-skin token checks, live switching, persistence and GNOME desktop following have corresponding checks. Other desktop/platform limitations remain disclosed. |
| **4.7** | The AppImage walkthrough is supported by the supplied log. The owner-accepted `xdotool` substitution is clearly distinguished from the unsuccessful `agent-desktop` attempt. |

Task **4.8 remains open**: the Windows self-test is not the required Windows call walkthrough. Debian and Windows audio failures are disclosed, not passing evidence.

Not checked: builds, test execution, screenshots or omitted dependencies; no source changes or network use. The account-switch race blocks closure; the disclosed audio failures remain release risks.  
Verdict on M2: SOUND WITH CHANGES