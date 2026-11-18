# Spec Delta

## MODIFIED Requirements

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
