# Go frontend: screens, skins and the backend contract

Notes of the frontend work done in parallel with the G0 spike of `openspec/changes/migrate-to-go-wails` (owner's decision of 2026-11-11). They record what exists in `frontend/`, how to run and check it, the contract the Go side must implement, and what is still open.

**This is interface evidence only.** Every screen here ran against a mock backend with fictional data. Nothing was wired to the real engine, no call was placed, and no OpenSpec task box was ticked. The evidence below is input for tasks 4.2 to 4.6 and 5.1 to 5.5; each of those also needs its lab-call, packaged-application or Go-side part before it can be checked.

## What was built

| Area | Files | Notes |
| --- | --- | --- |
| Backend contract | `frontend/src/api.ts` | The only module that reaches the backend. Types for the commands and events of `docs/migration/parity-matrix.md` B1/B2, the read queries, and two transports chosen by the Vite mode. |
| Mock engine | `frontend/src/mock.ts` | Imported only by `api.ts`, only in the mock build. Registration, outgoing and incoming calls, hold, mute, DTMF, blind transfer, DND, agent requests, notices and failures, with fictional data (`pbx.invalid`, `192.0.2.1`, extensions 1001 to 1009). |
| Reducer | `frontend/src/state.ts`, `state.test.ts` | Port of `apps/rust-microsip/src/ui/state.rs`: intents go through `dispatch` (at most one command each); registration and call state change only in `applyEvent`. |
| Store | `frontend/src/store.ts` | The reducer made reactive, fed by `subscribe`; calls `ready()` once the page has drawn, also when the backend is missing. |
| Screens | `frontend/src/App.vue`, `src/components/*.vue` | Dialpad, Calls, Contacts, contact editor, in-call panel, incoming call, settings, menu, status bar, agent approval and confirmation dialogs, engine-unavailable and stopped states. |
| Skins | `frontend/src/themes/` | Registry `index.ts`, one CSS file per skin (`light`, `dark`, `xp`, `macos`, `ubuntu`, `win98`) and `system`. Tests in `themes.test.ts`. |
| Checks | `frontend/scripts/prints.sh`, `contrast.js`, `keyboard-check.sh` | Screenshot matrix with a contrast audit of each screen as drawn, and a keyboard walkthrough, both driven with `agent-browser`. |
| Screenshots | `docs/prints/go/<skin>/*.png`, `docs/prints/go/contrast.txt`, `docs/prints/go/window.png` | 25 screens in each of the 6 skins from the mock; `window.png` is the real Wails window of this commit (`make go-prints`). |

Window layout follows MicroSIP: tabs on top with the menu button at the right, the keypad with `Backspace + C` under it, Call and End, the `DND / Mute / Hold / Xfer` row, and the status bar at the bottom with the registration state and the account.

Keyboard: `Ctrl+L` focuses the number, `Enter` calls or answers, `Escape` hangs up, rejects or leaves a view, digits `*` `#` type the number or send DTMF during a call (`A` to `D` too), arrow keys move in tabs, lists and the menu. Dialogs are native `<dialog>` elements: focus is trapped, the safe button has the focus (Deny, Cancel; Answer for an incoming call), and `Escape` cancels.

## Running the mock

All commands run in the build image; no host Node is used.

```sh
# Development server on http://127.0.0.1:5173 (mock backend, hot reload)
GO_DOCKER_FLAGS="-p 127.0.0.1:5173:5173" scripts/go-docker.sh \
  sh -c 'cd frontend && npm ci --no-audit --no-fund && npx vp dev --host 0.0.0.0 --port 5173'

# Checks
scripts/go-docker.sh sh -c 'cd frontend && npx vp check && npx vp test run'

# Builds: the embedded one (Wails transport) and a static mock one (frontend/dist-mock, never embedded)
make go-frontend
scripts/go-docker.sh sh -c 'cd frontend && npx vp build --mode mock'

# With the development server up, from the repository root (agent-browser on the host):
frontend/scripts/keyboard-check.sh
frontend/scripts/prints.sh http://127.0.0.1:5173 docs/prints/go
```

The mock offers its scenarios in the menu ("Simulate (mock engine)") and as `?scene=<id>` in the address: `incoming`, `hostile`, `active`, `held`, `muted`, `transfer-failed`, `approval`, `register-failed`, `audio`, `engine-failed`, `no-engine`. Scripted numbers: `1007` not acceptable, `1008` no answer, `1009` busy (as a transfer target it fails with 486). Registering with the password `wrong` is refused; the server `down.invalid` times out. The status bar shows "Mock" in this build.

Do not run `make go-frontend` (it runs `npm ci`) while the development server is up: it replaces `node_modules` under it.

## Results (2026-11-11)

| Check | Result |
| --- | --- |
| `vp check` (format, lint, type check of `.ts`) | pass, 24 files |
| `vp test run` | pass, 26 tests: 17 reducer and validation tests (ported from Rust, plus agent-request arming and confirmations), 9 skin tests |
| `vp build` and `make go-self-test` | pass: "window opened and the page called the backend" |
| Embedded build free of the mock | pass: no mock chunk and no fictional data in `internal/gui/dist` |
| `frontend/scripts/keyboard-check.sh` | pass, 34 of 34 |
| `frontend/scripts/prints.sh` | 150 screenshots (25 screens x 6 skins); 150 of 150 pass the contrast audit |
| Clipping at 360x560 and at the minimum 320x480 | none in `win98` and `ubuntu` (the widest fonts) on dialpad, in-call, approval, incoming, calls, contacts, menu, settings |

Ported reducer tests: dial validation, DTMF only when established, incoming answer/reject, hold and mute following snapshots, DND and the transfer prompt, sticky engine failure, overload notice, version dedup and final snapshots, redial needing an explicit action, quitting during a call, skin change during a call, settings validation and registration without printing the password, account and dial-target rules, ages and durations. The Rust tests of storage, keyring, CSV import, audio test and the link handler were not ported: that logic belongs to the Go core, not to the page.

Skin tests fail when: fewer or other skins than the six are registered; a skin misses or adds a token (the skin and the token are named); a skin block holds anything but token declarations; `--radius` is not `0` or a source rounds a corner; a hex/`rgb()`/named color or a Tailwind palette class appears outside `src/themes/`; a source uses `v-html`, `innerHTML` or `outerHTML`; a module other than `api.ts` imports the Wails runtime or the mock; a text token is below 4.5:1 on its background; the focus ring is below 3:1.

### Contrast

Two audits, both at 4.5:1 for text:

1. Tokens (`themes.test.ts`): 39 text-on-background pairs per skin, gradients checked at every stop. Lowest ratio per skin: light 5.02, dark 5.02, xp 5.01, macos 4.99, ubuntu 5.03, win98 5.60.
2. Rendered (`contrast.js` in `prints.sh`): every visible text, field value and placeholder of each screenshot against its nearest painted background. 150 of 150 screens pass (`docs/prints/go/contrast.txt`).

Fixed on the way: the call button of `xp` (4.37) and `macos` (4.41) and the hang-up button of `xp` (4.50) were darkened; the secondary text of a selected row took the muted color on the selection color and now takes the selection's text color.

Exempt, as in WCAG 1.4.3: the text of disabled controls, and what lies behind an open modal dialog (inert). The icon colors in the status bar are not text and were not audited.

### Visual refinement pass

- Keypad digits centered in their keys, with the digit column aligned across rows; letters raised to a legible size.
- The selected tab stands two pixels taller than its neighbors.
- `win98`: text had vanished because the font stack named a bitmap face the webview cannot draw; the stack was replaced. Added sunken status-bar panes and etched group boxes.
- `xp`: a Tahoma substitute installed on Linux dropped glyphs at small sizes; DejaVu Sans now leads the stack (Windows, which lacks it, falls through to Tahoma).
- The status bar no longer shows an account when the engine is unavailable.

## The contract the Go side must implement

Source of truth: the exported types and the `wails` function of `frontend/src/api.ts`. JSON field names are the TypeScript ones (camelCase).

### Bound methods on `internal/gui.App`

Called as `github.com/jniltinho/go-microsip/internal/gui.App.<Method>`. A Go error rejects the call with its message.

| Method | Arguments | Returns | Notes |
| --- | --- | --- | --- |
| `Version` | | `string` | exists |
| `Ready` | | | exists; the self-test waits for it |
| `Subscribe` | | | Starts event delivery to this page. The current registration, the current call and the pending approvals are emitted first. |
| `Register` | `AccountSettings`, `password string or null` | | B1.1. `null` means: use the password the backend holds (session or keyring). |
| `Unregister` | | | B1.2 |
| `Dial` | `target string` | | B1.3 |
| `Answer`, `Reject`, `Hangup`, `Hold`, `Resume` | `id number` | | B1.4 to B1.6, B1.8, B1.9 |
| `Dtmf` | `id number`, `key string` | | B1.7; one of `0-9 * # A-D` |
| `Transfer` | `id number`, `target string` | | B1.10 |
| `SetMute` | `id number`, `muted bool` | | B1.11 |
| `SetDnd` | `on bool` | | B1.12 |
| `Shutdown` | | | B1.13; `stopped` follows |
| `Settings` | | `Settings` | `account` (or `null`), `hasPassword`, `theme`, `audioInput`, `audioOutput`, `dnd`. Never a password. |
| `SaveSettings` | `SettingsPatch` | | Any of `account`, `theme`, `audioInput`, `audioOutput`; absent fields stay. |
| `AudioDevices` | | `{inputs, outputs}` of `{id, name, isDefault}` | An error means no device list is available. |
| `Contacts` | `search string` | `Contact[]` | Name or number contains `search`; favorites first. |
| `SaveContact` | `Contact` | `Contact` | `id` 0 adds. |
| `DeleteContact` | `id number` | | |
| `History` | `filter "all"/"incoming"/"outgoing"/"missed"`, `search string` | `HistoryEntry[]` | Newest first. |
| `DeleteCall` | `id number` | | |
| `ClearHistory` | | | |
| `Approve`, `Deny` | `id string` | | Decide one pending agent request. |
| `DenyAll` | | | Deny every pending request and pause new ones. |

**Command refusals.** A command method returns `nil` once the command is queued. When the engine refuses it, the error message must be exactly the notice's name (`busy`, `not_registered`, `unknown_call`, `invalid_state`, `audio_unavailable`, `overloaded`, `engine_failed`); the page shows that notice. Any other error text is shown as `engine_failed`, which is sticky until restart.

### Event

One Wails event, `go-microsip:event`, whose data is one `UiEvent`:

| `type` | Payload | Source |
| --- | --- | --- |
| `registration` | `registration: {state: "disabled"/"registering"/"registered"/"failed", reason?, code?}`; `reason` is `unauthorized`, `timeout`, `certificate`, `network` or `rejected` (with `code`) | B2.1 |
| `call` | `call: {id, direction: "outgoing"/"incoming", remote, phase, endReason?, muted, secure, transfer?, version}`; `phase` is `dialing`, `ringing`, `established`, `held`, `terminating` or `ended`; `endReason` (only with `ended`) is `normal`, `canceled`, `rejected`, `busy`, `no_answer`, `transferred`, `not_acceptable` or `failed`; `transfer` is `{state: "pending"/"succeeded"/"failed", code?}` | B2.2, B2.5 to B2.7 |
| `notice` | `notice`: one of the names above | B2.3 |
| `stopped` | | B2.4 |
| `approvals` | `approvals: [{id, requester, description, deadline}]`, the complete pending list, oldest first; `deadline` in Unix milliseconds | agent control |
| `desktop` | `dark`: the desktop prefers a dark appearance. Sent when the page subscribes and at every change; the System skin is dark when this or the webview's own `prefers-color-scheme` says so. Added in M2: WebKitGTK in a plain GTK window does not pass the desktop's preference on. | the desktop |

Rules the page relies on:

- Events arrive in order; a final call snapshot (`ended`) is never dropped. Snapshots with a `version` not higher than the last one seen for that call are ignored.
- The backend records the history. The page re-queries `History` after a final snapshot and after a delete.
- `remote`, `requester`, `description` and contact fields are shown as text only; sanitizing them is still the backend's job.
- There is no DND event: the page's flag follows a successfully queued `SetDnd` and the value of `Settings` at start (same as the Rust candidate).
- `hasPassword` lets the settings form save without retyping; the page never receives a password.

## Decisions taken here (the owner may change them)

Where two layouts were reasonable, the one closest to MicroSIP was taken:

- **Menu at the top right, status at the bottom**, as in MicroSIP, instead of a menu in the status bar.
- **No title bar inside the page.** The native window already has one; the skins' title chrome (`--title`) dresses dialogs and the Settings and contact-editor headers.
- **In-call panel in place of the number field**, on the Dialpad tab, with the keypad below it as the DTMF pad (as in the Rust candidate). Another tab shows a "Show call" strip.
- **Incoming call as a modal dialog** over any tab, focus on Answer; `Enter` answers and `Escape` rejects (the Rust candidate's keys).
- **Volume sliders are absent.** MicroSIP has them, but the contract has no volume command; Settings lists them under "Not available in this build" together with the audio test, CSV import and agent grants.
- **Settings and the contact editor are views inside the window**, not separate windows.
- **Mock by Vite mode**, not by `.env` files: the repository's `.gitignore` excludes `.env.*`. `vp dev` (development) and `--mode mock` select the mock; every other build selects Wails.
- **Icons are original inline SVG** in `components/Icon.vue`, so the bundle has no third-party icon set and needs no notice.
- **Retro font stacks use system fonts only** (see the refinement pass). Nothing is fetched from the network.

## Open

- **Go side**: done in milestone M2 (`internal/gui/service.go` over `internal/core/app`'s `Phone`): every method of the table and the event. `Approve`, `Deny` and `DenyAll` answer that agent requests are not available until the control channel exists (M4), and the approvals list is always empty. `docs/prints/go/window.png` is the real window on the real engine with an empty profile. Two things differ from the table: the engine also sends the notice `microphone_lost`, which the page now knows; and `Register` answers `invalid_state` when the account is not valid or no password is at hand, which the page checks before it asks.
- **Saved settings and other sources**: `SaveSettings` writes to the database, the lowest source. A field that the configuration file, the environment or a flag holds is refused with an error that names it; the form does not yet show such fields as fixed. A new microphone, speaker or CA bundle applies at the next start: the running engine keeps the ones it started with.
- **The password**: typed in the window, it is kept for the session and never stored; the keyring arrives with M4.
- **Skin persistence in the real window**: the skin is saved through `SaveSettings({theme})` (the `ui.theme` key) and mirrored in the webview's local storage for a flash-free first paint. With no `SaveSettings` yet, only the local storage copy applies; whether that copy survives a restart of the real window was not verified. In the mock, a reload keeps the skin (`keyboard-check.sh`).
- **`internal/archtest/skins_test.go`** (design D7, task 4.6) was not written: `internal/` belongs to the engine session. The same rules are enforced by `frontend/src/themes/themes.test.ts`.
- **Vue templates are not type-checked.** `vp check` type-checks `.ts` files only; checking templates needs `vue-tsc` as a new development dependency (owner's call).
- **No component rendering tests.** The hostile-name case is covered by the source scan (no raw HTML rendering) and by `keyboard-check.sh` in a real browser, not by a unit test; a DOM test environment would be a new dependency.
- **Bundled fonts** (task 5.4): none are bundled. The retro skins depend on system fonts, so `xp` and `win98` draw in DejaVu Sans on Linux.
- **Scaling**: checked by layout (no clipping at the default and minimum sizes). The 150 % and 200 % runs of task 4.3 need the built application on a scaled desktop.
- **Not built** because the contract does not carry them: audio test, volume, CSV contact import, agent grant management, About/licences view, `sip:`/`tel:` link confirmation, tray. They belong to M2 to M4.
- **`docs/themes.md`** (task 5.7) is not written; `frontend/src/themes/index.ts` explains adding a skin in its header.
- **Checks not in CI**: `keyboard-check.sh` and `prints.sh` need `agent-browser` and a running development server; only `vp check` and `vp test` are self-contained.
