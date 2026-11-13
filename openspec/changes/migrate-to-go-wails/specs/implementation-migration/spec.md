# Spec Delta

## Purpose

Defines the rules for moving the product from the Rust implementation to go-microsip without losing user data, overstating progress, or leaving two release lines: coexistence, data import, the parity gate, archiving and renaming.

## ADDED Requirements

### Requirement: Coexistence until cutover
Until the parity gate is recorded, the Rust application SHALL keep building and the Go application SHALL use its own binary name, configuration directory, runtime directory, control endpoint and desktop entry, so both can be installed and run by the same user without sharing writable state.

#### Scenario: Both applications running
- **WHEN** the Rust candidate and a go-microsip milestone build run in the same user session
- **THEN** each keeps its own single-instance lock and settings, and neither modifies the other's files

### Requirement: Explicit data import
go-microsip SHALL offer an import of the Rust application's settings, contacts, history and keyring password that shows a preview, requires the user's confirmation, copies a consistent snapshot, and never modifies the source. Nothing SHALL be imported silently, and a failed or interrupted import SHALL leave go-microsip's existing data and credentials unchanged.

#### Scenario: Import with confirmation
- **WHEN** the user runs the import on a profile with Rust-application data and confirms the preview
- **THEN** the account, contacts and history appear in go-microsip and the Rust application's files are byte-identical to before

#### Scenario: First start without import
- **WHEN** go-microsip starts for the first time on a profile with Rust-application data and the user does not request an import
- **THEN** go-microsip starts empty and reads nothing from the Rust application's data

#### Scenario: Interrupted import
- **WHEN** the import is killed at each step, including between storing the credential and replacing the database
- **THEN** on the next start go-microsip shows its previous data, no partially imported credential remains in use, and the import can be run again

#### Scenario: Settings kept only in the configuration file
- **WHEN** the Rust profile's account and TLS settings exist only in its configuration file
- **THEN** after the confirmed import go-microsip shows the same account and TLS settings

#### Scenario: Rust application still running
- **WHEN** the import is requested while the Rust application is running
- **THEN** the import refuses, explains that the Rust application must be closed, and changes nothing

### Requirement: Recorded parity gate
The Rust code SHALL be archived only after a parity receipt is recorded for one commit. The baseline is every requirement and scenario of the Rust-era specs at the `0.1.0-rc.3` commit; each SHALL be proven with runtime evidence or dropped by a scope change the owner approves in writing. A review, a build, or a rendered screen SHALL NOT count as call evidence.

#### Scenario: Gate with a missing behavior
- **WHEN** the parity receipt lacks evidence for blind transfer
- **THEN** the gate is not passed and the Rust code is not archived

#### Scenario: Baseline requirement without a row
- **WHEN** a requirement or scenario of the pinned Rust-era specs has no row in the parity record
- **THEN** the gate is not passed

#### Scenario: Accepted difference
- **WHEN** the Go application intentionally differs from the Rust candidate
- **THEN** the difference is listed in the receipt with its reason and the owner's acceptance

### Requirement: Archived Rust reference
After cutover, the Rust workspace SHALL live under `archive/rust/` as historical reference: it SHALL NOT be built by the default build, shipped in any artifact, or released, and local-only references SHALL stay out of the repository.

#### Scenario: Default build after cutover
- **WHEN** the documented build runs from a clean clone after cutover
- **THEN** it produces only Go artifacts and does not compile anything under `archive/`

### Requirement: Released artifacts validated after cutover
Artifacts published after the cutover SHALL be rebuilt from the post-cutover commit and SHALL pass the call and start-up smoke checks again on those exact files before publication.

#### Scenario: Packaging changed after the gate
- **WHEN** the default build, packaging or release workflow changes after the parity receipt
- **THEN** the artifacts are rebuilt and re-checked, and the release notes cite the hashes that were checked

### Requirement: Repository and product rename
The product, binary, package and repository SHALL be named `go-microsip`. The repository rename SHALL happen only with the owner's confirmation, and documentation, badges, module path and remotes SHALL point to the new name afterwards.

#### Scenario: Rename confirmed
- **WHEN** the owner confirms the rename and it is applied
- **THEN** the remote resolves to `jniltinho/go-microsip`, a push succeeds, and no tracked file still presents `rust-microsip` as the current product name outside `archive/` and historical records

### Requirement: Honest status of superseded work
Rust-era changes superseded by this migration SHALL be closed without marking unverified tasks complete. A requirement they introduced SHALL NOT be dropped silently: it is either proven by go-microsip or removed by an owner-approved scope change. Progress reports SHALL distinguish planned, implemented and verified work.

#### Scenario: Superseded change with open tasks
- **WHEN** a superseded Rust-era change still has unchecked tasks at cutover
- **THEN** those tasks stay unchecked and the change records that it was superseded by this migration
