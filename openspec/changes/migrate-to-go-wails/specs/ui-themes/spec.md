# Spec Delta

## Purpose

Defines how go-microsip is skinned: a registry of interchangeable skins sharing one set of design tokens, so the interface can offer modern and nostalgic looks without per-screen styling work.

## ADDED Requirements

### Requirement: Shipped skins
The application SHALL ship at least these skins: Light, Dark, Windows XP, macOS, Ubuntu and Windows 98, plus a System choice that resolves to Light or Dark from the desktop preference. Every screen, dialog, menu and the incoming-call view SHALL be styled in every skin.

#### Scenario: Every screen in every skin
- **WHEN** the screenshot matrix runs each screen in each shipped skin
- **THEN** every screen renders with that skin's colors, fonts and control style, and none falls back to another skin's appearance

#### Scenario: System follows the desktop
- **WHEN** System is selected and the desktop switches between light and dark
- **THEN** the window follows within two seconds without a restart

### Requirement: Live switching and persistence
The skin SHALL be selectable in settings, apply immediately to the whole window without restarting or interrupting a call, and be restored on the next start. An unknown or removed skin identifier SHALL resolve to System.

#### Scenario: Switch during a call
- **WHEN** the user changes the skin during an active call
- **THEN** the whole window changes at once and the call's audio and state are unaffected

#### Scenario: Unknown skin in the configuration
- **WHEN** the stored skin identifier does not match a shipped skin
- **THEN** the application starts with System and reports the fallback in the doctor output

### Requirement: One token contract for all skins
Every skin SHALL define the same set of design tokens, and screens SHALL take colors, fonts, borders and spacing only from those tokens. An automated check SHALL fail when a skin omits or adds a token, so a new skin needs one stylesheet and one registry entry and no screen changes.

#### Scenario: Skin with a missing token
- **WHEN** a skin stylesheet leaves out a token another skin defines
- **THEN** the skin check fails and names the skin and the token

#### Scenario: Adding a skin
- **WHEN** a new skin stylesheet defining the full token set is added and registered
- **THEN** it appears in settings and styles every screen without editing any screen

### Requirement: Legibility and call-state clarity in every skin
In every skin, text and controls SHALL meet a 4.5:1 contrast ratio, focus SHALL be visible, and call states (ringing, active, on hold, muted, error) SHALL be distinguishable by more than color alone. In Light and Dark, text SHALL meet 7:1, and the boundaries of controls and the focus indicator SHALL meet 3:1 against what surrounds them.

#### Scenario: Contrast audit
- **WHEN** the automated contrast audit runs over each screen in each skin
- **THEN** no text or control falls below 4.5:1

#### Scenario: Contrast of the default skins
- **WHEN** the automated contrast audit runs over each screen in Light and Dark
- **THEN** no text falls below 7:1 and no control boundary or focus indicator falls below 3:1

#### Scenario: Muted call in a retro skin
- **WHEN** a call is muted with the Windows 98 skin selected
- **THEN** the muted state is shown with an icon and a label, not only a color change

### Requirement: Self-contained skins
Skins SHALL use only assets bundled in the application with recorded licences and SHALL NOT fetch fonts, images or stylesheets from the network or include trademarked logos or copied proprietary artwork.

#### Scenario: Offline start
- **WHEN** the application starts with no network access and each skin is selected in turn
- **THEN** every skin renders completely and no network request is made for styling assets
