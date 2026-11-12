# Parity matrix: Rust candidate to go-microsip

Baseline: the Rust-era delta specs at tag `v0.1.0-rc.3` (commit `a75a601`). This file is the working record for the parity gate of `migrate-to-go-wails` (see its `implementation-migration` spec, "Recorded parity gate").

Rules: a row is **proven** only with runtime evidence linked in the Evidence column (a receipt, a test name plus its run, or a lab report); **dropped** only with the owner's written approval linked there. A review, a build or a screenshot is not call evidence. Empty status means not yet verified. **partial** (used from M1 on) means the behavior has runtime evidence at core or command-line level, and the rest of the row (the window, the control channel, Windows, physical hardware, or a case not exercised yet) is still open; a partial row does not count as proven at the gate.

`add-web-wasm-softphone` is not part of the baseline: nothing of it was implemented in the Rust candidate and it is on hold.

## Part A: behavioral baseline

57 requirements and 83 scenarios in 14 spec files (140 rows).

| # | Change | Capability | Requirement | Scenario | Status | Go evidence |
| --- | --- | --- | --- | --- | --- | --- |
| A1 | add-agent-cli-mcp | agent-cli-mcp | Single owner and local modes | (requirement) | partial | One owner of agent control in a session, by the lock of its runtime directory; clients reach it over its socket and never start it (`internal/control`). The command line and MCP clients are task 6.5. go-microsip has no headless service: a difference for the parity gate. [M4 receipt](../receipts/go-m4-desktop-2026-11-12.md) |
| A2 | add-agent-cli-mcp | agent-cli-mcp | Single owner and local modes | AG-01 multiple clients | partial | `TestAClientReachesTheOwnerOfItsSession` (two clients), `TestOneOwnerServesASessionAndTheNextTakesOver`, `TestNoOwnerIsUnavailableAndNothingIsStarted`; the service cases do not apply |
| A3 | add-agent-cli-mcp | agent-cli-mcp | Structured CLI and real commands | (requirement) |  |  |
| A4 | add-agent-cli-mcp | agent-cli-mcp | Structured CLI and real commands | AG-02 CLI contract |  |  |
| A5 | add-agent-cli-mcp | agent-cli-mcp | Interoperable stdio MCP | (requirement) |  |  |
| A6 | add-agent-cli-mcp | agent-cli-mcp | Interoperable stdio MCP | AG-03 protocol and limits |  |  |
| A7 | add-agent-cli-mcp | agent-cli-mcp | Local authorization and data protection | (requirement) | partial | Mutations wait for a person or need a grant; status names nobody; personal reads need a read grant; capabilities kept as hashes (`internal/control/owner.go`, `grants.go`). The window's grant screen and the grant files are not built (6.5, 6.7) |
| A8 | add-agent-cli-mcp | agent-cli-mcp | Local authorization and data protection | AG-04 deny and grant | partial | `TestDenialAndExpiryHaveNoEffect`, `TestGrantsRunOnlyWhatTheyCoverAndNeverPrompt`, `TestAGrantAnswersOnlyTheCallersItNames`, `TestDenyAllAlsoStopsGrantsAndTheAuditIsCleared`; `make go-window-call` ("agent control"): no approve operation exists, a no in the window sends nothing |
| A9 | add-agent-cli-mcp | agent-cli-mcp | Idempotency, concurrency, audit | (requirement) | partial | The ledger is written before any effect, by epoch and key; an unavailable ledger refuses mutations; audit without digits or whole numbers (`internal/core/store/control.go`, `internal/control/owner.go`) |
| A10 | add-agent-cli-mcp | agent-cli-mcp | Idempotency, concurrency, audit | AG-05 replay and race | partial | `TestAPersonApprovesExactlyOnceAndReplaysNeverRepeat`, `TestKeysFromAnEarlierEpochAreStaleAndNeverRerun`, `TestAKeyPastItsRetentionIsAConflict`, `TestDTMFIsPacedAndDigitsAreNotStored`, `TestControlLedgerKeysEpochsAndFinalStates`; through the command line and MCP in 6.5 |
| A11 | add-agent-cli-mcp | agent-cli-mcp | Proven telephony and independent client shutdown | (requirement) | partial | A client of the control socket places and ends a call on the lab PBX through the built window, with the person's yes; a client that leaves does not end the owner (`make go-window-call`, "agent control"). No real-device voice through agent control |
| A12 | add-agent-cli-mcp | agent-cli-mcp | Proven telephony and independent client shutdown | AG-06 real operation and disconnection | partial | `make go-window-call` and `make go-appimage-call`, subtest "agent control": [M4 receipt](../receipts/go-m4-desktop-2026-11-12.md) |
| A13 | add-windows-port | agent-cli-mcp | Windows control channel | (requirement) |  |  |
| A14 | add-windows-port | agent-cli-mcp | Windows control channel | WIN-10 other user |  |  |
| A15 | add-windows-port | agent-cli-mcp | Windows control channel | WIN-10 approval still required |  |  |
| A16 | add-windows-port | agent-cli-mcp | Windows console executable | (requirement) |  |  |
| A17 | add-windows-port | agent-cli-mcp | Windows console executable | WIN-10 MCP client on Windows |  |  |
| A18 | add-windows-port | audio-media | Windows audio through WASAPI shared mode | (requirement) |  |  |
| A19 | add-windows-port | audio-media | Windows audio through WASAPI shared mode | WIN-08 five-minute call on Windows |  |  |
| A20 | add-windows-port | audio-media | Windows audio through WASAPI shared mode | WIN-08 device renamed or unplugged |  |  |
| A21 | add-windows-port | audio-media | Windows audio through WASAPI shared mode | WIN-08 microphone blocked |  |  |
| A22 | add-windows-port | config-storage | Windows folders and precedence | (requirement) |  |  |
| A23 | add-windows-port | config-storage | Windows folders and precedence | WIN-09 first run |  |  |
| A24 | add-windows-port | config-storage | Windows folders and precedence | WIN-09 cross-OS import |  |  |
| A25 | add-windows-port | config-storage | Windows folders and precedence | WIN-09 invalid folder variable |  |  |
| A26 | add-windows-port | reproducible-distribution | Windows cross build in Docker | (requirement) |  |  |
| A27 | add-windows-port | reproducible-distribution | Windows cross build in Docker | WIN-11 build on a Linux host | partial | `make go-dist` builds the Windows `.exe` and `.zip` in the Go build image; not started on Windows since G0 |
| A28 | add-windows-port | reproducible-distribution | Windows release artifacts | (requirement) |  |  |
| A29 | add-windows-port | reproducible-distribution | Windows release artifacts | WIN-12 per-user MSI |  |  |
| A30 | add-windows-port | reproducible-distribution | Windows release artifacts | WIN-12 scanner on Windows artifacts |  |  |
| A31 | add-windows-port | reproducible-distribution | Windows quality gates | (requirement) |  |  |
| A32 | add-windows-port | reproducible-distribution | Windows quality gates | WIN-13 Windows regression |  |  |
| A33 | add-windows-port | reproducible-distribution | Windows quality gates | WIN-13 Linux release without Windows gates |  |  |
| A34 | add-windows-port | windows-integration | Supported Windows versions without runtimes | (requirement) |  |  |
| A35 | add-windows-port | windows-integration | Supported Windows versions without runtimes | WIN-01 clean machine |  |  |
| A36 | add-windows-port | windows-integration | Single instance and forwarding on Windows | (requirement) |  |  |
| A37 | add-windows-port | windows-integration | Single instance and forwarding on Windows | WIN-02 second launch with a link |  |  |
| A38 | add-windows-port | windows-integration | Single instance and forwarding on Windows | WIN-02 impostor pipe |  |  |
| A39 | add-windows-port | windows-integration | Notifications and tray on Windows | (requirement) |  |  |
| A40 | add-windows-port | windows-integration | Notifications and tray on Windows | WIN-03 answer from a toast |  |  |
| A41 | add-windows-port | windows-integration | Notifications and tray on Windows | WIN-03 Focus Assist on |  |  |
| A42 | add-windows-port | windows-integration | Opt-in links and autostart on Windows | (requirement) |  |  |
| A43 | add-windows-port | windows-integration | Opt-in links and autostart on Windows | WIN-04 malicious link |  |  |
| A44 | add-windows-port | windows-integration | Opt-in links and autostart on Windows | WIN-04 removal |  |  |
| A45 | add-windows-port | windows-integration | Session end and quit on Windows | (requirement) |  |  |
| A46 | add-windows-port | windows-integration | Session end and quit on Windows | WIN-05 sign-out during a call |  |  |
| A47 | add-windows-port | windows-integration | Secrets and private data on Windows | (requirement) |  |  |
| A48 | add-windows-port | windows-integration | Secrets and private data on Windows | WIN-06 another user |  |  |
| A49 | add-windows-port | windows-integration | Secrets and private data on Windows | WIN-06 Credential Manager unavailable |  |  |
| A50 | add-windows-port | windows-integration | Unsafe code confinement | (requirement) |  |  |
| A51 | add-windows-port | windows-integration | Unsafe code confinement | WIN-07 unsafe outside the adapter |  |  |
| A52 | create-native-rust-linux-release | audio-media | RTP and essential codecs | (requirement) | partial | `make go-call SECONDS=300`, [M1 receipt](../receipts/go-m1-engine-2026-11-11.md); `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/echo/*`, `engine/pair/*` |
| A53 | create-native-rust-linux-release | audio-media | RTP and essential codecs | AUD-01 bidirectional voice | partial | `make go-call SECONDS=300`, [M1 receipt](../receipts/go-m1-engine-2026-11-11.md) (virtual sound cards, both directions through the echo); physical hardware open |
| A54 | create-native-rust-linux-release | audio-media | RTP and essential codecs | AUD-02 degraded network and incompatible codec |  |  |
| A55 | create-native-rust-linux-release | audio-media | Devices and hotplug | (requirement) | partial | `make go-call SECONDS=300`, [M1 receipt](../receipts/go-m1-engine-2026-11-11.md): devices chosen by ID, microphone unplugged; choosing a new one in the window is M2 |
| A56 | create-native-rust-linux-release | audio-media | Devices and hotplug | AUD-03 selection and headset removal | partial | same as A55; `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/microphone-lost` |
| A57 | create-native-rust-linux-release | audio-media | Mute and local testing | (requirement) | partial | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/hold-mute/*` (mute); the local audio test is not implemented |
| A58 | create-native-rust-linux-release | audio-media | Mute and local testing | AUD-04 test and mute | partial | same as A57 |
| A59 | create-native-rust-linux-release | audio-media | Secure media without downgrade | (requirement) | partial | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/secure/plain-offer-refused`, `engine/echo/1201/*`, `secure/*` |
| A60 | create-native-rust-linux-release | audio-media | Secure media without downgrade | AUD-05 mandatory SRTP | partial | same as A59; the persistent warning in the window is M2 |
| A61 | create-native-rust-linux-release | audio-media | NAT with explicit limits | (requirement) |  |  |
| A62 | create-native-rust-linux-release | audio-media | NAT with explicit limits | AUD-06 supported topology |  |  |
| A63 | create-native-rust-linux-release | config-storage | Deterministic precedence | (requirement) | partial | `internal/core/config` tests (18), `cmd/cli_test.go`; [M1 receipt](../receipts/go-m1-engine-2026-11-11.md) |
| A64 | create-native-rust-linux-release | config-storage | Deterministic precedence | CFG-01 precedence and provenance | partial | `TestEveryPrecedenceCombinationPicksTheHighestSource`, `doctor` shows each source (`TestDoctorReportsWithoutRevealingThePassword`) |
| A65 | create-native-rust-linux-release | config-storage | Deterministic precedence | CFG-02 invalid file | partial | `TestTOMLSchemaTypesAndUnknownKeysAreRejected`, `TestMalformedFilesNeverEchoSecretLines` |
| A66 | create-native-rust-linux-release | config-storage | Persistence and editing | (requirement) | partial | `TestEditsSkipUnchangedAndRefuseOverriddenFields`, `TestSettingsRoundTripAcrossRestartWithoutSecrets`; the settings screen is M2 |
| A67 | create-native-rust-linux-release | config-storage | Persistence and editing | CFG-03 durable setting | partial | same as A66 |
| A68 | create-native-rust-linux-release | config-storage | Persistence and editing | CFG-04 active external override | partial | `TestEditsSkipUnchangedAndRefuseOverriddenFields`, `TestFirstRunEditsUseLayerSourcesAndSkipOverrides`; shown in the window in M2 |
| A69 | create-native-rust-linux-release | config-storage | Secrets outside the database | (requirement) | partial | `TestSecretsNeverAppearInFormattingErrorsExportsOrDoctor`, `TestSettingsRoundTripAcrossRestartWithoutSecrets`; the keyring adapter is M4 |
| A70 | create-native-rust-linux-release | config-storage | Secrets outside the database | CFG-05 credentials and missing keyring | partial | A typed password is kept in the keyring and nowhere else, and the next start registers by itself; a keyring that is missing, locked or late leaves it for the session: `make go-keyring`, `TestATypedPasswordIsKeptInTheKeyringForTheNextStart`, `make go-window-call` ("the password is remembered"): [M4 receipt](../receipts/go-m4-desktop-2026-11-12.md). Not on a real desktop's keyring; On the default desktops of Ubuntu 26.04 and Debian 13 the session's keyring is missing or locked for a user logged in without a password (reported, no prompt) and stores, replaces and removes a password once started with the login password (`make go-vm`) |
| A71 | create-native-rust-linux-release | config-storage | Secrets outside the database | CFG-06 insecure secret file | partial | `TestWritableOrSecretBearingFilesNeedSafePermissions`, `TestPasswordFileMustBePrivateRegularAndNotASymlink` |
| A72 | create-native-rust-linux-release | config-storage | Migrations and failures preserve data | (requirement) | partial | `internal/core/store` tests (23); [M1 receipt](../receipts/go-m1-engine-2026-11-11.md) |
| A73 | create-native-rust-linux-release | config-storage | Migrations and failures preserve data | CFG-07 interrupted migration | partial | `TestInterruptedMigrationLeavesV1IntactAndRetries`, `TestV1UpgradesWithPrivateBackupAndKeepsData` |
| A74 | create-native-rust-linux-release | config-storage | Migrations and failures preserve data | CFG-08 unavailable or future database | partial | `TestMigrationsAreIdempotentAndFutureSchemaIsRefusedUntouched`, `TestCorruptFileIsReportedAndPreserved`, `TestLockedDatabaseReportsBusyWithoutDataLoss`; what the window shows is M2 |
| A75 | create-native-rust-linux-release | config-storage | Deliberate import and export | (requirement) | partial | `TestConfigImportPreviewsThenAppliesIntoSQLiteOnly`, `TestValidateAndExportUseTheEffectiveConfiguration` |
| A76 | create-native-rust-linux-release | config-storage | Deliberate import and export | CFG-09 import without touching legacy data | partial | `TestImportPreviewIsValidatedSanitizedAndReadOnly`; `internal/core/rustimport` (the Rust profile is only read) |
| A77 | create-native-rust-linux-release | contacts-history | Local contacts and favorites | (requirement) | partial | `TestContactsCRUDFavoritesAndLiteralSearch`, `TestPhoneContacts`; on the real window a contact is added by a person, listed, and named in an incoming call: `make go-window-call`, [M2 receipt](../receipts/go-m2-window-2026-11-11.md) |
| A78 | create-native-rust-linux-release | contacts-history | Local contacts and favorites | CH-01 contact and favorite | partial | same as A77 |
| A79 | create-native-rust-linux-release | contacts-history | Validated atomic CSV | (requirement) | partial | `TestCSVPreviewHandlesQuotesCRLFBOMDuplicatesAndIssues`, `TestCSVImportMergesByNameAndIsAtomic`, `TestCSVLimitsAndHeaderAreEnforced` |
| A80 | create-native-rust-linux-release | contacts-history | Validated atomic CSV | CH-02 invalid and duplicate CSV | partial | same as A79 |
| A81 | create-native-rust-linux-release | contacts-history | Coherent history | (requirement) | partial | `TestHistoryRoundTripDedupFiltersSearchAndRetention`, `TestKilledMidCallIsRecoveredOnceAsInterrupted`, `TestRunJournalsTheCallItPlaced`, `TestPhoneRegistersJournalsAndCloses`; `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `app/run-call/*`; the Calls tab of the real window lists an answered outgoing, an answered incoming and a rejected call: `make go-window-call`, [M2 receipt](../receipts/go-m2-window-2026-11-11.md) |
| A82 | create-native-rust-linux-release | contacts-history | Coherent history | CH-03 outcomes and recovery | partial | same as A81 |
| A83 | create-native-rust-linux-release | contacts-history | Search and privacy | (requirement) | partial | `TestHistoryRoundTripDedupFiltersSearchAndRetention`, `TestContactsCRUDFavoritesAndLiteralSearch` |
| A84 | create-native-rust-linux-release | contacts-history | Search and privacy | CH-04 search and retention | partial | same as A83 |
| A85 | create-native-rust-linux-release | linux-integration | Optional notifications and tray | (requirement) | partial | Notifications: `make go-notify`, and the lab window run refuses a call from the notification. Tray: the lab window run with the test as the tray host (icon registered, close hides, a click shows, the latest calls, a choice calls again, Quit): [M4 receipt](../receipts/go-m4-desktop-2026-11-12.md). Not on a real desktop's services, beyond the owner's report; On the default desktops of Ubuntu 26.04 and Debian 13 the desktop's notification service shows and takes away a call's notification, and Ubuntu's tray host holds the window's icon (`make go-vm`) |
| A86 | create-native-rust-linux-release | linux-integration | Optional notifications and tray | LIN-01 desktop without tray | partial | `make go-window-call`, [tray receipt](../receipts/go-tray-and-route-2026-11-12.md): without a tray host the close button quits and unregisters, asking first during a call; without a notification service nothing is shown and nothing blocks (`make go-notify`); Debian 13's default desktop has no tray host: no icon (`make go-vm`) |
| A87 | create-native-rust-linux-release | linux-integration | Confirmed external URI | (requirement) | partial | The Rust parser ported with its cases (`internal/core/domain/link_test.go`); a link asks before it calls, in the window started with it and in the running one (`make go-window-call`, "links"): [M4 receipt](../receipts/go-m4-desktop-2026-11-12.md). Different from the Rust candidate, which filled the number: here the window asks, and the yes calls |
| A88 | create-native-rust-linux-release | linux-integration | Confirmed external URI | LIN-02 legitimate and malicious links | partial | `TestDialLinksThatAreAccepted`, `TestDialLinksThatAreRefused`, `TestShellMetacharactersInALinkAreRefused`, `TestDialLinkFuzzIsBounded`, `TestALinkArgumentIsCheckedBeforeTheWindow`; a link with parameters ends with the usage error and reaches no window (`make go-window-call`): [M4 receipt](../receipts/go-m4-desktop-2026-11-12.md) |
| A89 | create-native-rust-linux-release | linux-integration | Local single instance | (requirement) | partial | `TestRunIsASingleInstance`, `internal/platform/lock`; a second start hands its link to the running window and ends, one window stays (`make go-window-call`, "links"): [M4 receipt](../receipts/go-m4-desktop-2026-11-12.md) |
| A90 | create-native-rust-linux-release | linux-integration | Local single instance | LIN-03 second instance |  |  |
| A91 | create-native-rust-linux-release | linux-integration | Safe close and quit | (requirement) | partial | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/shutdown/*`; `make go-call` (interrupt: call ended, unregistered, status 130) |
| A92 | create-native-rust-linux-release | linux-integration | Safe close and quit | LIN-04 exit during a call | partial | same as A91; the confirmation in the window is M2 |
| A93 | create-native-rust-linux-release | native-desktop | Native interface and reference layout | (requirement) | partial | The window on the engine: Dialpad, Calls and Contacts tabs, keypad, call controls, settings; a call placed, received, held, muted, transferred and ended from it: `make go-window-call`, `make go-appimage-call`, [M2 receipt](../receipts/go-m2-window-2026-11-11.md). Comparison with MicroSIP is by layout description and screenshots only |
| A94 | create-native-rust-linux-release | native-desktop | Native interface and reference layout | ND-01 comparable layout | partial | same as A93 |
| A95 | create-native-rust-linux-release | native-desktop | Native interface and reference layout | ND-02 feature outside the release | partial | The settings name what this build cannot do instead of offering it: `make go-window-call`, [M2 receipt](../receipts/go-m2-window-2026-11-11.md) |
| A96 | create-native-rust-linux-release | native-desktop | Persistent themes | (requirement) | partial | Seven skins and System; the skin changes during a lab call without touching it and is restored at the next start (`make go-window-call`); `TestSkinsShareOneTokenContract`; an unknown saved skin starts as System and `doctor` says so (`internal/core/config`): [M2 receipt](../receipts/go-m2-window-2026-11-11.md), [M3 receipt](../receipts/go-m3-skins-2026-11-12.md) |
| A97 | create-native-rust-linux-release | native-desktop | Persistent themes | ND-03 theme switching | partial | same as A96 |
| A98 | create-native-rust-linux-release | native-desktop | Persistent themes | ND-04 system theme | partial | System follows the desktop to dark and back in under 400 ms on Ubuntu 26.04 and Debian 13 (GNOME): `make go-vm`, [M2 receipt](../receipts/go-m2-window-2026-11-11.md). Not verified on KDE or Windows |
| A99 | create-native-rust-linux-release | native-desktop | Keyboard operation and scaling | (requirement) | partial | Keyboard: 52 checks on the mock with `agent-browser` and a keyboard-only call on the real window. Scale: 100, 150 and 200 % in seven skins on the mock (140 checks), 100 and 200 % on the real window: [M2 receipt](../receipts/go-m2-window-2026-11-11.md), [M3 receipt](../receipts/go-m3-skins-2026-11-12.md) |
| A100 | create-native-rust-linux-release | native-desktop | Keyboard operation and scaling | ND-05 keyboard and HiDPI | partial | same as A99; 150 % was not run on a real desktop |
| A101 | create-native-rust-linux-release | native-desktop | Visible states and failures | (requirement) | partial | Every state on the page is compared with what the engine sent, in order, through a lab call: `make go-window-call`, [M2 receipt](../receipts/go-m2-window-2026-11-11.md) |
| A102 | create-native-rust-linux-release | native-desktop | Visible states and failures | ND-06 registration failure | partial | A password the PBX refuses: the engine reports unauthorized and the window shows "Failed: wrong credentials": `make go-window-call`, [M2 receipt](../receipts/go-m2-window-2026-11-11.md) |
| A103 | create-native-rust-linux-release | reproducible-distribution | Executable with embedded resources | (requirement) |  |  |
| A104 | create-native-rust-linux-release | reproducible-distribution | Executable with embedded resources | DIST-01 clean machine |  |  |
| A105 | create-native-rust-linux-release | reproducible-distribution | Additional AppImage does not hide dependencies | (requirement) |  |  |
| A106 | create-native-rust-linux-release | reproducible-distribution | Additional AppImage does not hide dependencies | DIST-02 separate ELF and AppImage |  |  |
| A107 | create-native-rust-linux-release | reproducible-distribution | Docker build without host toolchain | (requirement) | partial | every `make go-*` target runs in the build image ([M1 receipt](../receipts/go-m1-engine-2026-11-11.md)); a clean-clone build is not run yet |
| A108 | create-native-rust-linux-release | reproducible-distribution | Docker build without host toolchain | DIST-03 clean-clone build |  |  |
| A109 | create-native-rust-linux-release | reproducible-distribution | Disposable Asterisk lab | (requirement) | partial | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json) (66 scenarios), `make go-call` |
| A110 | create-native-rust-linux-release | reproducible-distribution | Disposable Asterisk lab | DIST-04 lab and isolation | partial | same as A109: fictional accounts and a new CA per run, internal network, PBX torn down |
| A111 | create-native-rust-linux-release | reproducible-distribution | Disposable Asterisk lab | DIST-05 manual desktop profile |  |  |
| A112 | create-native-rust-linux-release | reproducible-distribution | Real gates and independent review | (requirement) |  |  |
| A113 | create-native-rust-linux-release | reproducible-distribution | Real gates and independent review | DIST-06 incomplete release blocked |  |  |
| A114 | create-native-rust-linux-release | reproducible-distribution | Traceable, private-by-default publication | (requirement) |  |  |
| A115 | create-native-rust-linux-release | reproducible-distribution | Traceable, private-by-default publication | DIST-07 pre-push audit |  |  |
| A116 | create-native-rust-linux-release | sip-telephony | Registration and transports | (requirement) | partial | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `register/*`, `engine/register/*`; `TestRegistrationRecovers` |
| A117 | create-native-rust-linux-release | sip-telephony | Registration and transports | SIP-01 registration and renewal | partial | same as A116 |
| A118 | create-native-rust-linux-release | sip-telephony | Registration and transports | SIP-02 invalid configuration and authentication | partial | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `register/wrong-password-refused`, `engine/register/wrong-password`; configuration errors: A65 |
| A119 | create-native-rust-linux-release | sip-telephony | Call lifecycle | (requirement) | partial | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/pair/*`, `engine/echo/*`, `pair/*` |
| A120 | create-native-rust-linux-release | sip-telephony | Call lifecycle | SIP-03 calls in both directions | partial | same as A119 |
| A121 | create-native-rust-linux-release | sip-telephony | Call lifecycle | SIP-04 cancellation and answer race | partial | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/cancel`; the answer race is covered in the actor (`Terminating` as the answer arrives), not by a lab scenario |
| A122 | create-native-rust-linux-release | sip-telephony | Call lifecycle | SIP-05 rejection and second call | partial | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/reject`, `engine/busy` |
| A123 | create-native-rust-linux-release | sip-telephony | Call controls | (requirement) | partial | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/dtmf/*`, `engine/hold-mute/*`, `engine/dnd` |
| A124 | create-native-rust-linux-release | sip-telephony | Call controls | SIP-06 DTMF and hold | partial | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/dtmf/*`, `engine/hold-mute/*` |
| A125 | create-native-rust-linux-release | sip-telephony | Call controls | SIP-07 do not disturb | partial | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/dnd` |
| A126 | create-native-rust-linux-release | sip-telephony | Confirmed blind transfer | (requirement) | partial | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/transfer/*` (success); a failed transfer keeps the call in the actor, without a lab scenario yet |
| A127 | create-native-rust-linux-release | sip-telephony | Confirmed blind transfer | SIP-08 transfer and failure | partial | same as A126 |
| A128 | create-native-rust-linux-release | sip-telephony | Signaling security and recovery | (requirement) | partial | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `tls/*`, `secure/*`, `peer/sip-foreign-source-dropped`, `reregister/*` |
| A129 | create-native-rust-linux-release | sip-telephony | Signaling security and recovery | SIP-09 invalid certificate | partial | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `tls/unknown-ca-refused`, `tls/wrong-host-refused`, `engine/register/unknown-ca`; `TestBundleReplacesSystemRoots` |
| A130 | create-native-rust-linux-release | sip-telephony | Signaling security and recovery | SIP-10 network and suspend | partial | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `reregister/*`; `TestRegistrationRecovers`; suspend and a network change during a call are not exercised |
| A131 | refine-native-ui | native-ui-refinement | Square-cornered visual language | (requirement) | partial | Changed by the owner's choice of layout B on 2026-11-11 ([layouts](../ui/layouts/README.md), design D7): corners come only from the radius tokens, Windows 98 and Matrix stay square, the other skins round (`themes.test.ts` in `make go-frontend-test`). Whether the square-cornered baseline is dropped is for the owner to record at the parity gate |
| A132 | refine-native-ui | native-ui-refinement | Square-cornered visual language | UIR-01 no rounded corners | partial | same as A131 |
| A133 | refine-native-ui | native-ui-refinement | Square-cornered visual language | UIR-02 token contrast | partial | Token and rendered contrast audits at 7:1 for text and 3:1 for edges, icons and the focus ring in seven skins (`themes.test.ts`, `docs/prints/go/contrast.txt`: 196 of 196 screens); from the mock: [M3 receipt](../receipts/go-m3-skins-2026-11-12.md) |
| A134 | refine-native-ui | native-ui-refinement | Minimalist icons with labels | (requirement) |  |  |
| A135 | refine-native-ui | native-ui-refinement | Minimalist icons with labels | UIR-03 icons present and accessible | partial | Call states are an icon and a label in every screen (`CallPanel.vue`, `StatusBar.vue`); seen on the real window in `docs/prints/go/appimage/`. No accessibility-tree check: the page is not on the accessibility bus on the owner's desktop ([M2 receipt](../receipts/go-m2-window-2026-11-11.md)) |
| A136 | refine-native-ui | native-ui-refinement | MicroSIP-compatible refined layout | (requirement) |  |  |
| A137 | refine-native-ui | native-ui-refinement | MicroSIP-compatible refined layout | UIR-04 comparable to MicroSIP |  |  |
| A138 | refine-native-ui | native-ui-refinement | MicroSIP-compatible refined layout | UIR-05 call security is always visible | partial | The call panel and the incoming-call dialog show "Encrypted (SRTP)" or "Unencrypted (RTP)" as the engine reports it, checked for a plain and a secure account: `make go-window-call`, [M2 receipt](../receipts/go-m2-window-2026-11-11.md) |
| A139 | refine-native-ui | native-ui-refinement | Visual regression and scaling | (requirement) | partial | Screenshot matrix from the mock (`docs/prints/go/<skin>/`, 28 screens in seven skins), of the real window (`docs/prints/go/appimage/`, `docs/prints/go/offline/`) and at scale (`docs/prints/go/scale/`); no automated image comparison |
| A140 | refine-native-ui | native-ui-refinement | Visual regression and scaling | UIR-06 snapshots and scale | partial | same as A139 |

## Part B: code-surface inventory

Extracted on 2026-11-11 from a `codebase-memory` index of the repository (project generation `2026-11-11T11:38:46Z`, index mode full) and then read from source. Paths are repository-relative. A row is closed when the Go counterpart exists and its evidence is linked; a deliberate difference is recorded in the Go column and needs the owner's acceptance in the parity receipt.

### B1. Commands (front end to engine) — `crates/core/src/lib.rs:338-365`

| # | Rust item | Notes | Go counterpart | Evidence |
| --- | --- | --- | --- | --- |
| B1.1 | `Command::Register(AccountSettings, SecretString)` | password redacted from `Debug` | `domain.Register{Settings, Password}` | `internal/core/domain` tests; `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/*` |
| B1.2 | `Command::Unregister` |  | `domain.Unregister` | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/unregister` |
| B1.3 | `Command::Dial(DialTarget)` | target 1..64 of alnum `*#+-_.`, `+` only first | `domain.Dial{Target}`, `domain.ParseDialTarget` | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/echo/*` |
| B1.4 | `Command::Answer(CallId)` |  | `domain.Answer{Call}` | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/pair/*` |
| B1.5 | `Command::Reject(CallId)` | 486 Busy Here | `domain.Reject{Call}` | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/reject` |
| B1.6 | `Command::Hangup(CallId)` | cancel, reject or hang up | `domain.Hangup{Call}` | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/cancel`, `engine/echo/*` |
| B1.7 | `Command::Dtmf(CallId, DtmfKey)` | RFC 4733; `0-9 * # A-D` | `domain.SendDTMF{Call, Key}` | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/dtmf/*` |
| B1.8 | `Command::Hold(CallId)` |  | `domain.Hold{Call}` | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/hold-mute/*` |
| B1.9 | `Command::Resume(CallId)` |  | `domain.Resume{Call}` | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/hold-mute/*` |
| B1.10 | `Command::Transfer(CallId, DialTarget)` | blind transfer | `domain.Transfer{Call, Target}` | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/transfer/*` |
| B1.11 | `Command::SetMute(CallId, bool)` |  | `domain.SetMute{Call, Muted}` | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/hold-mute/*` |
| B1.12 | `Command::SetDnd(bool)` | new incoming calls get 486 | `domain.SetDND{On}` | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/dnd` |
| B1.13 | `Command::Shutdown` | bounded stop | `domain.Shutdown` | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/shutdown/*` |
| B1.14 | `AccountSettings` + `validate` (`lib.rs:68-136`) | display_name, username, auth_username, domain, server, port, transport, media, expires; SRTP requires TLS | `domain.AccountSettings.Validate` | `internal/core/domain` tests |
| B1.15 | `Transport` udp/tcp/tls, `MediaSecurity` PlainRtp/Srtp, `ValidationError` (5 variants) | default ports 5060/5060/5061 | `domain.Transport`, `domain.MediaSecurity`, `domain.ValidationError` | `internal/core/domain` tests |

### B2. Events (engine to front end) — `crates/core/src/lib.rs:209-444`

| # | Rust item | Notes | Go counterpart | Evidence |
| --- | --- | --- | --- | --- |
| B2.1 | `Event::Registration(RegistrationState)` | Disabled, Registering, Registered, Failed(Unauthorized/Timeout/Certificate/Network/Rejected(code)) | `domain.RegistrationChanged{State}` | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/register/*`; `TestRegistrationRecovers` |
| B2.2 | `Event::Call(CallSnapshot)` | id, direction, remote, phase, muted, secure, transfer, version | `domain.CallChanged{Call}` | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/*` |
| B2.3 | `Event::Notice(Notice)` | Busy, NotRegistered, UnknownCall, InvalidState, AudioUnavailable, Overloaded, EngineFailed | `domain.Noticed{Notice}`; adds `microphone_lost` | `TestRefusals`; `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/microphone-lost` |
| B2.4 | `Event::Stopped` | always delivered after `Shutdown` | `domain.Stopped` | `TestOutboxKeepsWhatIsFinal`; `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/shutdown/*` |
| B2.5 | `CallPhase` + `can_transition_to` (`lib.rs:271-298`) | Dialing, Ringing, Established, Held, Terminating, Ended(EndReason) | `domain.CallPhase.CanTransitionTo` | `internal/core/domain` tests |
| B2.6 | `EndReason` | Normal, Canceled, Rejected, Busy, NoAnswer, Transferred, NotAcceptable, Failed | `domain.EndReason` | `internal/core/domain` tests; `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/*` |
| B2.7 | `TransferState` | Pending, Succeeded, Failed(code) | `domain.TransferState` | `make go-lab`, [report](../receipts/go-m1/lab-report-engine.json): `engine/transfer/*` |
| B2.8 | `MediaIo` trait (`lib.rs:424-444`) | capture/play 160-sample frames, start, stop |  |  |

### B3. Command line — `crates/config/src/cli.rs`, `apps/rust-microsip/src/{main,cli,service}.rs`

| # | Rust item | Notes | Go counterpart | Evidence |
| --- | --- | --- | --- | --- |
| B3.1 | no command: open the window | exit codes 0/1/2/4, 130/143 on signals |  |  |
| B3.2 | positional `sip:`/`tel:` link | fills the number, never dials; parsing rules in `crates/platform/src/uri.rs:48-77` | `domain.ParseDialLink`; the window asks before it calls, and calls on the yes (never by itself) | `internal/core/domain/link_test.go`, `TestALinkArgumentIsCheckedBeforeTheWindow` |
| B3.3 | `--config PATH`, `--env-file PATH` | `=` form accepted for these two only | `--config`, `--env-file` on every command | `cmd/cli_test.go`, `build/qa/headless.sh` |
| B3.4 | `--doctor` [`--json`] | sanitized report, exit 0 even when invalid | `doctor [--json]`; adds build, display and sound | `TestDoctorReportsWithoutRevealingThePassword` |
| B3.5 | `-h/--help`, `-V/--version` |  | `--help`, `--version`, `version` | `TestHelpAndVersionNeedNoConfiguration` |
| B3.6 | `config validate` | exit 0/1 | `config validate` | `TestValidateAndExportUseTheEffectiveConfiguration` |
| B3.7 | `config export` | TOML without secrets | `config export` | `TestValidateAndExportUseTheEffectiveConfiguration`, `TestExportIsOrderedQuotedAndRoundTrips` |
| B3.8 | `config import FILE` [`--apply`] | preview, then one-transaction replace | `config import FILE [--apply]` | `TestConfigImportPreviewsThenAppliesIntoSQLiteOnly`, `TestConfigImportRejectsInvalidFiles` |
| B3.9 | password guard | any `-…pass…` option is refused, exit 2, arguments never echoed | `app.CheckArguments` before Cobra | `TestPasswordLikeArgumentsAreRefused`, `TestInvalidArgumentsAreUsageErrorsWithoutEcho`, `build/qa/headless.sh` |
| B3.10 | `status`, `operation get ID`, `devices list`, `config show` | control reads |  |  |
| B3.11 | `contacts search [TEXT]`, `history list` | `--limit`, `--cursor`, `--filter all/incoming/outgoing/missed` |  |  |
| B3.12 | `call dial TARGET\|-` | `-` reads one line from stdin |  |  |
| B3.13 | `call answer/reject/hangup CALL_ID VERSION` |  |  |  |
| B3.14 | `call mute/unmute/hold/resume CALL_ID VERSION` |  |  |  |
| B3.15 | `call dtmf CALL_ID VERSION DIGITS\|-`, `call transfer CALL_ID VERSION TARGET\|-` |  |  |  |
| B3.16 | `dnd on\|off` |  |  |  |
| B3.17 | control options `--json`, `--key`, `--grant-file FILE` | grant file: private, at most 512 bytes |  |  |
| B3.18 | control exit codes | 0 done/accepted, 2 invalid, 3 not authorized or awaiting approval, 4 unavailable, 5 timeout, 1 internal |  |  |
| B3.19 | `mcp --stdio` [`--grant-file FILE`] | exit 0/1/2 |  |  |
| B3.20 | `service --grant-file FILE [--config] [--env-file]` | headless owner; exit 0/1/2/3/4 | not built: go-microsip has no headless service; the owner is the window. A difference for the parity gate (7.4) |  |
| B3.21 | second binary `rust-microsip-cli` | same commands, console subsystem on Windows |  |  |

### B4. MCP tools — `apps/rust-microsip/src/mcp.rs:77-223`

| # | Tool | Kind and authorization | Go counterpart | Evidence |
| --- | --- | --- | --- | --- |
| B4.1 | `status_get` | read, no grant |  |  |
| B4.2 | `operation_get` | read, no grant |  |  |
| B4.3 | `devices_list` | read, `read:devices` |  |  |
| B4.4 | `contacts_search` | read, `read:contacts` |  |  |
| B4.5 | `history_list` | read, `read:history` |  |  |
| B4.6 | `config_show` | read, `read:config` |  |  |
| B4.7 | `call_dial` | effect; approval or `control:dial` with the exact destination and quota |  |  |
| B4.8 | `call_answer` | effect; approval or `control:answer` with the caller listed and quota |  |  |
| B4.9 | `call_reject` | effect; approval or `control:reject` |  |  |
| B4.10 | `call_hangup` | effect; approval or `control:hangup` |  |  |
| B4.11 | `call_dtmf` | effect; approval or `control:dtmf` |  |  |
| B4.12 | `call_mute` | effect; approval or `control:mute` |  |  |
| B4.13 | `call_hold` | effect; approval or `control:hold` |  |  |
| B4.14 | `call_transfer` | effect; approval or `control:transfer` with the exact destination |  |  |
| B4.15 | `dnd_set` | effect; approval or `control:dnd` |  |  |
| B4.16 | server limits | 1 MiB input line, 10 tool calls per second, strict schemas |  |  |

### B5. Local control protocol — `crates/control/src/lib.rs`, `crates/platform/src/control.rs`

| # | Rust item | Notes | Go counterpart | Evidence |
| --- | --- | --- | --- | --- |
| B5.1 | `Hello`/`Welcome`, `OwnerMode` gui/service, epoch | protocol name and schema version 1 | `control.Hello`, `Welcome`, `ModeWindow`; protocol name `go-microsip-control`, schema version 1; no service mode | `TestTheOtherApplicationIsNotAnswered`, `TestAClientReachesTheOwnerOfItsSession` |
| B5.2 | `Request` envelope and `validate` | request id, idempotency key required for mutations, capability redacted | `control.Request`, `Validate`; `String` redacts the capability | `TestValidationBoundsInputsWithoutEchoingThem`, `TestUnknownFieldsAndOperationsAreRejected` |
| B5.3 | 15 operations | the same set as the MCP tools (B4.1-B4.15) | `control.Operation`, the same 15 names and arguments | `TestRequestsRoundTripWithTheDocumentedShape` |
| B5.4 | `Response`, `Status` ok/accepted/approval_required/error |  | `control.Response`, `Status` | `TestExitCodesFollowTheCLIContract` |
| B5.5 | 18 error codes with exit codes and retryable flag | validation, grant_required, approval_denied, approval_expired, grant_expired, engine_unavailable, engine_owned_by_service, timeout, conflict, stale_epoch, result_expired, not_found, rate_limited, busy, storage_unavailable, invalid_state, unsupported, internal | `control.Code`, `ExitCode`, `Retryable`; `engine_owned_by_service` is kept in the list and never sent | `TestExitCodesFollowTheCLIContract` |
| B5.6 | Unix transport | private runtime directory, socket 0600, peer-credential check both sides | `control.Listen`, `Dial`: `$XDG_RUNTIME_DIR/go-microsip/control.sock`, directory 0700, socket 0600, `SO_PEERCRED` at both ends, one owner by `control.lock` | `TestAnEndpointThatIsNotPrivateIsRefused`, `TestOneOwnerServesASessionAndTheNextTakesOver`, `TestConnectionsAreBoundedAndAStrangerIsNotRead`; a peer of another user is driven with the kernel's answer replaced, not with a second user |
| B5.7 | Windows transport | per-user named pipes (`crates/platform-windows/src/pipes.rs`) | not built: task 6.3 (`control.ErrUnsupported` outside Linux) |  |
| B5.8 | transport limits | 16 connections, 10/s per connection, 50/s global, 5 s reply deadline, 1 MiB message | the same limits; a line over 1 MiB ends the connection; the owner answers on the connection, and the client waits 6 s at most | `TestTheTransportBoundsWhatAClientCanSend`, `TestConnectionsAreBoundedAndAStrangerIsNotRead`, `TestFramingIsBoundedAndLineBased` |
| B5.9 | 13 grant scopes | read:contacts, read:history, read:devices, read:config, control:dial/answer/reject/hangup/dtmf/mute/hold/transfer/dnd | `control.Scopes` | `TestGrantsRunOnlyWhatTheyCoverAndNeverPrompt`, `TestGrantedReadsArePagedAndHoldNoSecret` |
| B5.10 | approvals | 60 s window, at most 8 pending, pause after deny-all | `ApprovalWindow`, `MaxPending`, `PauseAfterDenyAll`; shown by the window, which comes in sight for a request | `TestDenyAllPausesControlAndOrphansAreNotPromisedAgain`, `TestAnAgentRequestWaitsForThePersonAtTheWindow`, `make go-window-call` ("agent control") |
| B5.11 | idempotency | same key and payload reports without repeating; different payload is `conflict`; earlier epoch is `stale_epoch` | `Owner.mutation` | `TestAPersonApprovesExactlyOnceAndReplaysNeverRepeat`, `TestKeysFromAnEarlierEpochAreStaleAndNeverRerun`, `TestAKeyPastItsRetentionIsAConflict` |
| B5.12 | operation ledger states | awaiting_approval, executing, succeeded, failed, denied, expired, outcome_unknown | `store.OperationState` (`app.Op...`), on the Rust schema's tables | `TestControlLedgerKeysEpochsAndFinalStates`, `TestControlRowsExpireAndTheAuditIsSanitizedAndClearable` |
| B5.13 | grant issue limits | validity 1..60 minutes, call quota up to 100, capability stored only as a hash | `Owner.Issue`, `store.InsertGrant`; no bootstrap grants (no service) | `TestGrantsRunOnlyWhatTheyCoverAndNeverPrompt`, `TestGrantsAreEpochBoundQuotaLimitedAndRevocable` |
| B5.14 | single-instance protocol | `activate`, `dial <target>`; acks ok/busy/invalid/service; `InstanceError` (5 variants) | the toolkit's single instance carries a link's number to the running window, which asks (task 6.1); no line protocol of its own | `make go-window-call` ("links") |

### B6. Configuration keys — `crates/config/src/lib.rs:110-207, 717-782`

Precedence per field: flag > environment > `.env` > TOML > SQLite > default.

| # | Key | Type and default | Go counterpart | Evidence |
| --- | --- | --- | --- | --- |
| B6.1 | `schema` | integer, must be 1 | `config.SchemaVersion` | `TestTOMLSchemaTypesAndUnknownKeysAreRejected` |
| B6.2 | `account.display_name` | string, empty | same key | `internal/core/config/config_test.go` |
| B6.3 | `account.username` | string, required | same key | `internal/core/config/config_test.go` |
| B6.4 | `account.auth_username` | string, optional | same key | `internal/core/config/config_test.go` |
| B6.5 | `account.domain` | string, required | same key | `internal/core/config/config_test.go` |
| B6.6 | `account.server` | string, default the domain | same key | `internal/core/config/config_test.go` |
| B6.7 | `account.port` | integer, 5060 or 5061 for TLS | same key | `internal/core/config/config_test.go` |
| B6.8 | `account.transport` | `udp`/`tcp`/`tls`, default `udp` | same key | `internal/core/config/config_test.go` |
| B6.9 | `account.media` | `rtp`/`srtp`, default `rtp`; `srtp` requires `tls` | same key | `internal/core/config/config_test.go` |
| B6.10 | `account.expires` | integer 1..86400, default 300 | same key | `internal/core/config/config_test.go` |
| B6.11 | `account.password_env` | variable name, default `RUST_MICROSIP_SIP_PASSWORD` | default becomes `GO_MICROSIP_SIP_PASSWORD` | `internal/core/config/config_test.go` |
| B6.12 | `account.password_file` | absolute path, optional | same key | `internal/core/config/config_test.go` |
| B6.13 | `account.ca_file` | absolute PEM path, optional; replaces system roots | same key | `internal/core/config/config_test.go` |
| B6.14 | `account.password` | TOML literal only, never saved or exported, file must be private | same key | `internal/core/config/config_test.go` |
| B6.15 | `ui.theme` | `system`/`light`/`dark`, default `dark` | values grow to `system` and the seven skins (design D7) | `internal/core/config/config_test.go` |
| B6.16 | `ui.dnd` | boolean, default false | same key | `internal/core/config/config_test.go` |
| B6.17 | `audio.input_device`, `audio.output_device` | string, optional | same key | `internal/core/config/config_test.go` |
| B6.18 | file rules | size limits, ownership and permission checks (`crates/config/src/files.rs:71-220`) | `internal/core/config/files.go`, `internal/platform/files` | `TestWritableOrSecretBearingFilesNeedSafePermissions`, `TestPasswordFileMustBePrivateRegularAndNotASymlink` |
| B6.19 | password source order | env var, `.env`, password file, TOML literal, then prompt or keyring | `config.Resolve`, then the keyring (`Phone.kept`) and the window's prompt | `TestPasswordSourcesFollowTheDocumentedOrder`, `TestATypedPasswordIsKeptInTheKeyringForTheNextStart` |
| B6.20 | write-back rule | only changed fields not held by a higher layer go to SQLite | `config.SQLiteEdits` | `TestEditsSkipUnchangedAndRefuseOverriddenFields` |
| B6.21 | doctor report shape (text and JSON) | `crates/config/src/doctor.rs:69-141` | `config.DoctorReport`; JSON adds `ignored_legacy_env`, text quotes strings once | `TestSQLiteLayerAloneResolvesAndInvalidValuesNameTheOrigin` |
| B6.22 | SQLite storage of settings and `secret_ref` | `crates/storage/src/settings.rs:16-233` | `internal/core/store/settings.go` | `TestSettingsRoundTripAcrossRestartWithoutSecrets`, `TestRustCandidateDatabaseOpens` |

### B7. Environment variables — `crates/config/src/lib.rs:189-194, 517-543`

| # | Variable | Maps to | Go counterpart | Evidence |
| --- | --- | --- | --- | --- |
| B7.1 | `RUST_MICROSIP_ACCOUNT_{DISPLAY_NAME,USERNAME,AUTH_USERNAME,DOMAIN,SERVER,PORT,TRANSPORT,MEDIA,EXPIRES,PASSWORD_ENV,PASSWORD_FILE,CA_FILE}` | the `account.*` keys | prefix `GO_MICROSIP_` (design D8) | `TestEveryPrecedenceCombinationPicksTheHighestSource`, `TestLegacyEnvironmentIsReportedNotRead` |
| B7.2 | `RUST_MICROSIP_UI_{THEME,DND}` | `ui.*` | prefix `GO_MICROSIP_` | `TestEveryPrecedenceCombinationPicksTheHighestSource`, `TestLegacyEnvironmentIsReportedNotRead` |
| B7.3 | `RUST_MICROSIP_AUDIO_{INPUT_DEVICE,OUTPUT_DEVICE}` | `audio.*` | prefix `GO_MICROSIP_` | `TestEveryPrecedenceCombinationPicksTheHighestSource`, `TestLegacyEnvironmentIsReportedNotRead` |
| B7.4 | `RUST_MICROSIP_SIP_PASSWORD` | default password variable | `GO_MICROSIP_SIP_PASSWORD` | `TestPasswordSourcesFollowTheDocumentedOrder` |
| B7.5 | `RUST_MICROSIP_TRACE` | sanitized engine diagnostics | `GO_MICROSIP_TRACE` |  |
| B7.6 | `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_RUNTIME_DIR`, `HOME` | configuration, data and runtime directories | same variables; the directory is `go-microsip` | `TestExplicitFilesMustExistButTheDefaultIsOptional`, `TestStorePath`, `TestImportFromRustPreviewsThenApplies` |
| B7.7 | `APPDATA`, `LOCALAPPDATA` | Windows directories |  |  |
| B7.8 | `APPDIR`, `APPIMAGE` | desktop entry target |  |  |
| B7.9 | `.env` handling and syntax | only from `--env-file`; `crates/config/src/dotenv.rs:13-89` |  |  |

### Index coverage for part B

`check_index_coverage` reported no recorded issue and matching freshness for every file cited above: `crates/core/src/lib.rs`; `crates/config/src/{cli,lib,dotenv,files,import,doctor}.rs`; `crates/storage/src/settings.rs`; `crates/control/src/{lib,instance,caller}.rs`; `crates/platform/src/{control,lib,instance,uri}.rs`; `crates/platform-windows/src/pipes.rs`; `crates/telephony/src/engine/mod.rs`; `apps/rust-microsip/src/{main,cli,mcp,service,owner,signals}.rs`, `apps/rust-microsip/src/bin/rust-microsip-cli.rs`, `apps/rust-microsip/src/ui/state/{control,grants}.rs`. The tool is best-effort, so every row was also read from source.

### Open points for the parity gate

- The Go command line is Cobra with subcommands (owner instruction of 2026-11-11): `--doctor` becomes `doctor`, `mcp --stdio` becomes `mcp`, `-V` stays as `--version` plus a `version` command. Rows B3.4, B3.5 and B3.19 close as accepted differences, not as identical syntax.
- Media properties of the Rust SRTP adapter that the Go stack (pion/srtp inside the vendored diago) does not provide, each needing the owner's acceptance or an implementation: the receiver bound to a single SSRC (`authenticated_other_ssrc_cannot_expand_the_bound_receiver`), transmit paths closed before the RTP or SRTCP index space is exhausted (`maximum_*_index_closes_both_transmit_paths_before_reuse`), and the bounded bank of unauthenticated sources (`unauthenticated_source_churn_never_binds_or_retains_provider_sessions`). A DTMF key is 7 packets of 100 ms instead of 8 of 120 ms.
- Configuration (`internal/core/config`, task 3.6) ports the 13 tests of `crates/config` and differs from the Rust candidate in three recorded ways: the names (`go-microsip` directory, `GO_MICROSIP_` prefix; `RUST_MICROSIP_` variables are listed by the doctor as not read, design D8); `ui.theme` accepts eight values and an unknown one resolves to `system` instead of failing (design D7); and the doctor's text prints strings quoted once (`account.domain = "pbx.invalid" [toml]`) and its JSON gains `ignored_legacy_env`. On Windows file ownership and mode are not checked yet, as in the Rust candidate (`internal/platform/files`, to be closed with the Windows adapters).
- Storage (`internal/core/store`, task 3.7) keeps the Rust schema (version 5, the five migrations verbatim) and ports 21 of the 25 tests of `crates/storage`; a database written by `rust-microsip 0.1.0-rc.3` opens unchanged (`TestRustCandidateDatabaseOpens`). Recorded differences: the pre-migration backup is made with `VACUUM INTO` instead of the online backup API (same guarantees: consistent, private, never overwrites); `SetSecretRef("")` clears the reference instead of being an error. The tables of the control ledger, audit and grants exist, but their API and its 4 tests are not ported yet: they belong to the control channel (group 5).
- Six error codes are defined but never constructed in the Rust code (`approval_denied`, `approval_expired`, `grant_expired`, `engine_owned_by_service`, `result_expired`, `unsupported`); decide whether the Go contract keeps them.
- Some JSON values in `status_get`, `config_show` and `history_list` come from Rust debug formatting (for example `config_show.media` yields `plainrtp`, not `rtp`); the Go contract should define these strings deliberately.
- Not inventoried here and to be added before the gate: the window's `Backend` contract, tray and notification actions, the SQLite schema of contacts and history, the Windows pipe naming and file-permission rules, and the storage ledger internals.
