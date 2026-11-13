# Spec Delta

## Purpose

Defines the macOS universal application as a release artifact of go-microsip: how it is built, what the bundle holds, how it is signed and opened, which checks run on it and on which machines, and what the documentation may claim about macOS.

## ADDED Requirements

### Requirement: Universal application artifact
The build SHALL produce `go-microsip-X.Y.Z-macos-universal.zip`, holding `go-microsip.app` whose one executable has an arm64 and an x86_64 slice built from the same commit as the other artifacts. The bundle SHALL carry the version, the application identifier, the icon, the licence and the third-party notices.

#### Scenario: Both slices
- **WHEN** the zip of a build is unpacked on a Mac
- **THEN** the executable in the bundle reports the architectures x86_64 and arm64, and `version` run from it prints the version and the commit of the build

#### Scenario: Bundle contents
- **WHEN** the zip is listed
- **THEN** it holds the application bundle with its information file, its executable, its icon, the licence and the notices, and nothing else

### Requirement: Built in Docker without Apple tools
The macOS artifact SHALL be built by the same Docker-only build as the other artifacts, without cgo, without a macOS machine and without Apple's tools or SDK, and SHALL be part of the one checked set of files a release publishes.

#### Scenario: Host with only Docker
- **WHEN** `make dist` runs on a Linux host that has Docker and no Go, Node or Apple tool
- **THEN** the macOS zip is in `dist/go` beside the Linux and Windows files

#### Scenario: Part of the checked set
- **WHEN** the build completes
- **THEN** `SHA256SUMS` names the macOS zip, the manifest records it, the bill of materials and the notices cover what it contains, and the publication scan has read its unpacked contents

### Requirement: Bundle declarations
The bundle's information file SHALL declare the minimum macOS version the executable was built for, the reasons the application uses the microphone and the local network, the `sip` and `tel` link schemes, and that the application is not an agent hidden from the Dock.

#### Scenario: Declarations match the executable
- **WHEN** the build's check reads the information file and the executable's slices
- **THEN** the declared minimum macOS version is not lower than the one recorded in either slice, and the microphone and local-network reasons and both link schemes are present

### Requirement: Unsigned application, stated plainly
The application SHALL be published without a Developer ID signature and without notarization, unsigned beyond what lets the arm64 slice run, and the guide and the release notes SHALL say so, with the exact steps to open a downloaded copy and to check it against `SHA256SUMS`.

#### Scenario: A downloaded copy
- **WHEN** a user downloads the zip with a browser on a Mac and follows the guide's steps
- **THEN** the application opens, and the guide's wording matches what macOS showed in the recorded check

#### Scenario: Notes of a release
- **WHEN** release notes list the macOS file
- **THEN** they say it is unsigned and not notarized and point to those steps

### Requirement: Checks on both architectures in CI
For every commit on `main` and every pull request, CI SHALL run the macOS zip that the build produced on a hosted macOS machine of each architecture GitHub offers: its checksum, its slices, the headless commands and the window's self-test. An architecture with no hosted machine SHALL be documented as built and not run. The release workflow SHALL refuse to publish when the macOS file is missing from the checked set.

#### Scenario: Green run
- **WHEN** CI runs for a commit
- **THEN** a macOS job for each architecture GitHub hosts unpacks the built zip and passes the version, doctor, MCP handshake and window self-test checks

#### Scenario: Architecture not available
- **WHEN** GitHub offers no hosted machine for one architecture
- **THEN** the documentation states that this architecture is built and not run by CI, and names the check that covers it instead, or that none does

#### Scenario: Missing file at release
- **WHEN** a tag is pushed and the checked artifact set has no macOS zip
- **THEN** the release workflow fails before anything is published

### Requirement: Checks on a Mac over SSH
The project SHALL provide a target that copies the built zip to a Mac named by an environment variable and runs there, over SSH with public-key authentication and a host key the operator verified, the headless checks, the window's self-test, the adapters' tests and a lab call. No address, user name, password or host key of that machine SHALL be written in the repository, in an artifact, in a log or in what the target prints.

#### Scenario: Run against the owner's VM
- **WHEN** the target runs with the variable naming the owner's macOS VM
- **THEN** each check prints pass or fail, the run's log goes to the git-ignored artifacts folder, and the receipt records the macOS version, the architecture and the checksum of the zip that was run

#### Scenario: Variable not set
- **WHEN** the target runs without the variable
- **THEN** it stops with a message naming the variable and runs nothing

#### Scenario: Unknown host key
- **WHEN** the Mac's host key is not in the trust file the operator supplied
- **THEN** the target fails without connecting further and without adding the key by itself

#### Scenario: Password authentication
- **WHEN** the Mac accepts only a password
- **THEN** the target fails without prompting for one or reading one from anywhere

### Requirement: Gates of a final release that carries the macOS file
A final version that publishes the macOS zip SHALL require, beside the existing gates, every task of this change checked, the mandatory macOS rows of the manual checklist passed on each architecture among them; the notification's buttons MAY be recorded as not applicable where the recorded measurement shows that macOS refuses notifications and that the window's ringing works instead. A pre-release MAY carry the macOS zip before that, and its notes SHALL list what was not run on macOS.

#### Scenario: Final version with an open macOS task
- **WHEN** a final tag is pushed while a task of this change is unchecked in the release's gate list
- **THEN** the release workflow refuses to publish

#### Scenario: A failed row
- **WHEN** the manual checklist's sound row for one architecture is recorded as failed or not run
- **THEN** the task that gates the final version stays unchecked

#### Scenario: Pre-release
- **WHEN** an `-rc.N` tag publishes the macOS zip before the manual checklist's macOS rows are filled
- **THEN** it is published as a pre-release whose notes name each macOS item not run

### Requirement: Claims follow the evidence
The documentation SHALL name, for macOS, the versions and architectures on which each of the window, sound, the keychain, agent control, notifications and links was run, and SHALL list as not verified whatever was not. Sound SHALL be claimed for macOS only after a five-minute two-way call within the threshold on a Mac with real sound hardware.

#### Scenario: Sound not yet measured on hardware
- **WHEN** the macOS file is published before the manual checklist's macOS rows are filled
- **THEN** the guide's Platforms section says sound on macOS is not verified on real hardware

#### Scenario: Platform table
- **WHEN** a reader opens the guide's Platforms section
- **THEN** macOS has a row per verified item with the macOS version and the architecture it was run on
