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
| Screens | `frontend/src/App.vue`, `src/components/*.vue` | Layout B of `docs/ui/layouts/` (owner's decision of 2026-11-11): the header, Home, Calls, Contacts, Keypad, the call view, the incoming-call sheet, contact editor, settings, menu, agent approval and confirmation dialogs, engine-unavailable and stopped states. |
| Skins | `frontend/src/themes/` | Registry `index.ts`, one CSS file per skin (`light`, `dark`, `xp`, `macos`, `ubuntu`, `win98`, `matrix`) and `system`. Tests in `themes.test.ts`. See "Skins at AAA" below. |
| Checks | `frontend/scripts/prints.sh`, `contrast.js`, `keyboard-check.sh`, `scale-check.sh` | Screenshot matrix with an AAA contrast audit of each screen as drawn, a keyboard walkthrough, and the window at 100, 150 and 200 % and at its minimum size, all driven with `agent-browser`. |
| Screenshots | `docs/prints/go/<skin>/*.png`, `docs/prints/go/contrast.txt`, `docs/prints/go/scale/scale-150-*.png`, `docs/prints/go/before/`, `docs/prints/go/compare/` | 28 screens in each of the 7 skins, and the call at 150 %, from the mock on layout B; `before/` holds four screens of each of the six earlier skins on the tab layout, before the AAA rework, and `compare/` one before-and-after sheet per skin. `window.png`, `window-kde.png`, `appimage/` and `scale/scale-200-*.png` are the real window on the earlier tab layout and skins and are to be taken again. |

Window layout is layout B of [`docs/ui/layouts/`](layouts/README.md), chosen by the owner on 2026-11-11 in place of the MicroSIP-like tabs:

- **Header** on every screen: the account, the registration state (icon and label), the DND toggle and the menu button.
- **Bottom navigation** with four destinations: Home, Calls, Contacts, Keypad.
- **Home**: a search that takes a name or a number, up to four favorites, and the recent calls, each with its own Call button. Enter dials a number as typed, calls the only contact a name matches, and moves to the list when several match.
- **Keypad**: the number with a clear button, twelve round keys, and `+`, Call and Backspace on a fifth row. Call is never disabled; without a number the window says why.
- **Call**: takes the place of the keypad, and the window goes there when a call is dialed or answered (not while a form is open). State chips, the caller, the timer, and Mute, Hold, Keypad and Transfer once the call is connected; Keypad swaps the caller for the DTMF keys. A control that cannot work is not drawn.
- **Incoming call**: a modal sheet at the bottom edge, Reject at the left and Answer at the right with the focus.
- **Calls**: filters as pills (the selected one carries a check mark), the entries under Today, Yesterday or their date.

Differences from the drawing in `docs/ui/layouts/`: the navigation stays visible during a call, so a contact can be looked up for a transfer; the number on the keypad is a bordered field, because it is an input; the registration label is in the text color with the state's color on its icon only; Calls keeps its search and Clear all; the icons keep square caps.

Keyboard: the window opens with the focus in Home's search; `Ctrl+L` goes to the keypad's number, `Enter` calls or answers, `Escape` hangs up, rejects or leaves a view, digits `*` `#` type the number or send DTMF during a call (`A` to `D` too), arrow keys move in the navigation, lists and the menu. Dialogs are native `<dialog>` elements: focus is trapped, the safe button has the focus (Deny, Cancel; Answer for an incoming call), and `Escape` cancels.

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
frontend/scripts/scale-check.sh                 # 100, 150 and 200 % in every skin
frontend/scripts/prints.sh http://127.0.0.1:5173 docs/prints/go
```

The three scripts drive `agent-browser`'s default browser session. Two checkouts running them at once must each name their own (`AGENT_BROWSER_SESSION=<name> frontend/scripts/prints.sh ...`), or they navigate each other's page.

The mock offers its scenarios in the menu ("Simulate (mock engine)") and as `?scene=<id>` in the address: `incoming`, `hostile`, `active`, `held`, `muted`, `transfer-failed`, `approval`, `register-failed`, `audio`, `engine-failed`, `no-engine`. Scripted numbers: `1007` not acceptable, `1008` no answer, `1009` busy (as a transfer target it fails with 486). Registering with the password `wrong` is refused; the server `down.invalid` times out. The status bar shows "Mock" in this build.

Do not run `make go-frontend` (it runs `npm ci`) while the development server is up: it replaces `node_modules` under it.

## Results (2026-11-11)

| Check | Result |
| --- | --- |
| `vp check` (format, lint, type check of `.ts`) | pass, 24 files |
| `vp test run` | pass, 31 tests: 20 reducer and validation tests (ported from Rust, plus agent-request arming and confirmations), 11 skin tests |
| `make go-self-test`, `make go-window-call` | pass on the merged layout and skins (the real window; see the M2 and M3 receipts) |
| `make go-offline-skins` | pass: the built window starts in each of the seven skins in a container with no network and draws the whole home screen; the built page loads nothing from outside itself (`docs/prints/go/offline/`) |
| `frontend/scripts/keyboard-check.sh` | pass, 52 of 52 on layout B (the focus ring is solid and at least 2 px; the Matrix backdrop moves once for under five seconds and not at all with reduced motion) |
| `frontend/scripts/scale-check.sh` | pass, 140 of 140, scroll bars drawn: the call, the contacts, the settings, Home and the keypad at 100, 150 and 200 % in the seven skins, then the call, its DTMF keys, the incoming call, Home and the keypad at the minimum window of 320x480 |
| `frontend/scripts/prints.sh` | 196 screenshots (28 screens x 7 skins), scroll bars drawn; 196 of 196 pass the AAA audit as drawn (`contrast.txt`), and in every skin each of the 12 call-state and error labels stands beside an icon (`states.txt`, 84 of 84): ringing, connected, encrypted, muted, on hold, transfer failed, incoming, registration failed, no audio device, engine stopped |
| Clipping at 360x560 and at the minimum 320x480 | none in the seven skins (the scale check above): nothing wider than the window, no control off its side, no key or caller text cut. At 320x480 the caller's avatar is not drawn and the DTMF keys in a call are wider than tall, so everything fits. |

Ported reducer tests: dial validation, DTMF only when established, incoming answer/reject, hold and mute following snapshots, DND and the transfer prompt, sticky engine failure, overload notice, version dedup and final snapshots, redial needing an explicit action, quitting during a call, skin change during a call, settings validation and registration without printing the password, account and dial-target rules, ages and durations. The Rust tests of storage, keyring, CSV import, audio test and the link handler were not ported: that logic belongs to the Go core, not to the page.

Skin tests fail when: fewer or other skins than the seven are registered; a skin misses or adds a token (the skin and the token are named); a skin block holds anything but token declarations; `win98` or `matrix` has a radius token that is not `0`, or a source rounds a corner other than through a radius token; a hex/`rgb()`/named color or a Tailwind palette class appears outside `src/themes/`; a source uses `v-html`, `innerHTML` or `outerHTML`; a module other than `api.ts` imports the Wails runtime or the mock; a text token is below 7:1 on a background it is drawn on; a control edge, a button's accent or call fill, a state icon, a check mark, the scroll thumb or the focus ring is below 3:1; the focus ring is not solid or is thinner than 2 px; a backdrop animation repeats or lasts five seconds or more, or the reduced-motion rule is missing, or a component animates.

### Contrast (WCAG AAA)

Two audits, both at 7:1 for text and 3:1 for what is not text, in every skin:

1. Tokens (`themes.test.ts`): the text-on-background pairs of each skin as the components draw them (65 where every background is a color), gradients and the backdrop checked at every stop, and 35 pairs that are not text plus the scroll thumb. Lowest ratio per skin, text then non-text: light 7.23 / 4.18, dark 7.13 / 4.29, xp 7.19 / 3.33, macos 7.16 / 3.41, ubuntu 7.32 / 3.31, win98 7.37 / 7.37, matrix 7.16 / 3.40.
2. Rendered (`contrast.js` in `prints.sh`): every visible text, field value and placeholder of each screenshot against its nearest painted background at 7:1; every icon, and the edge of every button, key, call control, field, check box and radio (its border, an opaque line of its bevel, or its face) against what it stands on, at 3:1. 196 of 196 screens pass (`docs/prints/go/contrast.txt`). The audit was checked against weakened tokens: it reports the text, the icon and the edges.

Exempt, as in WCAG 1.4.6 and 1.4.11: the text and edges of disabled controls, and what lies behind an open modal dialog (inert).

### Skins at AAA, closer to the systems, and Matrix (2026-11-11)

The owner asked for the skins at the best quality (AAA), closer to the real systems, and for a Matrix skin. Before the work the six skins had 12 to 27 text pairs below 7:1 each and control edges between 1.2 and 2.8:1 in every skin but `win98`. The work was done on the tab layout and then merged with layout B; what follows describes the merged state.

`light` and `dark` are layout B's own AAA palettes and metrics (they pass the stricter audit of this work as they were), with the tokens added here. The five other skins are the ones reworked here, with layout B's tokens added.

What changed in the token contract (65 tokens became 100; every skin defines all of them):

| Tokens | Why |
| --- | --- |
| `--radius-pill`, `--radius-window`, `--radius-menu`, `--tab-radius`, `--close-radius`, `--scroll-radius` beside `--radius` | Corners per part (design D7): `light`, `dark`, `macos` and `ubuntu` round, `xp` rounds slightly as Luna does, `win98` and `matrix` are square; the test names the skins that may round. |
| `--chip-border`, `--state-*-bg` (seven) | Layout B's state chips: a tint behind the state's color (`light`, `dark`) or an outline on the panel color (the other skins). |
| `--backdrop`, `--backdrop-shift`, `--backdrop-motion` | A layer over the surface (the scan lines and streaks of `matrix`; `none` elsewhere) and its one-time animation. |
| `--status-gap` | The space under the header in the skins whose header is a bar of its own; zero in `light` and `dark`, where the header blends into the window. |
| `--title-shadow`, `--title-direction`, `--title-align`, `--close`, `--close-text` | Title bars: shadowed text (`xp`), a centered title (`macos`, `ubuntu`), the close button at the left (`macos`) and in the system's shape and color. |
| `--tabbar-pad`, `--tab-active-bevel`, `--tab-lift` (`--tab-accent` is gone) | The bottom navigation: plain cells (`light`, `dark`, `macos`, `ubuntu`) or tabs that hang from the pane's edge (`xp`, `win98`, `matrix`). Tabs carry no border; a skin draws their edges with the bevels. |
| `--check`, `--check-mark` | Check boxes and radio buttons, drawn from the tokens instead of by the webview: `--accent` now fills buttons that stay on and carries a label at 7:1, which the system's own check colors (the green check of `xp`, Yaru's orange) cannot. |
| `--group`, `--group-border`, `--group-title` | Group boxes: filled boxes (`macos`, `ubuntu`), blue captions (`xp`). |
| `--menu-bevel` | A menu's shadow apart from a dialog's frame. |
| `--scroll-track`, `--scroll-thumb`, `--scroll-thumb-bevel`, `--scroll-size` | Scroll bars per skin. |

Also changed, without new tokens: a pressed button carries a 3:1 line; a focused menu item carries a ring in the selection's text color; the focus ring is solid, at least 2 px, stands 2 px off the control, and takes the text color of a title bar, the header, the navigation or a selected row when it is drawn there; the call and hang-up buttons, which layout B draws without a border, are held to 3:1 against the window by their fill.

Decisions taken in the merge (the owner may change them): `xp` keeps its slight rounding although layout B's branch kept it square, because the real Luna buttons and window frame are rounded; `matrix` selections are a dim green band instead of inverse video, because layout B fills every avatar with the selection color; the avatars of the five reworked skins are therefore strong fills (navy in `win98`, blue in `xp` and `macos`, burnt orange in `ubuntu`) where `light` and `dark` have pale ones.

Where a system's own colors miss AAA, the skin uses a deeper tone: the Luna title blue and selection, the greens and reds of every call and hang-up button, Yaru's orange selection (a burnt orange; the orange itself stays on focus rings, checks and the tab line), the stock macOS control outlines (as with its "Increase contrast" setting), the Windows 98 title gradient.

Not covered, and why:

- **Target size.** WCAG 2.5.5 (AAA) asks for 44 px targets. Layout B's keys, call controls and navigation are that large; its small buttons (24 to 36 px by skin: the transfer form, the row actions, the settings) are not, and that is the layout's question, not a skin's.
- **Focus ring against the green and red buttons.** One ring color cannot reach 3:1 against the window, a white field and a dark green button at once. The ring is 3:1 against what it is drawn on and against the plain faces, and stands 2 px off every control, so the gap separates it from a colored face.
- **The top-left edge of a Windows 98 button** is the white highlight (1.8:1 on the gray); the dark bottom-right edge (11.6:1) is what the audit counts.
- **The list of a `<select>`** is drawn by the webview and the system, not by the skin.
- **Real windows.** Everything here ran in headless Chromium against the mock. The skins were not looked at in the WebKitGTK window on Linux or the WebView2 window on Windows, where scroll-bar styling (`::-webkit-scrollbar`) and the system fonts may differ. Of the system fonts named by the stacks, this machine has Ubuntu and DejaVu; Tahoma, Segoe UI, the Apple system font and the monospace faces other than DejaVu Sans Mono were not seen.

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
| `quit` | The window's close button on a desktop without a tray, or the tray's Quit: the page runs its own Quit, which asks before it ends a call. A second request within three seconds is not left waiting for the page. | the window |

Rules the page relies on:

- Events arrive in order; a final call snapshot (`ended`) is never dropped. Snapshots with a `version` not higher than the last one seen for that call are ignored.
- The backend records the history. The page re-queries `History` after a final snapshot and after a delete.
- `remote`, `requester`, `description` and contact fields are shown as text only; sanitizing them is still the backend's job.
- There is no DND event: the page's flag follows a successfully queued `SetDnd` and the value of `Settings` at start (same as the Rust candidate).
- `hasPassword` lets the settings form save without retyping; the page never receives a password.

## Decisions taken here (the owner may change them)

Where two layouts were reasonable, the one closest to MicroSIP was taken:

- **Layout B**, the owner's choice of 2026-11-11 among the three proposals of `docs/ui/layouts/`. The three items below that describe the MicroSIP arrangement (menu and status positions, the in-call panel over the keypad, the centered incoming dialog) are replaced by it.
- ~~Menu at the top right, status at the bottom, as in MicroSIP.~~ The account and the registration state are in the header, with DND and the menu.
- **No title bar inside the page.** The native window already has one; the skins' title chrome (`--title`) dresses dialogs and the Settings and contact-editor headers.
- **The call takes the Keypad destination**, with the DTMF keys behind its Keypad control. Another destination shows a "Show call" strip.
- **Incoming call as a modal sheet** at the bottom edge, over any destination, focus on Answer; `Enter` answers and `Escape` rejects (the Rust candidate's keys).
- **Volume sliders are absent.** MicroSIP has them, but the contract has no volume command; Settings lists them under "Not available in this build" together with the audio test, CSV import and agent grants.
- **Settings and the contact editor are views inside the window**, not separate windows.
- **Mock by Vite mode**, not by `.env` files: the repository's `.gitignore` excludes `.env.*`. `vp dev` (development) and `--mode mock` select the mock; every other build selects Wails.
- **Icons are original inline SVG** in `components/Icon.vue`, so the bundle has no third-party icon set and needs no notice.
- **Retro font stacks use system fonts only** (see the refinement pass). Nothing is fetched from the network.

## Open

- **The lab scenario of the real window** (`internal/platform/audio/window_linux_test.go`, `make go-window-call` and `make go-appimage-call`) still drives the tab layout and was not run on layout B. Three of its steps name what no longer exists: it waits for the text `Dialpad` (the destination is `Keypad`), clicks `Show call` after answering (the window now goes to the call by itself, so the strip is not there) and clicks `Xfer` (the control is `Transfer`, a label the transfer form's own button shares once it is open). Until it is updated and run, tasks 4.4, 4.6 and 4.7 stay open.
- **Disabled controls that remain**: Allow in an agent request during its first second, End while the call is ending, Call and Add in a history row's actions, Clear all with an empty history. Their text is exempt from the contrast floors.
- **The menu button is an icon** with the accessible name "Menu"; DND keeps its label.

- **Go side**: done in milestone M2 (`internal/gui/service.go` over `internal/core/app`'s `Phone`): every method of the table and the event. `Approve`, `Deny` and `DenyAll` answer that agent requests are not available until the control channel exists (M4), and the approvals list is always empty. `docs/prints/go/window.png` is the real window on the real engine with an empty profile. Two things differ from the table: the engine also sends the notice `microphone_lost`, which the page now knows; and `Register` answers `invalid_state` when the account is not valid or no password is at hand, which the page checks before it asks.
- **Saved settings and other sources**: `SaveSettings` writes to the database, the lowest source. A field that the configuration file, the environment or a flag holds is refused with an error that names it; the form does not yet show such fields as fixed. A new microphone, speaker or CA bundle applies at the next start: the running engine keeps the ones it started with.
- **The password**: typed in the window, it is kept for the session and never stored; the keyring arrives with M4.
- **Skin persistence in the real window**: the skin is saved through `SaveSettings({theme})` (the `ui.theme` key) and mirrored in the webview's local storage for a flash-free first paint. With no `SaveSettings` yet, only the local storage copy applies; whether that copy survives a restart of the real window was not verified. In the mock, a reload keeps the skin (`keyboard-check.sh`).
- **`internal/archtest/skins_test.go`** (design D7, task 4.6) was not written: `internal/` belongs to the engine session. The same rules are enforced by `frontend/src/themes/themes.test.ts`.
- **Vue templates are not type-checked.** `vp check` type-checks `.ts` files only; checking templates needs `vue-tsc` as a new development dependency (owner's call).
- **No component rendering tests.** The hostile-name case is covered by the source scan (no raw HTML rendering) and by `keyboard-check.sh` in a real browser, not by a unit test; a DOM test environment would be a new dependency.
- **Bundled fonts** (task 5.4): none are bundled, so the third-party notices gain nothing; the icons are original drawings (`Icon.vue`). The skins depend on system fonts: `xp` and `win98` draw in DejaVu Sans on Linux, `matrix` in the first monospace face the system has (DejaVu Sans Mono on the Linux targets). `make go-offline-skins` shows every skin drawn with no network, and the browser's request log of each skin lists the page's own origin only (`docs/prints/go/network.txt`). Whether the task's "bundle fonts" is still wanted is the owner's decision.
- **Scaling**: checked by layout (no clipping at the default and minimum sizes). The 150 % and 200 % runs of task 4.3 need the built application on a scaled desktop.
- **Not built** because the contract does not carry them: audio test, volume, CSV contact import, agent grant management, About/licences view, `sip:`/`tel:` link confirmation. They belong to M2 to M4. The tray needs nothing from the page but the `quit` event: the Go side owns the icon, its menu (Show, Quit) and the close button, which hides the window where the desktop shows tray icons and quits where it does not.
- **[`docs/themes.md`](../themes.md)** (task 5.7) lists the seven edits a skin needs and the checks that catch a forgotten one; it was followed once with a throwaway skin.
- **Unencrypted calls are not in the screenshot matrix**: the mock's account asks for SRTP, so the "Unencrypted (RTP)" badge (the same markup, with the open-lock icon) is audited in no skin from the mock; the lab window run shows it in the real window.
- **Hover in Dark**: a key at rest and under the pointer had the same color (`--key` and `--btn-hover`); `--btn-hover` is now lighter than both a key and a button at rest (`docs/prints/go/compare/dark-key-hover.png`). It stays subtle on purpose: the muted text on it must keep 7:1.
- **Checks not in CI**: `keyboard-check.sh` and `prints.sh` need `agent-browser` and a running development server; only `vp check` and `vp test` are self-contained.
