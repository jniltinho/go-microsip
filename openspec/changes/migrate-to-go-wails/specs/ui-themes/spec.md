# Spec Delta

## Purpose

Defines how go-microsip is skinned: a registry of interchangeable skins sharing one set of design tokens, so the interface can offer modern and nostalgic looks without per-screen styling work.

## ADDED Requirements

### Requirement: Shipped skins
The application SHALL ship at least these skins: Light, Dark, Windows XP, macOS, Ubuntu, Windows 98 and Matrix, plus a System choice that resolves to Light or Dark from the desktop preference. Every screen, dialog, menu and the incoming-call view SHALL be styled in every skin.

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
In every skin, text and control labels SHALL meet a 7:1 contrast ratio on their backgrounds (disabled text is exempt), the edges of controls, state icons and the focus ring SHALL meet 3:1, the focus ring SHALL be at least 2 px wide, and call states (ringing, active, on hold, muted, error, encrypted or not) SHALL be distinguishable by an icon and a label, not by color alone. Nothing SHALL move for longer than five seconds, or at all when the desktop asks for reduced motion.

#### Scenario: Contrast audit
- **WHEN** the automated contrast audit runs over each screen in each skin
- **THEN** no text falls below 7:1 and no control edge, state icon or focus ring falls below 3:1

#### Scenario: Reduced motion
- **WHEN** the desktop asks for reduced motion and the Matrix skin is selected
- **THEN** its backdrop is drawn still and nothing in the window animates

#### Scenario: Muted call in a retro skin
- **WHEN** a call is muted with the Windows 98 skin selected
- **THEN** the muted state is shown with an icon and a label, not only a color change

### Requirement: Self-contained skins
Skins SHALL use only assets bundled in the application with recorded licences and SHALL NOT fetch fonts, images or stylesheets from the network or include trademarked logos or copied proprietary artwork.

#### Scenario: Offline start
- **WHEN** the application starts with no network access and each skin is selected in turn
- **THEN** every skin renders completely and no network request is made for styling assets
