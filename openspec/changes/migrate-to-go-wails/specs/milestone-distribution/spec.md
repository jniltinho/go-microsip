# Spec Delta

## Purpose

Defines how go-microsip is built, packaged and verified at every milestone: reproducible Docker builds, a fixed set of downloadable artifacts, an isolated PBX and VM test matrix, and an independent review before a milestone is declared.

## ADDED Requirements

### Requirement: Docker-only build
Every artifact SHALL build from a clean clone with only Docker available, without a Go, Node, Rust or C toolchain on the host, and without sending local-only references, secrets or previous artifacts in the build context.

#### Scenario: Clean machine
- **WHEN** an operator runs the documented build command on a machine with Docker and no language toolchain
- **THEN** the artifacts are produced in `dist/`

#### Scenario: Build context audit
- **WHEN** the build context is inspected
- **THEN** it contains no file from `base/`, `dist/`, `artifacts/`, no `.env` file and no database or key file

### Requirement: Milestone artifact set
Each milestone SHALL produce in `dist/` a Linux amd64 executable, an `x86_64` AppImage, a Windows amd64 `.exe` and `.zip`, a checksum file, and the third-party licence notices, all built from one recorded commit. macOS artifacts SHALL NOT be claimed until a macOS build is delivered by a later change.

#### Scenario: Milestone build
- **WHEN** the milestone build command completes
- **THEN** the four artifacts and the checksum file exist, the checksums verify, and the manifest names the commit

#### Scenario: Version reported
- **WHEN** each artifact is run with the version command
- **THEN** it prints the milestone version and the commit it was built from

### Requirement: Self-contained artifacts
The AppImage SHALL run on a clean Ubuntu 26.04 or later and on a clean Debian 13 desktop (x86_64) without installing extra packages and without FUSE, and the Windows executable SHALL run on a clean supported Windows without installing a runtime other than the system webview. Artifacts SHALL NOT contain personal data, absolute personal paths or credentials.

#### Scenario: Clean Linux desktop
- **WHEN** the AppImage is started in a clean Ubuntu 26.04 desktop VM and in a clean Debian 13 desktop VM
- **THEN** on both the window opens and a lab call has two-way audio without any package being installed

#### Scenario: Unsupported distribution
- **WHEN** the documentation and release notes list the supported systems
- **THEN** they name Ubuntu 26.04 and later, Debian 13 and the supported Windows versions, and promise nothing for older distributions

#### Scenario: Clean Windows
- **WHEN** the `.exe` is started in a clean Windows VM
- **THEN** the window opens and a lab call has two-way audio

### Requirement: Isolated test matrix
Call tests SHALL use only disposable PBXs with fictional extensions on an isolated network — the project's Asterisk lab and a disposable FreePBX installation for interoperability — and disposable VMs for desktop checks. Tests SHALL NOT use production PBXs, real credentials, personal settings or the installed reference application.

#### Scenario: Interoperability call
- **WHEN** the candidate registers to the FreePBX installation and to the Asterisk lab
- **THEN** a two-way call with DTMF, hold and blind transfer succeeds on both and the receipt records PBX, transport and codec

#### Scenario: Teardown
- **WHEN** a test run ends
- **THEN** the PBX containers, networks and VMs created for it are removed and the host's configuration is unchanged

### Requirement: Automated interface checks
Each milestone that changes the interface SHALL be exercised through the built application by desktop automation and through the frontend by browser automation, producing screenshots of each screen per skin and a keyboard-operation result.

#### Scenario: Built application driven
- **WHEN** desktop automation opens the milestone AppImage, dials a lab extension and hangs up
- **THEN** the run records each step's result and the screenshots, with fictional data only

### Requirement: Independent review per milestone
The proposal and each milestone SHALL be reviewed by an independent Codex run with read-only access and sanitized context. Blocking findings SHALL be resolved or explicitly accepted by the owner before the milestone is declared, and a review SHALL NOT count as runtime evidence.

#### Scenario: Blocking finding
- **WHEN** the review reports a blocking finding
- **THEN** the milestone is not declared until the finding is fixed and re-reviewed or the owner records its acceptance

#### Scenario: Review context
- **WHEN** the review input is prepared
- **THEN** it contains no secret, no personal path and no local-only reference material

### Requirement: Release gates preserved
A version tag SHALL be created only on a commit whose CI run on the main branch is green, and a final version only when every release gate is checked with evidence and the owner has decided. Milestone builds in `dist/` SHALL NOT be presented as releases.

#### Scenario: Tag on a red commit
- **WHEN** a tag is pushed on a commit whose CI run is not green
- **THEN** the release workflow refuses to build or publish
