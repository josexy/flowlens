# localization Specification

## Purpose
Defines the languages FlowLens is presented in and how interface text follows the user's language choice across windows.

## Requirements

### Requirement: Supported languages
FlowLens SHALL present all interface text in Chinese and English, defaulting to Chinese.

#### Scenario: Fresh installation
- **WHEN** FlowLens starts with no saved language
- **THEN** the interface is shown in Chinese

#### Scenario: Built-in control text
- **WHEN** the language is English
- **THEN** text supplied by interface components, such as empty states and picker labels, is also English

### Requirement: Runtime language switching
Changing the language SHALL update every open window and the tray menu without a restart, and the choice SHALL persist.

#### Scenario: Switching language
- **WHEN** the user switches from Chinese to English
- **THEN** every open window and the tray menu show English text immediately

#### Scenario: Language after restart
- **WHEN** the user has selected English and restarts FlowLens
- **THEN** the interface is shown in English

### Requirement: Complete translations
Every interface message SHALL exist in both languages with matching placeholders.

#### Scenario: Catalog check
- **WHEN** the localization check runs
- **THEN** it fails if a message is missing from either language or the placeholders of its translations differ

### Requirement: Localized Python API help
The Python plugin API help SHALL be available in both languages.

#### Scenario: Help in English
- **WHEN** the language is English and the user opens Python API help
- **THEN** descriptions are shown in English

### Requirement: Backend error presentation
Messages shown for known backend failures SHALL use the current language, and unknown errors SHALL show the original error text.

#### Scenario: Unknown backend error
- **WHEN** an operation fails with an error that has no translated message
- **THEN** a translated summary is shown together with the original error text
