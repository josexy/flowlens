# Proposal

## Why

ICO responses are classified as images but cannot be decoded by the current previewer. Ordinary favicon requests therefore show an unsupported-format notice, including the reported Baidu favicon.

## What Changes

- Add pure-Go ICO decoding to the shared image preview, including PNG and DIB representations, transparency, and multi-image files.
- Check ICO directory ranges and decoded dimensions before allocating pixel buffers.
- Preserve original bytes for Hex and saving, and retain existing failure messages for invalid images.
- Add regression and rendering coverage and update the user guide.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `traffic-inspection`: Preview ICO image bodies through the existing Image tab.

## Impact

The owner is `internal/desktop/inspect`, shared by capture/history details and Request Editor responses. A pure-Go codec dependency works with `CGO_ENABLED=0` on Windows, macOS, and Linux; there are no platform-specific APIs or external processes.

Networking, stored traffic/models/codecs, Python Workers, process attribution, settings, events, shortcuts, window lifecycle, and localized copy are unchanged. New decoding runs through the existing background job lifecycle. Completion requires PNG/DIB and multi-image regression coverage, malformed-input handling, byte-exact saving and Hex, successful applicable Go/UI checks, and light/dark render review.
