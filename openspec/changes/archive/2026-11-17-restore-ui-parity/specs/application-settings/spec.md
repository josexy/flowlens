# Spec Delta

## ADDED Requirements

### Requirement: Descriptive certificate and interpreter pickers
File pickers opened from Settings SHALL show the localized title and, where the platform shows one, a message naming the expected file. This covers the MITM CA certificate and key, upstream Root CA files, the client certificate and key, and the Python interpreter. Key pickers SHALL offer a filter named "Private Key Files".

#### Scenario: Choosing a client key on macOS
- **WHEN** the user browses for a client certificate's private key on macOS
- **THEN** the dialog explains that it expects the key matching the client certificate, and it offers the Private Key Files filter

## MODIFIED Requirements

### Requirement: General settings
General settings SHALL configure fonts, font sizes, theme colors, title bar mode, and main window close behavior.

#### Scenario: Font search
- **WHEN** the user searches the font list
- **THEN** matching system fonts are offered with a preview

#### Scenario: Title bar change requires restart
- **WHEN** the user changes the title bar mode
- **THEN** a notice states that a restart is required

#### Scenario: No pointer cursor option
- **WHEN** the user opens General settings, including after upgrading from 1.0.5 with the pointer cursor enabled
- **THEN** no pointer cursor option is shown, and the other general settings load and save unchanged
