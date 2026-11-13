# Spec Delta

## ADDED Requirements

### Requirement: Native interface usability
The interface SHALL preserve FlowLens's information hierarchy, available actions, meaningful status distinctions, and readable light and dark themes. Native control placement, dimensions, typography, scrolling, and animation SHALL be accepted when those properties remain usable. Visual differences from the original frontend SHALL NOT alone require restoring its presentation.

#### Scenario: Reviewing an equivalent native surface
- **WHEN** a native surface exposes the same information and business actions with different spacing, control placement, or platform rendering
- **THEN** those differences are accepted if content is readable, actions remain discoverable and operable, and state distinctions remain clear

#### Scenario: Visual similarity hides an interaction loss
- **WHEN** a visually similar control cannot be reached or operated through an input method required for that control
- **THEN** it fails interaction acceptance regardless of screenshot similarity

## REMOVED Requirements

### Requirement: FlowLens 1.0.5 as the visual reference
**Reason**: Requiring the original frontend's visual implementation conflicts with the native-first direction.
**Migration**: Use Native interface usability for visual review, retain specific business and information requirements, and record accepted capability differences in the UI comparison documentation.
