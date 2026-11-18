# rewrite-rules Specification

## Purpose
Defines the interface for creating, ordering, testing, and saving live HTTP(S) rewrite rules, including draft handling.

## Requirements

### Requirement: Rule management
The Rewrite Rules section SHALL list rules with a master switch and per-rule switches, and SHALL let the user create, name, delete, and reorder rules by dragging or with arrow controls.

#### Scenario: New rule
- **WHEN** the user creates a rule
- **THEN** it starts disabled

#### Scenario: Toggles and ordering
- **WHEN** the user toggles a rule or reorders the list
- **THEN** the change is saved immediately

#### Scenario: Loading failure
- **WHEN** rules fail to load at startup
- **THEN** the error is shown, editing is blocked, and the proxy does not start

#### Scenario: Unsupported saved rule
- **WHEN** a saved rule is of an unsupported form
- **THEN** it remains visible and does not execute

### Requirement: Rule editing
A rule SHALL configure methods, a URL pattern, redirect target with Keep current Host, ordered header and query operations of Add, Set, and Delete, and UTF-8 body replacement as direct text or regular expression.

#### Scenario: Row order
- **WHEN** header operations are arranged in a sequence
- **THEN** they are saved and applied in that displayed order

#### Scenario: Invalid rule
- **WHEN** the pattern or redirect target is invalid
- **THEN** a validation message identifies the field and the rule is not saved

### Requirement: Match test
The rule editor SHALL test a sample URL against the pattern and show the resulting target without sending a request.

#### Scenario: Matching sample
- **WHEN** the user tests a URL that matches the pattern
- **THEN** the captured groups are applied and the resulting URL is shown

#### Scenario: Non-matching sample
- **WHEN** the sample does not match
- **THEN** the result states that there is no match

### Requirement: Regex replacement preview
The rule editor SHALL preview a regular expression replacement on sample text, showing the match count and an inline diff, without sending requests or saving the sample.

#### Scenario: Live refresh
- **WHEN** the user edits the pattern or replacement while previewing
- **THEN** the preview refreshes and copy is disabled for pending or failed results

#### Scenario: Large sample
- **WHEN** the sample text is large
- **THEN** the full result is shown without diff highlighting or wrapping

### Requirement: Draft handling
Rule edits SHALL remain drafts until saved with Save or the Save shortcut, Revert SHALL restore saved content, and switching rules SHALL retain drafts. Deleting a rule SHALL offer only Cancel and Delete; Delete SHALL permanently remove the rule and its unsaved edits.

#### Scenario: Switching rules
- **WHEN** the user edits a rule and selects another
- **THEN** the first rule's draft is retained and marked unsaved

#### Scenario: Save failure
- **WHEN** saving fails
- **THEN** the draft is retained and the error is shown

#### Scenario: External conflict
- **WHEN** the saved rule changed since the draft began
- **THEN** the user must resolve the conflict before overwriting it

#### Scenario: Deleting a dirty rule
- **WHEN** the user deletes a rule with unsaved edits
- **THEN** FlowLens offers Cancel and Delete; Cancel keeps the draft, and Delete removes the rule and its draft without saving

#### Scenario: Deletion fails
- **WHEN** deleting a rule fails
- **THEN** the dialog stays open and the draft is retained so the user can retry
