## Problems

1. **MAJOR — Incomplete configuration bypasses override refusal. Must fix before closure.**  
   `src/internal/core/app/phone.go`, `Phone.SaveSettings` (255–265); `src/internal/core/config/resolve.go`, `SQLiteEdits` (379–384).

   **Scenario:** A TOML file supplies username `1001` but lacks a domain. The user fills the window’s form with username `1002` and the missing fields. Because `Draft` returned an error, `SaveSettings` passes a nil starting configuration. `SQLiteEdits` silently skips the file-held username instead of rejecting its change. The page then registers `1002`, while subsequent settings resolution uses `1001`.

   **Fix:** Preserve comparison and override checks for fields available in an incomplete draft. Refuse the whole save when a supplied account differs from a higher-precedence value. Test incomplete configurations with both file-held and environment-held fields.

2. **MAJOR — Bound settings methods accept values their configuration reader rejects. Must fix before closure.**  
   `src/internal/core/app/phone.go`, `Phone.SaveSettings` (250–290); `src/internal/core/domain/domain.go`, `AccountSettings.Validate` (147–176).

   **Scenario:** A hostile binding call supplies an otherwise valid account with `expires: 0`, or an audio device ID containing a newline. Account validation does not check expiration, and the audio setters perform no value validation before handing edits to storage. Yet `config.build` rejects expiration outside 1–86400 and device IDs containing control characters. Persisting such edits makes the next `Resolve` fail, preventing automatic registration.

   **Fix:** Apply the configuration reader’s value constraints before persistence, including expiration and device IDs. Reject invalid edits without changing stored settings. Test these inputs through `App.SaveSettings`, not only the configuration parser.

3. **MAJOR — A refused shutdown permanently disables retrying Quit. Must fix before closure.**  
   `src/internal/gui/service.go`, `App.Shutdown` (233–237); `src/frontend/src/state.ts`, `UiState.quitNow` (399–403).

   **Scenario:** The engine’s bounded command queue is full when the user confirms Quit. Both layers set `quitting` before sending, but `Shutdown` returns `overloaded` without queuing shutdown. The page never clears its flag, so subsequent Quit actions return immediately. The call and registration can continue until the user closes the native window or signals the process.

   **Fix:** Either make shutdown reliably reach the engine using the existing close/retry mechanism, or clear both flags on enqueue failure and permit retry. Also handle an already-processed `Stopped` explicitly rather than depending on another event to trigger `quit`. Add rejected-shutdown and stopped-before-shutdown tests.

4. **MAJOR — Failure to install the Linux navigation guard can still produce a successful self-test. Must fix before closure.**  
   `src/internal/gui/gui.go`, `Run` (165–172, 194–206); `src/internal/gui/confine.go`, `probeScript` and `probeVerdict`.

   **Scenario:** Guard setup returns false—for example, its widget search fails or its recovered setup path encounters an error. The normal window continues with only CSP. The self-test omits navigation and popup attempts, and `probeVerdict` accepts the unguarded result. CSP does not prevent top-level `location.href` navigation, so this can certify a Linux window without its required confinement.

   **Fix:** Distinguish an explicitly accepted unsupported platform from Linux guard-installation failure. Require a working guard for Linux startup/confinement verification, and test that attachment failure cannot pass.

   The separately documented, owner-accepted Windows “CSP only” behavior can remain a **recorded limitation**; it is not proof of blocked navigation.

## Receipt assessment

The lab test provides useful integration evidence, but does not establish every checked task in full.

| Task | What the evidence establishes—and its limits |
|---|---|
| **4.1 — confinement** | `window_linux_test.go` does not test confinement. That evidence comes from the separate self-test and guard tests. Those exercise inline script, eval, one external navigation, popup creation and listening TCP sockets, subject to problem 4. They do not comprehensively exercise frames, redirects or scheme handling. Windows navigation confinement is explicitly unverified and absent. |
| **4.3 — screens, keyboard and scaling** | The real-window test establishes keyboard dialing/hangup and selected interactions, plus horizontal overflow/control-bound checks at 100% and 200%. It does not establish every action’s keyboard reachability, actual 150% desktop scaling, overlap detection or general readability. The mock keyboard script also uses programmatic focus and does not exercise the claimed search/favorites workflows. “Every action by keyboard” is stronger than the supplied test: extend it or narrow/explicitly accept that claim before closure. |
| **4.4 — event-driven states and frozen-page audio** | Good coverage exists for registration, connected/incoming calls, mute, hold/resume, successful transfer completion, rejection and wrong credentials, with measured echo audio. However, transfer-pending/success rendering is not asserted; the final transfer snapshot is. The ordering check tracks previously seen phase names, not call IDs, versions or agreement with the latest snapshot. Also, freeze signaling errors are ignored: finding a PID is not proof that it stopped for five seconds. Check signal results and stopped state before claiming the freeze was necessarily exercised. |
| **4.5 — hostile text** | The literal hostile contact name and zero-image assertions support the tested rendering paths. This is not a network display-name test—the receipt correctly explains that distinction. The raw-HTML prohibition is a source-scanning test, not a lint rule or CI gate, also correctly disclosed. That tooling substitution can remain a recorded limitation. |
| **4.6 — skins** | The window test supports live XP switching during a call, audio continuity and persistence on restart. Token consistency comes from separate Go/frontend tests; desktop following comes from separate GNOME VM checks. These claims have corresponding evidence. KDE/Windows desktop-following remains a limitation, not an established property. |
| **4.7 — packaged application** | The wrapper selects the supplied AppImage and enables screenshots, so this is a packaged-window exercise, not merely a mock run. It uses `xdotool`, not `agent-desktop`; the recorded owner acceptance reasonably covers that deviation. The “three calls” assertion only checks text presence, not three distinct rows with their expected direction/outcome. That exact-count claim is stronger than its assertion. |

**Password and event-path assessment:** I found no direct M2 path that persists or returns the typed password. Settings/events exclude it; the trace snapshot does not read input values; tracing requires an explicit flag. In an ordinary untraced run, `Probed` has no callback and cannot activate tracing.

The forwarder and `Subscribe` serialize replay and live emission under the same mutex. `Phone.Close` waits for journaling, and `Run` waits for the forwarder. I found no additional concrete reordering defect there. However, a final snapshot processed before subscription is not replayed, and backend emission is not acknowledgment of JavaScript consumption. Preserve those distinctions rather than claiming unconditional delivery to a closing page.

Task **4.8 remains open**, including the exact Windows executable’s call walkthrough. The supplied Windows log proves neither that walkthrough nor passing audio; its audio check failed.

Not checked: builds, test execution, omitted engine/store/toolkit implementations or screenshots; no network was used.  
Verdict on M2: SOUND WITH CHANGES