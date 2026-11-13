# The parity gate: what a sign-off would cover

Prepared on 2026-11-13 for the owner. Nothing here is decided. The gate (task 7.4) asks that every row of [the matrix](parity-matrix.md) be proven, or dropped or accepted as a difference with the owner's written approval, on one commit; the cutover that archives the Rust workspace (tasks 7.5 to 7.7), the repository's rename (7.8) and the pre-release (7.10) come after it.

## Where the rows stand

| Status | Rows |
| --- | --- |
| proven | 31 |
| accepted difference (decisions of 2026-11-12) | 24 |
| dropped (same decisions) | 2 |
| partial | 69 |
| nothing shown | 14 |

The 83 rows of the last two lines are what a sign-off has to cover. [The receipt](../receipts/go-m5-parity-2026-11-12.md) ("Task 7.4, 2026-11-13") lists them by what each waits for.

## Five groups, and a recommendation for each

| # | Group | Rows | Choices | Recommendation |
| --- | --- | --- | --- | --- |
| S1 | Needs a real machine or a person: real sound cards and headsets, five minutes per codec, the toast's buttons and the tray, Focus Assist, sign-out during a call, suspend and resume, KDE, a real HiDPI desktop, Windows 10 | 26 | run task 7.2's checklist first; or accept them as "not verified on real hardware" for the pre-release only | **Accept for the pre-release `v0.2.0-rc.1` only**, each named in its limitations; they stay open and are required before a final version. The owner has used the softphone on his own desktop and PBX in the meantime, which is not a recorded check |
| S2 | Needs steps the Windows VM does not have yet: a second user, `mcp` on Windows, where the files land, a link, a failing Credential Manager | 13 | add the steps before the cutover; or accept for the pre-release | **Add the steps before the cutover.** No decision is needed for the work; the cutover would wait for it |
| S3 | Needs the CI and release work itself (tasks 7.5, 7.6, 7.10) | 10 | they cannot be shown before that work exists | **Accept as shown by the tasks that follow**: each of those tasks has its own verification, and the pre-release is not published without them |
| S4 | Two differences with no decision | 2 | see below | see below |
| S5 | More tests in the lab, no decision needed | 32 (24 that need only this, and 8 whose difference is already decided) | go on writing them; or accept the rows as partial for the pre-release | **Go on, and accept what is left as partial for the pre-release.** None of these clauses is known to fail; they are not shown |

The two differences of S4:

| # | Difference | Choices | Recommendation |
| --- | --- | --- | --- |
| S4a | An unknown `ui.theme` resolves to `system` instead of being an error (A63) | accept; or fail as the Rust text says | **Accept.** A setting that names a skin a later version removed should not stop the softphone from starting |
| S4b | A TLS account registers only with a CA bundle named in its settings; the system's certificates are never used, where the Rust application uses them when no bundle is named (B6.13). Found on 2026-11-13; the README said otherwise until then | keep; or use the system's certificates when no bundle is named, and the named bundle alone when one is | **Use the system's certificates when no bundle is named.** It is what the Rust application does and what a PBX with a certificate from a public authority needs; a named bundle would still replace them, so an installation that wants only its own authority keeps that. It is a change to a rule the G0 and M1 work tested, and needs those tests changed with it |

## What a sign-off in these terms would mean

That the cutover goes ahead with 31 rows proven, 26 dropped or accepted, the Windows steps of S2 added, and the rest carried as stated limitations of a pre-release: mostly things nobody has checked on a real machine. It would not say that the softphone was verified on real hardware, and the final version would still need task 7.2.

What is known to be wrong or unsettled on the day this was written, whatever is signed: the runs of the night of 2026-11-13 were made on a machine under heavy load and have to be made again ([receipt](../receipts/go-m5-parity-2026-11-12.md)); the changes since the sixth review round (the owner's four decisions, the TLS classification, the crossing hang-up) have not been read by the independent reviewer; and no real call was placed after the review's changes.
