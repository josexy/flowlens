# Proposal

## Why

Binary bodies currently offer a Raw tab that only redirects users to Hex or Image. The tab adds an ineffective interaction and can remain selected after switching from text.

## What Changes

- Hide Raw and the empty Formatted view when the shared body viewer receives a body marked binary.
- Keep image previews and Hex available, and resolve unavailable selections to an applicable view. Keep text, including SVG, eligible for Raw.
- Update the body-viewing specification, user guide, and regression coverage.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `traffic-inspection`: Show text-only body modes only when text is available and keep active selections valid.

## Impact

The owning packages are `internal/desktop/logic`, `internal/desktop/inspect`, and the shared pill tabs in `internal/desktop/widgets`; their shared viewer covers capture/history request and response bodies and Request Editor responses on Windows, macOS, and Linux. No platform-specific API is introduced.

Non-goals are changing body classification, decoding, image/SSE failure handling, the full HTTP Raw view, or storage/network models. Persistence, codecs, exports, Python Workers, process attribution, window lifecycle, events, and shortcuts are unaffected. Theme controls are reused; localized image error hints direct users to Hex and the tab group's accessible label describes body views.

Completion requires binary bodies to omit Raw/Formatted, correct selection fallback across body changes, retained text/SVG Raw access, byte-exact Hex/save behavior, passing UI and Go checks, light/dark rendering review, and synchronized documentation/specs.
