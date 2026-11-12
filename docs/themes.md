# Adding a skin

A skin of go-microsip is one stylesheet of design tokens and one entry in a registry. Screens take every color, font, border, corner and control size from the tokens, so a new skin changes no component. This page lists every file a skin touches and the checks that tell when one was forgotten.

The frontend is described in [ui/go-frontend.md](ui/go-frontend.md); builds run in Docker ([DEVELOPMENT.md](DEVELOPMENT.md)).

## What a skin is

- `frontend/src/themes/<id>.css`: one block, `:root[data-theme="<id>"] { ... }`, holding `color-scheme: light` or `dark` and the token declarations, nothing else. The id is lower-case letters and digits.
- Every skin declares the same tokens: no more, no fewer. Copy the skin closest to the one you want and change values. `light.css` is the reference the checks compare with.
- Colors exist only in `frontend/src/themes/`. A color written in a component or in `style.css` fails the frontend test.

Rules the checks enforce on the values:

| Rule | Where |
| --- | --- |
| Text on its background at 7:1 or more; control edges, state icons and the focus ring at 3:1 or more | `themes.test.ts` (token pairs), `prints.sh` (the page as drawn) |
| The focus ring is solid and at least 2 px wide | `themes.test.ts` |
| Corners are rounded only through the radius tokens. A skin not listed in `ROUNDED` sets every radius token and `--scroll-radius` to `0` | `themes.test.ts` |
| `--backdrop-motion` is `none`, or one run of the `backdrop` animation shorter than five seconds. Nothing else in a skin animates | `themes.test.ts` |
| `--font-ui` names a bundled family (`frontend/src/fonts/`) and ends in a generic one. A bundled font is an unmodified open-licensed file with its licence beside it; nothing is fetched: no remote `url()`, no `@import` of a remote sheet | `themes.test.ts`, content security policy, `build/qa/offline-skins.sh` |
| No trademarked logo and no copied proprietary artwork | review |

## Steps

Seven edits, none of them in a screen. `<id>` is the skin's id and `<Label>` its name in Settings.

1. **The stylesheet.** Copy a skin to `frontend/src/themes/<id>.css`, rename the selector to `:root[data-theme="<id>"]`, and change the values.
2. **The import.** Add `@import "./<id>.css";` to `frontend/src/themes/index.css`.
3. **The registry.** Add `{ id: "<id>", label: "<Label>", dark: <true|false> }` to `SKINS` in `frontend/src/themes/index.ts`. Settings lists the registry: the skin appears there with no other change. If the skin rounds its corners, add the id to `ROUNDED` in `frontend/src/themes/themes.test.ts`.
4. **The configuration.** Add a `Theme` constant to `internal/core/config/config.go` and to `Themes()`. Without it the saved value is refused and the next start falls back to System.
5. **The lists the checks keep.** They name the shipped skins on purpose, so that a skin cannot appear or vanish unnoticed:
   - `frontend/src/themes/themes.test.ts`: the list in "the shipped skins are registered";
   - `internal/archtest/skins_test.go`: `shipped`;
   - `internal/core/config/config_test.go`: the count of `Themes()`.
6. **The runs over every skin.** Add the id to the skin lists of `frontend/scripts/prints.sh`, `frontend/scripts/scale-check.sh`, `build/qa/offline-skins.sh` (with the bundled family the skin must load) and `internal/platform/audio/window_linux_test.go`.
7. **The documents.** Add the skin to the table in [ui/go-frontend.md](ui/go-frontend.md) and, for a shipped skin, to the `ui-themes` requirement "Shipped skins" of the OpenSpec change.

A skin that needs a font no other skin uses: follow `frontend/src/fonts/README.md` (the file, its licence, a face in `fonts.css`, a row in the table). The third-party notices pick the licence up at the next build.

## Checks

```bash
make go-frontend-test        # token contract, contrast of the token pairs, corners, motion, no color literal outside themes/
make go-test                 # the registry, the stylesheets and the configuration name the same skins
```

With the development server up (see [ui/go-frontend.md](ui/go-frontend.md)), the page as drawn:

```bash
frontend/scripts/prints.sh http://127.0.0.1:5173 docs/prints/go <id>   # 28 screens, contrast and state audit
frontend/scripts/scale-check.sh                                        # 100, 150 and 200 %
```

`prints.sh` fails when a text is below 7:1, an edge or icon below 3:1, or a call state or error has no icon beside its label. Look at the 28 pictures: the audits do not judge whether the skin looks right.

Then the built window: `make go-offline-skins` starts the binary with no network in every skin, and `make go-window-call` draws each skin in the real webview at 100 and 200 %.

## What the checks say when a step was skipped

| Skipped | Failure |
| --- | --- |
| A token (step 1) | `themes.test.ts`: `skin <id>` with the `missing` or `extra` tokens named; `internal/archtest`: `skin "<id>" lacks the token --...` |
| The import (step 2) | `themes.test.ts`: `index.css imports <id>` |
| The registry (step 3) | `internal/archtest`: the registry and the stylesheets name different skins |
| The configuration (step 4) | `internal/archtest`: the configuration accepts other skins than the stylesheets |
| The lists (step 5) | the tests of step 5 name the skin that is new |

## Removing a skin

Undo the seven steps. A setting that still names the removed skin resolves to System at the next start (`ParseTheme` in Go, `knownSkin` in the page), and `go-microsip doctor` reports the fallback.

## The exercise behind this page

On 2026-11-12 a throwaway skin was added by these steps and removed again, with the checks run in between; the record is in [receipts/go-m3-skins-2026-11-12.md](receipts/go-m3-skins-2026-11-12.md).
