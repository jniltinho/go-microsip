UNSOUND

1. **Major — repeated close bypasses call confirmation.**  
   `internal/gui/service.go:266–279` (`leave`), `frontend/src/state.ts:386–412`  
   A second request within three seconds calls `Shutdown` directly, even when the page displayed the question or the user already pressed Cancel. Closing, cancelling, then closing again quickly ends the call without confirmation. Conversely, a frozen page can ignore requests indefinitely when they arrive more than three seconds apart.  
   **Fix:** track the page’s acknowledgement and cancellation explicitly. Repeated close must not mean consent; make any force-quit action explicit. Test cancellation followed immediately by another close.

2. **Major — a duplicate shutdown can lose the pending quit.**  
   `internal/gui/service.go:234–256`, `service.go:100–109`, `service.go:279`  
   `Shutdown` unconditionally sets `quitting`, then clears it on refusal. Scenario: the first shutdown succeeds; another close arrives after the engine stops but before `forward` receives `Stopped`. That second send returns `engine_failed` and clears `quitting`. When `Stopped` arrives, `forward` no longer calls `quit`, leaving the stopped application open.  
   **Fix:** make shutdown idempotent while pending. A duplicate request must not clear an earlier accepted request. Test with delivery of `Stopped` deliberately delayed.

3. **Major — the selected source address is not coupled to the actual registrar destination.**  
   `internal/core/sip/endpoint.go:106–134`, `endpoint.go:205–211`  
   `bindHost` chooses an address using one resolved peer, but SIP requests still use the hostname. If successive DNS answers select registrars reached through different interfaces, the application binds using registrar A’s route and sends to registrar B. With private LAN/VPN addresses, replies can become unreachable. Every account also uses an `udp4` probe, which need not represent TCP/TLS routing under protocol-specific VPN policies.  
   **Fix:** couple destination selection and local-address selection for the actual transport, retaining the hostname for TLS verification. Add multi-address and multi-interface checks. The loopback unit test establishes neither case.

4. **Major — the registrar’s route is not necessarily the audio route.**  
   `internal/core/sip/endpoint.go:103–105`, `endpoint.go:131–172`  
   The selected address also becomes diago’s media bind/advertised address. A concrete regression is a local SIP proxy at `127.0.0.1` negotiating direct media with another machine: the new selection is loopback, which the remote media peer cannot reach. Similarly, SIP through a VPN does not establish that RTP uses that VPN. Registration can succeed while calls have no audio.  
   **Fix:** separate signalling address selection from media address selection/advertisement. Test a registrar/proxy and negotiated media peer reached through different routes. The owner’s registration-only VPN report does not establish audio correctness.

5. **Major — the “seven skins” scaling evidence can exercise one skin repeatedly.**  
   `frontend/scripts/scale-check.sh:31–35,59–62`; `frontend/src/mock.ts:23,73–75`; `frontend/src/store.ts:42–46`  
   The scaling script changes `go-microsip.skin`, but the mock loads its setting from `go-microsip.mock.theme`. `loadSettings` then overwrites the cached choice. With empty storage, the iterations settle on System; after another script, they can all settle on its last saved skin. Nothing asserts the effective skin. Thus the receipt’s 140 passes do not establish seven-skin scaling, particularly at 150%.  
   **Fix:** set the mock’s authoritative setting too, and assert `dataset.skin` and `dataset.theme` before each measurement. Regenerate the scaling evidence.

6. **Major — the 5.5 audits can pass with missing focus rings and invisible state icons.**  
   `frontend/scripts/contrast.js:43–82`; `frontend/scripts/prints.sh:36–48`; `frontend/src/themes/themes.test.ts:305–326`  
   The rendered contrast audit never examines outlines. Adding `.ctl:focus-visible { outline: none }` leaves the token checks passing; the keyboard check tests a different control. Likewise, `.badge svg { display: none }` still satisfies `state()`: the SVG exists and its parent contains the label. The contrast audit skips the hidden SVG.  
   **Fix:** audit the actual focused controls, including outline width and contrast, and require a visible, nonempty state icon beside the scoped label. Add these negative cases. The receipt’s “196 … focus ring” and “84 … icon and label” claims exceed those assertions.

7. **Minor — font and offline checks do not establish complete rendering.**  
   `frontend/src/themes/themes.test.ts:339–365`; `internal/gui/trace.go:63`; `build/qa/offline-skins.sh:58–60`  
   The unit test checks filenames and a licence filename prefix, not font validity or licence contents. The trace reports any loaded face of a family, not successful loading of every required face. For example, a broken DejaVu bold file can escape detection while regular DejaVu loads and bold text falls back. Four tab names also do not establish that the whole home screen rendered correctly.  
   **Fix:** assert successful loading of the required faces/weights and check their rendered samples. Describe the existing offline result as a startup smoke check, not complete rendering proof.

8. **Minor — the literal-color gate does not reject all literal colors.**  
   `frontend/src/themes/themes.test.ts:134–149`  
   A component containing `style="color: navy"` passes: `navy` is absent from the palette regex. The token-contract tests compare declaration names and do not close this gap. This directly contradicts the claimed enforcement that a component color literal fails the frontend test.  
   **Fix:** validate color-bearing declarations/utilities against the permitted token forms, with negative fixtures for named colors and arbitrary-value utilities.

9. **Minor — unknown-skin reporting disappears when the account is incomplete.**  
   `internal/core/config/doctor.go:43–58`; `internal/core/config/resolve.go:328–333`  
   A profile containing only `[ui] theme = "removed"` starts with System through `Draft`, but `Resolve` fails account validation before constructing `UnknownTheme`. Doctor consequently reports `unknown_theme: false` and omits the fallback warning. This is a normal first-run/settings-only profile, like those used by the offline check.  
   **Fix:** derive the skin warning independently of account validity, while retaining the configuration error. Test an unknown skin without an account.

10. **Minor — the window test’s “unregistered” assertion observes only `Stopped`.**  
    `internal/platform/audio/window_linux_test.go:184–197,875–883`; `internal/core/sip/endpoint.go:319–324`  
    `unregistered()` never observes the PBX binding or an unregister acknowledgement. If the unregister request fails—or is accidentally removed—the engine can still emit `Stopped` and this assertion passes. The tray receipt’s claim that the extension unregisters is therefore stronger than this check establishes.  
    **Fix:** observe the lab PBX binding or the `REGISTER` with zero expiry and its response. Separately test bounded exit when acknowledgement is unavailable.

### Claims checked and supported

| Task | Supported evidence and limits |
|---|---|
| **5.1** | Both contract tests detect individual missing/extra token names; the literal-color gate has finding 8’s gap. |
| **5.2–5.3** | The registry checks require all seven named skins and stylesheets. `prints.sh` schedules 28 captures per skin. Their visual correctness cannot be checked from the omitted pictures. |
| **5.4** | The make target isolates the runtime network; font URLs are local. The recorded network log is explicitly from the mock development server. Complete-rendering limits are in finding 7. |
| **5.5** | Text/control contrast calculations and the twelve state-label checks exist, but do not establish the complete requirement; see finding 6. |
| **5.6** | Owner review notes and approval are recorded. The browser scaling evidence needs correction under finding 5. |
| **5.7** | The guide and recorded throwaway exercise describe adding/removing a skin without screen edits. I did not reproduce that exercise. |

- The Linux skin walkthrough genuinely selects each skin, checks its reported identity, dials, observes connection, and hangs up. It does **not** measure audio in each iteration; the separate active-call switching/audio check selects Windows XP.
- The font records contain copyright/permission notices, full OFL texts, and a full Apache licence in the JetBrains file; the notices script concatenates them. I found no concrete prohibition or missing notice for the **recorded unmodified bundling**. The relevant redistribution conditions permit bundling with retained notices. [OFL](https://openfontlicense.org/open-font-license-official-text/), [Apache 2.0](https://www.apache.org/licenses/LICENSE-2.0).
- The diff does not loosen the CSP or navigation guard. TCP/TLS still take the no-listener branch. IPv6 remains an explicitly documented unsupported case, not support demonstrated by the new route test.
- Leaving 5.8 unchecked accurately reflects the Windows call-walkthrough and automation-method gaps.

Not checked: no application, build, or test was run; omitted pictures, font binaries, and packaged notices were not independently verified. Runtime and artifact claims remain unconfirmed.