# Manual checklist (task 7.2)

The checks only a person at a machine can make, as a sheet to fill in. It is task 7.2 of the
[migration change](../../openspec/changes/migrate-to-go-wails/tasks.md) item by item; the task is
checked when a filled copy is recorded as `docs/receipts/manual-checklist-<date>.md`. Nothing here
is done yet: an empty Result is an open item, and a final version needs all of them.

Use the published files, never a local build, and check them first
(`sha256sum --check SHA256SUMS`). Use the lab PBX or a test extension; write no real server,
account, number or password in this sheet.

| | |
| --- | --- |
| Version and commit (`go-microsip version`) | |
| SHA-256 of the AppImage | |
| SHA-256 of the `.exe` | |
| Date and who ran it | |

Result is `pass`, `fail` or `not run`; a `fail` gets a note saying what was seen.

## 1. Linux: the AppImage on a clean system

One column per session: Ubuntu 26.04 (GNOME, Wayland), Debian 13 (GNOME, Wayland) and one KDE
Plasma session.

| Check | Ubuntu 26.04 | Debian 13 | KDE Plasma | Notes |
| --- | --- | --- | --- | --- |
| Starts and registers with no package installed for it | | | | |
| Five minutes of two-way sound, PCMU, UDP with RTP | | | | |
| Five minutes of two-way sound, PCMA, UDP with RTP | | | | |
| Five minutes of two-way sound, TCP with RTP | | | | |
| Five minutes of two-way sound, TLS with SRTP | | | | |
| A headset chosen in the settings is the one the call uses | | | | |
| A device chosen in the window between two calls is used by the second | | | | |
| The ring and the ringback are heard on real speakers | | | | |

## 2. Windows: the `.exe` on a clean system

| Check | Result | Notes |
| --- | --- | --- |
| Starts and registers with nothing installed for it | | |
| Five minutes of two-way sound, PCMU, UDP with RTP | | |
| Five minutes of two-way sound, PCMA, UDP with RTP | | |
| Five minutes of two-way sound, TCP with RTP | | |
| Five minutes of two-way sound, TLS with SRTP | | |
| A headset chosen in the settings is the one the call uses | | |
| A device chosen in the window between two calls is used by the second | | |

## 3. Windows: a machine with a real sound card

| Check | Result | Notes |
| --- | --- | --- |
| Five-minute call within the sound threshold (at most 0.5 % of frames late or lost, no gap over 60 ms) | | |
| The same while the window is frozen for five seconds | | |
| The microphone unplugged during a call: the call stays up and no other microphone is taken | | |
| The buttons of an incoming call's notification answer and reject | | |
| The ring and the ringback are heard on real speakers | | |
| The settings' audio test plays and records | | |
| A CSV file of contacts is imported | | |
| A call whose PBX plays audio before it answers: that audio is heard | | |
| The commands print their output on an attached console (`go-microsip version`, `doctor`) (D2) | | |
| A second Windows user cannot read the control folder or the grant files (G10) | | |
| With the microphone blocked in Windows' privacy settings, the notice names that setting (G7) | | |

## 4. Networks

| Check | Result | Notes |
| --- | --- | --- |
| Behind a home router, registered over UDP: an incoming call still arrives after the line was idle; if not, a keepalive is needed (G6) | | |
| A call on hold for more than fifteen seconds through a VPN that closes a path carrying one side's audio alone: sound returns on resume | | |
