# Spec Delta

## MODIFIED Requirements

### Requirement: Tooltips on icon-only controls
Every icon-only control that had a tooltip in FlowLens 1.0.5 SHALL show a tooltip with the same localized text. Where 1.0.5 showed the command's key binding, the tooltip SHALL show the resolved binding.

#### Scenario: Shortcut row actions
- **WHEN** the user hovers the clear or reset button of a shortcut row in Settings
- **THEN** a tooltip names the action and the command

#### Scenario: Toolbar control with a binding
- **WHEN** the user hovers Clear Traffic in the proxy toolbar
- **THEN** the tooltip shows the label and the resolved Clear Traffic binding

#### Scenario: Split proxy control
- **WHEN** the user hovers the split control's Start Proxy, Pause, Resume, or dropdown arrow
- **THEN** no tooltip is shown, and the controls retain their visible or accessible names
