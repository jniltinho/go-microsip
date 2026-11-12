# Review of the window with the owner (task 5.6)

What the owner asked for while looking at the window, what changed, and where the pictures are. Screens from the mock use fictional data (`pbx.invalid`, extensions 1001 to 1006).

| Date | Seen in | The owner's note | What changed | Pictures |
| --- | --- | --- | --- | --- |
| 2026-11-11 | the MicroSIP-like window of the first frontend | Make it more modern, at AAA; show three layouts to choose from | Three layouts drawn; **layout B chosen** (the call and the people first, bottom navigation, the keypad as one destination) and built | before: [`before/`](before/); proposals: [`../../ui/layouts/`](../../ui/layouts/README.md); after: the skin folders here |
| 2026-11-11 | layout B from the mock | Skins at the best quality, closer to the real systems; add a Matrix skin | Seven skins at 7:1 text and 3:1 edges, per-skin corners, the Matrix skin | [`compare/`](compare/) (one picture per skin), [`contrast.txt`](contrast.txt) |
| 2026-11-12 | the built AppImage on the owner's desktop | The account stayed at "Failed: no response" | Not the window: the local address followed the first interface instead of the route (fixed in `internal/core/sip`) | [receipt](../../receipts/go-tray-and-route-2026-11-12.md) |
| 2026-11-12 | the built AppImage on the owner's desktop | The close button should send the window to the tray, as in the Rust application | Tray icon with Show and Quit; the close button hides the window where there is a tray | [receipt](../../receipts/go-tray-and-route-2026-11-12.md) |
| 2026-11-12 | the built AppImage on the owner's desktop, Dark | Buttons do not change color under the pointer | A key at rest and under the pointer had the same color; `--btn-hover` of Dark is now lighter than a key and a button at rest | [`compare/dark-key-hover.png`](compare/dark-key-hover.png): before on the left, after on the right, the pointer on the 5 |

## Focus order and proportions

Not judged by eye alone:

- Focus order and keyboard operation: `frontend/scripts/keyboard-check.sh`, 52 of 52 ([log](../../receipts/go-m3/keyboard-check.log)).
- Proportions of the compact window: `frontend/scripts/scale-check.sh`, 140 of 140: 100, 150 and 200 % in the seven skins and the minimum window of 320x480 ([log](../../receipts/go-m3/scale-check.log)); [`scale/`](scale/) holds the real window at 200 %.

## Open

The owner has not yet said that layout, spacing and proportions of the built window need nothing more. Until then task 5.6 stays unchecked.
