# Design

## Context

See proposal.md for motivation. `logic.BodyTabs` always includes Raw and can offer Formatted for binary bodies with a structured-text MIME type. `inspect.BodyViewer.set` then fills Raw with a hint and Formatted with an empty string. The existing spec explicitly describes the hint; this change replaces that behavior.

## Goals / Non-Goals

Keep eligibility and fallback in the existing logic/view owners. Preserve service-level binary classification and image/SSE diagnostics; no schema, transport, or codec changes are needed.

## Decisions

- Use the existing binary marker to gate Raw and Formatted in `logic.BodyTabs`. Checking only MIME would lose textual SVG or expose invalid UTF-8. Reclassifying payloads in the view would duplicate service rules and add work per frame.
- Reuse `NextBodyTab` on every update to reject stale choices as well as transitions, preserving its image/SSE defaults and valid user choices. Clear hidden text buffers for binary content instead of installing placeholder copy.
- Keep the previous item list with the shared pill tab control's index. When available items change, resolve the index from the selected ID instead of interpreting an old index as a click. The transition regression exposed this when Hex moved after text modes returned.
- Retain existing asynchronous image decoding and its failure message, SSE-specific diagnostics, byte-based Hex, saving, and empty/missing states. Cancellation, stale result rejection, and shutdown remain with the existing runtime jobs.
- Keep selection state on the main thread and reuse Nui pill tabs. The shared viewer covers capture/history details and Request Editor responses without new key handlers or window logic. Reuse light/dark colors; update English/Chinese image failure hints to point to Hex, remove the unused binary placeholder, and name the tab group Body views instead of Raw.

## Risks / Trade-offs

- A stale Raw selection could expose an empty panel after tabs change; verify replacement, repeated frames, and restoration to text with the UI tester.
- Binary payloads can declare JSON or text MIME types; verify the marker takes precedence for text-only modes and original bytes remain available in Hex/save.
- Image decode failures keep a diagnostic preview alongside Hex; they do not restore a placeholder Raw tab.

## Migration Plan

No persistent data migration is required. Reverting the view and eligibility changes restores the former tabs without changing saved data. Validate headlessly on Windows and inspect both themes/languages; this does not claim native macOS/Linux runtime acceptance.
